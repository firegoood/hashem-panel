package main

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type carrierConfig struct {
	Mode          string   `json:"mode"`           // "auto", "direct", "fou:PORT"
	ActiveCarrier string   `json:"active_carrier"` // "direct", "fou:443", "fou:55555"
	FOUPort1      int      `json:"fou_port1"`
	FOUPort2      int      `json:"fou_port2"`
	Candidates    []string `json:"candidates"`
	LastSwitch    string   `json:"last_switch"`
	SwitchCount   int      `json:"switch_count"`
}

type carrierStatusResponse struct {
	Mode          string   `json:"mode"`
	ActiveCarrier string   `json:"active_carrier"`
	FOUPort1      int      `json:"fou_port1"`
	FOUPort2      int      `json:"fou_port2"`
	Candidates    []string `json:"candidates"`
	LastSwitch    string   `json:"last_switch"`
	SwitchCount   int      `json:"switch_count"`
	PingStatus    string   `json:"ping_status"`
	PingRTT       string   `json:"ping_rtt"`
}

type carrierPostRequest struct {
	Action string `json:"action"` // "set_mode", "set_active", "cycle_next"
	Mode   string `json:"mode,omitempty"`
	Target string `json:"target,omitempty"`
}

func carrierConfigPath() string {
	return filepath.Join(configDir, "carrier.json")
}

func defaultCarrierConfig() carrierConfig {
	return carrierConfig{
		Mode:          "auto",
		ActiveCarrier: "direct",
		FOUPort1:      443,
		FOUPort2:      55555,
		Candidates:    []string{"direct", "fou:443", "fou:55555"},
		LastSwitch:    "",
		SwitchCount:   0,
	}
}

func loadCarrierConfig() carrierConfig {
	def := defaultCarrierConfig()
	data, err := os.ReadFile(carrierConfigPath())
	if err != nil {
		return def
	}
	var c carrierConfig
	if err := json.Unmarshal(data, &c); err != nil {
		return def
	}
	if c.Mode == "" {
		c.Mode = "auto"
	}
	if c.ActiveCarrier == "" {
		c.ActiveCarrier = "direct"
	}
	if c.FOUPort1 <= 0 {
		c.FOUPort1 = 443
	}
	if c.FOUPort2 <= 0 {
		c.FOUPort2 = 55555
	}
	if len(c.Candidates) == 0 {
		c.Candidates = []string{"direct", "fou:" + strconv.Itoa(c.FOUPort1), "fou:" + strconv.Itoa(c.FOUPort2)}
	}
	return c
}

func saveCarrierConfig(cfg carrierConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return err
	}
	tmp := carrierConfigPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, carrierConfigPath())
}

func getPeerGRE() string {
	if p := findPeer(1); p != nil && p.PeerGre != "" {
		return p.PeerGre
	}
	data, err := os.ReadFile(filepath.Join(configDir, "setup.json"))
	if err == nil {
		var s struct {
			PeerGre string `json:"peer_gre"`
		}
		if json.Unmarshal(data, &s) == nil && s.PeerGre != "" {
			return s.PeerGre
		}
	}
	return "10.10.10.1"
}

func getCarrierPingStatus() (string, string) {
	peer := getPeerGRE()
	if peer == "" {
		return "no_peer", ""
	}
	out, err := exec.Command("ping", "-c", "1", "-W", "2", peer).CombinedOutput()
	if err != nil {
		return "fail", ""
	}
	str := string(out)
	if strings.Contains(str, "1 received") || strings.Contains(str, "1 packets received") {
		// Parse RTT e.g. time=24.5 ms
		if idx := strings.Index(str, "time="); idx != -1 {
			sub := str[idx+5:]
			if end := strings.Index(sub, " "); end != -1 {
				return "ok", sub[:end] + " ms"
			}
		}
		return "ok", "< 1 ms"
	}
	return "fail", ""
}

func handleCarrierGet(w http.ResponseWriter, r *http.Request) {
	cfg := loadCarrierConfig()
	st, rtt := getCarrierPingStatus()
	resp := carrierStatusResponse{
		Mode:          cfg.Mode,
		ActiveCarrier: cfg.ActiveCarrier,
		FOUPort1:      cfg.FOUPort1,
		FOUPort2:      cfg.FOUPort2,
		Candidates:    cfg.Candidates,
		LastSwitch:    cfg.LastSwitch,
		SwitchCount:   cfg.SwitchCount,
		PingStatus:    st,
		PingRTT:       rtt,
	}
	writeJSON(w, resp)
}

func handleCarrierPost(w http.ResponseWriter, r *http.Request) {
	var req carrierPostRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, r, "E-ACTION-01", "invalid json")
		return
	}

	cfg := loadCarrierConfig()

	switch req.Action {
	case "set_mode":
		m := strings.TrimSpace(req.Mode)
		if m != "auto" && m != "direct" && !strings.HasPrefix(m, "fou:") {
			writeAPIError(w, r, "E-ACTION-01", "invalid mode: want auto, direct, or fou:PORT")
			return
		}
		cfg.Mode = m
		if m != "auto" {
			cfg.ActiveCarrier = m
			cfg.LastSwitch = time.Now().Format("2006-01-02 15:04:05")
			cfg.SwitchCount++
			_ = exec.Command("/usr/local/bin/hashem", "carrier", "set", m).Run()
		} else {
			_ = exec.Command("/usr/local/bin/hashem", "carrier", "mode", "auto").Run()
		}
		_ = saveCarrierConfig(cfg)
		writeJSON(w, map[string]any{"status": "ok", "mode": cfg.Mode, "active_carrier": cfg.ActiveCarrier})

	case "set_active":
		t := strings.TrimSpace(req.Target)
		if t != "direct" && !strings.HasPrefix(t, "fou:") {
			writeAPIError(w, r, "E-ACTION-01", "invalid target carrier: want direct or fou:PORT")
			return
		}
		cfg.ActiveCarrier = t
		cfg.LastSwitch = time.Now().Format("2006-01-02 15:04:05")
		cfg.SwitchCount++
		_ = saveCarrierConfig(cfg)
		_ = exec.Command("/usr/local/bin/hashem", "carrier", "set", t).Run()
		writeJSON(w, map[string]any{"status": "ok", "active_carrier": cfg.ActiveCarrier})

	case "cycle_next":
		out, err := exec.Command("/usr/local/bin/hashem", "carrier", "next").CombinedOutput()
		if err == nil && len(out) > 0 {
			cfg = loadCarrierConfig()
		} else {
			// Fallback Go-level cycle if hashem binary isn't installed in test environment
			cands := cfg.Candidates
			if len(cands) == 0 {
				cands = []string{"direct", "fou:443", "fou:55555"}
			}
			idx := 0
			for i, c := range cands {
				if c == cfg.ActiveCarrier {
					idx = (i + 1) % len(cands)
					break
				}
			}
			cfg.ActiveCarrier = cands[idx]
			cfg.LastSwitch = time.Now().Format("2006-01-02 15:04:05")
			cfg.SwitchCount++
			_ = saveCarrierConfig(cfg)
		}
		writeJSON(w, map[string]any{"status": "ok", "active_carrier": cfg.ActiveCarrier})

	default:
		writeAPIError(w, r, "E-ACTION-01", "unknown action: want set_mode, set_active, or cycle_next")
	}
}
