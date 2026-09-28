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
	Mode          string   `json:"mode"`           // "auto", "direct", "fou:PORT", "wss:PORT"
	ActiveCarrier string   `json:"active_carrier"` // "direct", "fou:443", "fou:55555", "wss:8443"
	FOUPort1      int      `json:"fou_port1"`
	FOUPort2      int      `json:"fou_port2"`
	WSSPort       int      `json:"wss_port"`
	Candidates    []string `json:"candidates"`
	LastSwitch    string   `json:"last_switch"`
	SwitchCount   int      `json:"switch_count"`
}

type carrierStatusResponse struct {
	Mode          string           `json:"mode"`
	Active        string           `json:"active"`
	ActiveCarrier string           `json:"active_carrier"`
	FOUPort1      int              `json:"fou_port1"`
	FOUPort2      int              `json:"fou_port2"`
	WSSPort       int              `json:"wss_port"`
	FouPorts      []int            `json:"fou_ports"`
	Candidates    []string         `json:"candidates"`
	LastSwitch    string           `json:"last_switch"`
	SwitchCount   int              `json:"switch_count"`
	PingStatus    string           `json:"ping_status"`
	PingRTT       string           `json:"ping_rtt"`
	StatusText    string           `json:"status_text"`
	WSS           wssCarrierStatus `json:"wss"`
}

type carrierPostRequest struct {
	Action    string `json:"action"` // "set_mode", "set-mode", "set_active", "apply", "cycle_next", "cycle", "set-ports", "set_wss"
	Mode      string `json:"mode,omitempty"`
	Target    string `json:"target,omitempty"`
	FouPorts  []int  `json:"fou_ports,omitempty"`
	FOUPort1  int    `json:"fou_port1,omitempty"`
	FOUPort2  int    `json:"fou_port2,omitempty"`
	WSSPort   int    `json:"wss_port,omitempty"`
	WSSRemote string `json:"wss_remote,omitempty"`
	WSSSNI    string `json:"wss_sni,omitempty"`
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
		WSSPort:       8443,
		Candidates:    []string{"direct", "fou:443", "fou:55555", "wss:8443"},
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
	if c.WSSPort <= 0 {
		c.WSSPort = 8443
	}
	if len(c.Candidates) == 0 {
		c.Candidates = []string{"direct", "fou:" + strconv.Itoa(c.FOUPort1), "fou:" + strconv.Itoa(c.FOUPort2), "wss:" + strconv.Itoa(c.WSSPort)}
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
	statusText := "Active"
	if st == "fail" {
		statusText = "Packet Loss / Link Down"
	}
	resp := carrierStatusResponse{
		Mode:          cfg.Mode,
		Active:        cfg.ActiveCarrier,
		ActiveCarrier: cfg.ActiveCarrier,
		FOUPort1:      cfg.FOUPort1,
		FOUPort2:      cfg.FOUPort2,
		WSSPort:       cfg.WSSPort,
		FouPorts:      []int{cfg.FOUPort1, cfg.FOUPort2},
		Candidates:    cfg.Candidates,
		LastSwitch:    cfg.LastSwitch,
		SwitchCount:   cfg.SwitchCount,
		PingStatus:    st,
		PingRTT:       rtt,
		StatusText:    statusText,
		WSS:           getWSSStatus(),
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
	act := strings.ToLower(strings.TrimSpace(req.Action))

	switch act {
	case "set_mode", "set-mode":
		m := strings.TrimSpace(req.Mode)
		if m != "auto" && m != "direct" && !strings.HasPrefix(m, "fou:") && !strings.HasPrefix(m, "wss") {
			writeAPIError(w, r, "E-ACTION-01", "invalid mode: want auto, direct, fou:PORT, or wss:PORT")
			return
		}
		cfg.Mode = m
		if m != "auto" {
			cfg.ActiveCarrier = m
			cfg.LastSwitch = time.Now().Format("2006-01-02 15:04:05")
			cfg.SwitchCount++
			if strings.HasPrefix(m, "wss") {
				wssCfg := loadWSSConfig()
				wssCfg.Enabled = true
				_ = saveWSSConfig(wssCfg)
				_ = startWSSCarrier(wssCfg)
			} else {
				_ = stopWSSCarrier()
			}
			_ = exec.Command("/usr/local/bin/hashem", "carrier", "set", m).Run()
		} else {
			_ = exec.Command("/usr/local/bin/hashem", "carrier", "mode", "auto").Run()
		}
		_ = saveCarrierConfig(cfg)
		writeJSON(w, map[string]any{"status": "ok", "mode": cfg.Mode, "active": cfg.ActiveCarrier, "active_carrier": cfg.ActiveCarrier})

	case "set_active", "set-active", "apply", "set":
		t := strings.TrimSpace(req.Target)
		if t == "" && req.Mode != "" {
			t = strings.TrimSpace(req.Mode)
		}
		if t != "direct" && !strings.HasPrefix(t, "fou:") && !strings.HasPrefix(t, "wss") {
			t = cfg.ActiveCarrier
		}
		cfg.ActiveCarrier = t
		cfg.LastSwitch = time.Now().Format("2006-01-02 15:04:05")
		cfg.SwitchCount++
		if strings.HasPrefix(t, "wss") {
			wssCfg := loadWSSConfig()
			wssCfg.Enabled = true
			_ = saveWSSConfig(wssCfg)
			_ = startWSSCarrier(wssCfg)
		} else {
			_ = stopWSSCarrier()
		}
		_ = saveCarrierConfig(cfg)
		_ = exec.Command("/usr/local/bin/hashem", "carrier", "set", t).Run()
		writeJSON(w, map[string]any{"status": "ok", "active": cfg.ActiveCarrier, "active_carrier": cfg.ActiveCarrier, "detail": "Applied carrier " + t})

	case "cycle_next", "cycle", "next":
		out, err := exec.Command("/usr/local/bin/hashem", "carrier", "cycle").CombinedOutput()
		if err == nil && len(out) > 0 {
			cfg = loadCarrierConfig()
		} else {
			cands := cfg.Candidates
			if len(cands) == 0 {
				cands = []string{"direct", "fou:443", "fou:55555", "wss:8443"}
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
			if strings.HasPrefix(cfg.ActiveCarrier, "wss") {
				wssCfg := loadWSSConfig()
				wssCfg.Enabled = true
				_ = saveWSSConfig(wssCfg)
				_ = startWSSCarrier(wssCfg)
			} else {
				_ = stopWSSCarrier()
			}
			_ = saveCarrierConfig(cfg)
			_ = exec.Command("/usr/local/bin/hashem", "carrier", "set", cfg.ActiveCarrier).Run()
		}
		writeJSON(w, map[string]any{"status": "ok", "active": cfg.ActiveCarrier, "active_carrier": cfg.ActiveCarrier})

	case "set_ports", "set-ports":
		p1, p2 := req.FOUPort1, req.FOUPort2
		if len(req.FouPorts) >= 2 {
			p1, p2 = req.FouPorts[0], req.FouPorts[1]
		} else if len(req.FouPorts) == 1 {
			p1 = req.FouPorts[0]
			p2 = 55555
		}
		if p1 <= 0 || p1 > 65535 || p2 <= 0 || p2 > 65535 {
			writeAPIError(w, r, "E-ACTION-01", "invalid ports: must be between 1 and 65535")
			return
		}
		cfg.FOUPort1 = p1
		cfg.FOUPort2 = p2
		cfg.Candidates = []string{"direct", "fou:" + strconv.Itoa(p1), "fou:" + strconv.Itoa(p2), "wss:" + strconv.Itoa(cfg.WSSPort)}
		_ = saveCarrierConfig(cfg)
		_ = exec.Command("/usr/local/bin/hashem", "carrier", "set-ports", strconv.Itoa(p1), strconv.Itoa(p2)).Run()
		writeJSON(w, map[string]any{"status": "ok", "fou_port1": p1, "fou_port2": p2, "fou_ports": []int{p1, p2}})

	case "set_wss", "set-wss":
		wssCfg := loadWSSConfig()
		if req.WSSPort > 0 {
			wssCfg.ListenPort = req.WSSPort
			cfg.WSSPort = req.WSSPort
			_ = saveCarrierConfig(cfg)
		}
		if req.WSSRemote != "" {
			wssCfg.RemoteAddr = req.WSSRemote
		}
		if req.WSSSNI != "" {
			wssCfg.SNI = req.WSSSNI
		}
		_ = saveWSSConfig(wssCfg)
		if strings.HasPrefix(cfg.ActiveCarrier, "wss") {
			_ = startWSSCarrier(wssCfg)
		}
		writeJSON(w, map[string]any{"status": "ok", "wss": wssCfg})

	default:
		writeAPIError(w, r, "E-ACTION-01", "unknown action: want set_mode, set_active, cycle_next, set_ports, or set_wss")
	}
}
