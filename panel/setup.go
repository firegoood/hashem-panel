package main

// Setup API: validate the web form, then run the SAME hashem.sh install
// functions the CLI/menu use (single source of truth). The request fields
// map 1:1 to the setup-iran / setup-foreign CLI flags at the bottom of
// hashem.sh, and the installer script path is resolved next to the binary so it
// works both in dev (./hashem.sh) and on servers (/usr/local/bin/).
//
// Iran side: GRE + frps (token auto-generated, shown for copy to Foreign).
// Foreign side: GRE + frpc (token entered manually, ports like "443, 2083").
//
// Setup bundle: the Iran side also returns a single readable string holding
// everything the foreign side needs:
//   hsh1_<IRAN_PUB>_<FRP_PORT>_<IRAN_GRE>_<FOREIGN_GRE>_<TOKEN>[_<PORTS>]
// Pasting it into the foreign token field auto-fills the rest (explicit
// fields always win). Legacy 32-char tokens (no hsh1_ prefix) keep working.

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const (
	defaultIranGRE    = "10.10.10.2"
	defaultForeignGRE = "10.10.10.1"
	defaultFrpPortMin = 20000
	defaultFrpPortMax = 60000

	// bundlePrefix marks a single-string foreign-setup bundle:
	// hsh1_<IRAN_PUB>_<FRP_PORT>_<IRAN_GRE>_<FOREIGN_GRE>_<TOKEN>[_<PORTS>]
	bundlePrefix            = "hsh1_"
	bundlePrefixBackhaul    = "bh1_"
	bundlePrefixGreBackhaul = "gh1_"
)

// ---- hashem.sh location ----

// scriptPath finds the installer: HASHEM_SCRIPT env wins, legacy
// GRE_SCRIPT still accepted, else <bindir>/hashem.sh (servers: alongside
// /usr/local/bin/gre-panel), else legacy <bindir>/gre.sh or
// /usr/local/bin/gre.sh, else ./hashem.sh (repo dev).
// update_all() migrates servers to the new name; old paths are read-only
// fallbacks so already-installed servers never break.
func greScriptPath() (string, error) {
	if p := os.Getenv("HASHEM_SCRIPT"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
		return "", fmt.Errorf("HASHEM_SCRIPT=%s not found", p)
	}
	if p := os.Getenv("GRE_SCRIPT"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
		return "", fmt.Errorf("GRE_SCRIPT=%s not found", p)
	}
	if exe, err := os.Executable(); err == nil {
		if p := filepath.Join(filepath.Dir(exe), "hashem.sh"); fileExists(p) {
			return p, nil
		}
		if p := filepath.Join(filepath.Dir(exe), "gre.sh"); fileExists(p) {
			return p, nil
		}
	}
	if fileExists("/usr/local/bin/hashem.sh") {
		return "/usr/local/bin/hashem.sh", nil
	}
	if fileExists("/usr/local/bin/gre.sh") {
		return "/usr/local/bin/gre.sh", nil
	}
	if fileExists("/usr/local/bin/hashem") {
		if abs, err := filepath.Abs("/usr/local/bin/hashem"); err == nil {
			return abs, nil
		}
		return "/usr/local/bin/hashem", nil
	}
	if fileExists("hashem.sh") {
		if abs, err := filepath.Abs("hashem.sh"); err == nil {
			return abs, nil
		}
		return "hashem.sh", nil
	}
	if fileExists("gre.sh") {
		if abs, err := filepath.Abs("gre.sh"); err == nil {
			return abs, nil
		}
		return "gre.sh", nil
	}
	return "", fmt.Errorf("hashem.sh not found (set HASHEM_SCRIPT=/path/to/hashem.sh)")
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// greScriptTarget is where syncPanelScript writes the fresh script:
// same place greScriptPath() reads from (HASHEM_SCRIPT wins, else next to
// the running binary, else ./hashem.sh for panel/ dev).
func greScriptTarget() string {
	if p := os.Getenv("HASHEM_SCRIPT"); p != "" {
		return p
	}
	if p := os.Getenv("GRE_SCRIPT"); p != "" {
		return p
	}
	if exe, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(exe), panelScriptName)
	}
	return panelScriptName
}

// ensureFreshScript checks the script on disk supports recent features
// (add-peer CLI verb and --bundle flag in setup-foreign); if not (stale copy),
// it pulls the latest hashem.sh from main (bash -n verified) and replaces it — so
// setup requests self-heal instead of failing with E-INSTALL-02.
func ensureFreshScript(script string) {
	data, err := os.ReadFile(script)
	if err != nil {
		return
	}
	content := string(data)
	if strings.Contains(content, "add-peer") && strings.Contains(content, "--bundle") {
		return
	}
	syncPanelScript()
}

// ---- GET /api/setup: defaults + whether a tunnel already exists ----

func handleSetupGet(w http.ResponseWriter, r *http.Request) {
	st := localStatus()
	writeJSON(w, map[string]any{
		"local_public": detectPublicIP(),
		"iran_gre":     defaultIranGRE,
		"foreign_gre":  defaultForeignGRE,
		"frp_port":     randomFrpPort(),
		"role_guess":   st.Role,
		"exists":       tunnelExists(),
	})
}

func tunnelExists() bool {
	if out, err := exec.Command("ip", "tunnel", "show").CombinedOutput(); err == nil {
		if strings.Contains(string(out), "gre-tunnel") {
			return true
		}
	}
	if out, err := exec.Command("ip", "link", "show", "gre-tunnel").CombinedOutput(); err == nil {
		if strings.Contains(string(out), "gre-tunnel") {
			return true
		}
	}
	for _, f := range []string{"/etc/frp/frps.toml", "/etc/frp/frpc.toml"} {
		if _, err := os.Stat(f); err == nil {
			return true
		}
	}
	return false
}

func detectPublicIP() string {
	out, err := exec.Command("ip", "route", "get", "1.1.1.1").CombinedOutput()
	if err == nil {
		f := strings.Fields(string(out))
		for i, p := range f {
			if p == "src" && i+1 < len(f) {
				if net.ParseIP(f[i+1]) != nil {
					return f[i+1]
				}
			}
		}
	}
	return ""
}

// ---- POST /api/setup ----

type setupRequest struct {
	Role      string `json:"role"` // "iran" | "foreign" | "add-peer"
	Engine    string `json:"engine,omitempty"` // "" | "frp" | "backhaul" | "gre-backhaul"
	Transport string `json:"transport,omitempty"` // "tcpmux" (default), "tcp", "ws", "wss", "wsmux", "wssmux"
	Name      string `json:"name"` // add-peer label
	LocalPub  string `json:"local_public"`
	RemotePub string `json:"remote_public"`
	LocalGre  string `json:"local_gre"`
	PeerGre   string `json:"peer_gre"`
	FrpPort   int    `json:"frp_port"`
	Token     string `json:"token"` // foreign/add-peer (manual or auto)
	Ports     string `json:"ports"` // foreign/add-peer, e.g. "443, 2083, 8080"
	Force     bool   `json:"force"`
	Autogen   bool   `json:"autogen"` // add-peer: generate token server-side
	// OrigBundle holds the raw hsh1_... string when setup-foreign is invoked
	// via a bundle paste. applyBundle() fills every explicit field AND stores
	// the bundle here so runInstaller can pass --bundle to hashem.sh (which
	// handles carrier/FOU setup that the explicit flags do not cover).
	OrigBundle string `json:"-"` // internal; not sent by client
}

func handleSetupPost(w http.ResponseWriter, r *http.Request) {
	var body setupRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, r, "E-SETUP-01", "")
		return
	}
	body.LocalPub = strings.TrimSpace(body.LocalPub)
	body.RemotePub = strings.TrimSpace(body.RemotePub)
	body.LocalGre = strings.TrimSpace(body.LocalGre)
	body.PeerGre = strings.TrimSpace(body.PeerGre)
	body.Token = strings.TrimSpace(body.Token)
	body.Name = strings.TrimSpace(body.Name)

	// add-peer mode: each new foreign server gets its own token + tunnel.
	// Port conflicts (same remotePort on two peers) are rejected with 409
	// so the user picks another port instead of silently breaking a peer.
	if body.Role == "add-peer" {
		if body.Autogen || body.Token == "" {
			body.Token = randomToken(32)
		}
		maxPeers := 10
		if v := os.Getenv("GRE_MAX_PEERS"); v != "" {
			if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
				maxPeers = parsed
			}
		}
		if len(loadPeers()) >= maxPeers {
			writeAPIError(w, r, "E-PEER-01", "")
			return
		}
	}

	// bundle paste: a foreign-setup string in the token field auto-fills
	// whatever the user left empty (explicit fields always win).
	// Malformed bundles fail fast with E-SETUP-08, before field checks.
	if body.Role == "foreign" && isBundle(body.Token) {
		b, err := ParseBundle(body.Token)
		if err != nil {
			writeAPIError(w, r, "E-SETUP-08", "bad setup bundle: "+err.Error())
			return
		}
		// Preserve the original bundle string for runInstaller before
		// applyBundle() replaces body.Token with the inner token.
		body.OrigBundle = body.Token
		applyBundle(&body, b)
	}

	// validation (mirrors hashem.sh prompt_* / validate_setup_common rules)
	if body.Role != "iran" && body.Role != "foreign" && body.Role != "add-peer" {
		writeAPIError(w, r, "E-SETUP-02", "")
		return
	}
	if net.ParseIP(body.LocalPub) == nil {
		writeAPIError(w, r, "E-SETUP-03", "")
		return
	}
	if net.ParseIP(body.RemotePub) == nil {
		writeAPIError(w, r, "E-SETUP-04", "")
		return
	}
	if body.Engine != "backhaul" {
		if net.ParseIP(body.LocalGre) == nil || !isV4(body.LocalGre) {
			writeAPIError(w, r, "E-SETUP-05", "")
			return
		}
		if net.ParseIP(body.PeerGre) == nil || !isV4(body.PeerGre) {
			writeAPIError(w, r, "E-SETUP-06", "")
			return
		}
	}
	if body.FrpPort < 1 || body.FrpPort > 65535 {
		writeAPIError(w, r, "E-SETUP-07", "")
		return
	}

	var ports []int
	var rawPorts []string
	if body.Role == "foreign" || body.Role == "add-peer" {
		if body.Token == "" {
			writeAPIError(w, r, "E-SETUP-08", "")
			return
		}
		if !isBundle(body.Token) && len(body.Token) > 128 {
			writeAPIError(w, r, "E-SETUP-09", "")
			return
		}
		if isBundle(body.Token) && len(body.Token) > 256 {
			writeAPIError(w, r, "E-SETUP-09", "")
			return
		}
		if body.Engine == "backhaul" || body.Engine == "gre-backhaul" {
			rawPorts = parseRawPorts(body.Ports)
			ports = extractNumericPorts(rawPorts)
		} else {
			ports = parsePorts(body.Ports)
			for _, p := range ports {
				rawPorts = append(rawPorts, strconv.Itoa(p))
			}
		}
		if len(rawPorts) == 0 && len(ports) == 0 {
			writeAPIError(w, r, "E-SETUP-10", "")
			return
		}
	}

	// add-peer pre-check: reject ports another tunnel already serves (409 + peer name)
	if body.Role == "add-peer" {
		if clash := peerDuplicateIPClash(body.RemotePub, -1); clash != "" {
			writeAPIError(w, r, "E-PEER-04", "Foreign IP is already used by tunnel '"+clash+"' — you cannot add multiple tunnels to the exact same server")
			return
		}
		if clash := peerPortClash(ports, -1); clash != "" {
			writeAPIError(w, r, "E-PEER-02", "port "+clash+" is already served by another tunnel — pick a different port")
			return
		}
	}

	// overwrite guard: warn first, proceed only with force
	// add-peer never overwrites: it appends a new tunnel instead.
	if body.Role != "add-peer" && tunnelExists() && !body.Force {
		writeAPIError(w, r, "E-PEER-03", "tunnel already exists — resubmit with force:true to overwrite")
		return
	}

	// run the shared installer: GRE_SKIP_PANEL=1 because the panel is already
	// running here — reinstalling/downloading it mid-request would be slow and
	// could restart this very process.
	token, bundle, steps, err := runInstaller(body, ports, rawPorts)
	if err != nil {
		steps = append(steps, "FAILED: "+err.Error())
		code := "E-INSTALL-02"
		if strings.Contains(err.Error(), "not found") {
			code = "E-INSTALL-01"
		}
		info := errCatalog[code]
		recordError(code, r.Method+" "+r.URL.Path, err.Error())
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(info.Status)
		_ = json.NewEncoder(w).Encode(map[string]any{"error_code": code, "error": err.Error(), "hint": info.Hint, "steps": steps})
		return
	}
	out := map[string]any{"status": "ok", "steps": steps}
	if token != "" {
		out["token"] = token
	}
	if bundle != "" {
		out["bundle"] = bundle
	}
	writeJSON(w, out)
}

// runInstaller shells out to hashem.sh setup-iran|setup-foreign with the same
// flags the CLI uses, so menu / CLI / panel execute identical steps.
// Returns the Iran-side token + foreign-setup bundle (bundle holds the
// token plus all addresses/ports, so one paste configures foreign).
// ("", "", steps, nil) for foreign (nothing generated there).
func runInstaller(b setupRequest, ports []int, rawPorts []string) (string, string, []string, error) {
	script, err := greScriptPath()
	if err != nil {
		return "", "", nil, err
	}
	// Self-healing: if setup or add-peer is invoked on a host whose hashem.sh is
	// an old version (lacking the "add-peer" CLI verb or "--bundle"), auto-sync the
	// script before shelling out so the user does not hit E-INSTALL-02.
	ensureFreshScript(script)
	token := ""
	args := []string{}

	if b.Engine == "backhaul" {
		if b.Transport == "" {
			b.Transport = "tcpmux"
		}
		switch b.Role {
		case "add-peer":
			name := b.Name
			if name == "" {
				name = "peer"
			}
			token = b.Token
			args = []string{"add-backhaul-peer",
				"--name", name,
				"--no-gre",
				"--local-pub", b.LocalPub, "--remote-pub", b.RemotePub,
				"--port", strconv.Itoa(b.FrpPort),
				"--transport", b.Transport,
				"--token", token,
				"--ports", strings.Join(rawPorts, ","),
			}
		case "iran":
			token = randomToken(32)
			args = []string{"setup-backhaul-iran",
				"--local-pub", b.LocalPub, "--remote-pub", b.RemotePub,
				"--port", strconv.Itoa(b.FrpPort),
				"--transport", b.Transport,
				"--token", token,
			}
			if len(rawPorts) > 0 {
				args = append(args, "--ports", strings.Join(rawPorts, ","))
			}
		default: // foreign
			args = []string{"setup-backhaul-foreign",
				"--local-pub", b.LocalPub, "--remote-pub", b.RemotePub,
				"--port", strconv.Itoa(b.FrpPort),
				"--transport", b.Transport,
				"--token", b.Token,
			}
			if b.OrigBundle != "" {
				args = append(args, "--bundle", b.OrigBundle)
			}
		}
	} else if b.Engine == "gre-backhaul" {
		if b.Transport == "" {
			b.Transport = "tcpmux"
		}
		switch b.Role {
		case "add-peer":
			name := b.Name
			if name == "" {
				name = "peer"
			}
			token = b.Token
			args = []string{"add-backhaul-peer",
				"--name", name,
				"--local-pub", b.LocalPub, "--remote-pub", b.RemotePub,
				"--port", strconv.Itoa(b.FrpPort),
				"--local-gre", b.LocalGre, "--peer-gre", b.PeerGre,
				"--transport", b.Transport,
				"--token", token,
				"--ports", strings.Join(rawPorts, ","),
			}
		case "iran":
			token = randomToken(32)
			args = []string{"setup-gre-backhaul-iran",
				"--local-pub", b.LocalPub, "--remote-pub", b.RemotePub,
				"--port", strconv.Itoa(b.FrpPort),
				"--local-gre", b.LocalGre, "--peer-gre", b.PeerGre,
				"--transport", b.Transport,
				"--token", token,
			}
			if len(rawPorts) > 0 {
				args = append(args, "--ports", strings.Join(rawPorts, ","))
			}
		default: // foreign
			args = []string{"setup-gre-backhaul-foreign",
				"--local-pub", b.LocalPub, "--remote-pub", b.RemotePub,
				"--port", strconv.Itoa(b.FrpPort),
				"--local-gre", b.LocalGre, "--peer-gre", b.PeerGre,
				"--transport", b.Transport,
				"--token", b.Token,
			}
			if b.OrigBundle != "" {
				args = append(args, "--bundle", b.OrigBundle)
			}
		}
	} else {
		switch b.Role {
		case "add-peer":
			name := b.Name
			if name == "" {
				name = "peer"
			}
			strs := make([]string, len(ports))
			for i, q := range ports {
				strs[i] = strconv.Itoa(q)
			}
			token = b.Token
			args = []string{"add-peer",
				"--name", name,
				"--local-pub", b.LocalPub, "--remote-pub", b.RemotePub,
				"--frp-port", strconv.Itoa(b.FrpPort),
				"--local-gre", b.LocalGre, "--peer-gre", b.PeerGre,
				"--token", token,
				"--ports", strings.Join(strs, ","),
			}
		case "iran":
			token = randomToken(32)
			args = []string{"setup-iran",
				"--local-pub", b.LocalPub, "--remote-pub", b.RemotePub,
				"--frp-port", strconv.Itoa(b.FrpPort),
				"--local-gre", b.LocalGre, "--peer-gre", b.PeerGre,
				"--token", token,
			}
		default:
			strs := make([]string, len(ports))
			for i, p := range ports {
				strs[i] = strconv.Itoa(p)
			}
			args = []string{"setup-foreign",
				"--local-pub", b.LocalPub, "--remote-pub", b.RemotePub,
				"--frp-port", strconv.Itoa(b.FrpPort),
				"--local-gre", b.LocalGre, "--peer-gre", b.PeerGre,
				"--token", b.Token,
				"--ports", strings.Join(strs, ","),
			}
			// Pass --bundle when available so hashem.sh can configure carrier/FOU
			// ports (carrier_set_fou_ports) that are not covered by explicit flags.
			if b.OrigBundle != "" {
				args = append(args, "--bundle", b.OrigBundle)
			} else if isBundle(b.Token) {
				// Fallback: if applyBundle was not called (old client path)
				args = append(args, "--bundle", b.Token)
			}
		}
	}
	if b.Force {
		args = append(args, "--force")
	}
	// token is used verbatim as an argv element (no shell), safe from injection.
	cmd := exec.Command("bash", append([]string{script}, args...)...)
	cmd.Env = append(os.Environ(), "GRE_SKIP_PANEL=1")
	out, runErr := cmd.CombinedOutput()
	steps := []string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			steps = append(steps, line)
		}
	}
	if b.Role == "iran" || b.Role == "add-peer" {
		steps = append([]string{"token generated (copy to Foreign side)"}, steps...)
	}
	if runErr != nil {
		errText := runErr.Error()
		if len(steps) > 0 {
			if tail := strings.Join(steps[max(0, len(steps)-3):], " | "); tail != "" {
				errText += " — " + tail
			}
		}
		if b.Role == "add-peer" && strings.Contains(strings.Join(steps, "\n"), "Unknown command") {
			errText += " — panel hashem.sh is an old version: run Update to latest, or re-run install.sh on this host"
		}
		return "", "", steps, fmt.Errorf("hashem.sh %s failed: %s", args[0], errText)
	}
	if b.Role == "iran" || b.Role == "add-peer" {
		var bundleStr string
		if b.Engine == "backhaul" {
			bundleStr = MakeBackhaulBundle(b.LocalPub, b.FrpPort, b.Transport, token, rawPorts)
		} else if b.Engine == "gre-backhaul" {
			bundleStr = MakeGreBackhaulBundle(b.LocalPub, b.FrpPort, b.LocalGre, b.PeerGre, b.Transport, token, rawPorts)
		} else {
			bundleStr = MakeBundle(b.LocalPub, b.FrpPort, b.LocalGre, b.PeerGre, token, ports)
		}
		return token, bundleStr, steps, nil
	}
	return "", "", steps, nil
}

// ---- peers API: list tunnels + token lookup ----

func handlePeersGet(w http.ResponseWriter, r *http.Request) {
	if idStr := r.URL.Query().Get("id"); idStr != "" {
		id, err := strconv.Atoi(idStr)
		if err != nil {
			writeAPIError(w, r, "E-PEER-05", "")
			return
		}
		script, err := greScriptPath()
		if err != nil {
			writeAPIError(w, r, "E-INSTALL-01", err.Error())
			return
		}
		cmd := exec.Command("bash", script, "peer-token", "--id", strconv.Itoa(id))
		cmd.Env = append(os.Environ(), "GRE_SKIP_PANEL=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			writeAPIError(w, r, "E-PEER-04", strings.TrimSpace(string(out)))
			return
		}
		resp := map[string]string{"id": idStr}
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		resp["token"] = strings.TrimSpace(lines[0])
		for _, ln := range lines[1:] {
			if b, ok := strings.CutPrefix(strings.TrimSpace(ln), "BUNDLE:"); ok {
				resp["bundle"] = strings.TrimSpace(b)
			}
		}
		if _, ok := resp["bundle"]; !ok {
			// old hashem.sh prints token only: rebuild the bundle from the
			// registry record (live Iran pub + port fill the rest).
			if p := findPeer(id); p != nil {
				resp["bundle"] = MakeBundle(p.LocalPub, p.FrpPort, p.LocalGre, p.PeerGre, resp["token"], p.Ports)
			}
		}
		writeJSON(w, resp)
		return
	}
	peers := livePeers()
	if peers == nil {
		peers = []peerLive{}
	}
	writeJSON(w, map[string]any{"peers": peers, "peer_count": len(peers), "max": 5})
}

// POST /api/peers just proxies validation errors from /api/setup role=add-peer.
// The real creation path is /api/setup (role add-peer) so only one code path
// shells out to hashem.sh. This endpoint exists for future per-peer edits.
func handlePeersPost(w http.ResponseWriter, r *http.Request) {
	writeAPIError(w, r, "E-PEER-06", "")
}

// PATCH /api/peers — edit the configuration of an existing peer tunnel or main tunnel.
// Body: { "id": N, "name": "...", "remote_pub": "...", "carrier": "...", "ports": [443, 2083] }
// Strategy (graceful degradation):
//   1. Try hashem.sh edit-peer --id N ... (or edit-peer-ports).
//   2. Fallback to direct edit: update peers.json, adjust kernel GRE remote endpoint,
//      persist systemd unit, apply carrier mode, rewrite TOML proxy blocks, reload service, and allow UFW.
type peerPatchRequest struct {
	ID        int       `json:"id"`
	Name      string    `json:"name,omitempty"`
	RemotePub string    `json:"remote_pub,omitempty"`
	Carrier   string    `json:"carrier,omitempty"`
	Engine    string    `json:"engine,omitempty"`
	Transport string    `json:"transport,omitempty"`
	Ports     *[]int    `json:"ports,omitempty"`
	RawPorts  *[]string `json:"raw_ports,omitempty"`
}

func handlePeersPatch(w http.ResponseWriter, r *http.Request) {
	var body peerPatchRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, r, "E-PEER-07", "bad request body")
		return
	}
	if body.ID < 0 {
		writeAPIError(w, r, "E-PEER-07", "id must be >= 0")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	body.RemotePub = strings.TrimSpace(body.RemotePub)
	body.Carrier = strings.TrimSpace(body.Carrier)
	body.Engine = strings.TrimSpace(body.Engine)
	body.Transport = strings.TrimSpace(body.Transport)

	// Explicit empty ports array: {"ports": []} or {"raw_ports": []} is rejected
	if body.Ports != nil && len(*body.Ports) == 0 {
		writeAPIError(w, r, "E-PEER-07", "ports list is empty")
		return
	}
	if body.RawPorts != nil && len(*body.RawPorts) == 0 {
		writeAPIError(w, r, "E-PEER-07", "raw_ports list is empty")
		return
	}

	// Must provide at least one field to update
	if body.Name == "" && body.RemotePub == "" && body.Carrier == "" && body.Engine == "" && body.Transport == "" && body.Ports == nil && body.RawPorts == nil {
		writeAPIError(w, r, "E-PEER-07", "no fields to update")
		return
	}

	// Validate raw ports if provided
	if body.RawPorts != nil {
		for _, rp := range *body.RawPorts {
			if !isValidPortOrRangeOrMapping(rp) {
				writeAPIError(w, r, "E-PEER-07", fmt.Sprintf("invalid port/range/mapping: %s", rp))
				return
			}
		}
		if body.Ports == nil {
			extracted := extractNumericPorts(*body.RawPorts)
			body.Ports = &extracted
		}
	}

	// Validate remote public IP if provided
	if body.RemotePub != "" {
		if !isV4(body.RemotePub) {
			writeAPIError(w, r, "E-PEER-07", "invalid remote public IP")
			return
		}
		if clash := peerDuplicateIPClash(body.RemotePub, body.ID); clash != "" {
			writeAPIError(w, r, "E-PEER-03", "IP "+body.RemotePub+" already used by peer "+clash)
			return
		}
	}

	// Validate carrier if provided
	if body.Carrier != "" {
		c := strings.ToLower(body.Carrier)
		if c != "direct" && !strings.HasPrefix(c, "fou:") && !strings.HasPrefix(c, "wss:") && c != "fou" && c != "wss" {
			writeAPIError(w, r, "E-PEER-07", "invalid carrier mode (must be direct, fou:PORT, or wss:PORT)")
			return
		}
	}

	// Validate ports if provided
	if body.Ports != nil {
		for _, p := range *body.Ports {
			if p < 1 || p > 65535 {
				writeAPIError(w, r, "E-PEER-07", fmt.Sprintf("invalid port %d", p))
				return
			}
		}
	}

	// Case 1: ID == 0 -> Edit Main / Base Tunnel
	if body.ID == 0 {
		excludeID := 0
		var legacyPeer *peerRecord
		for _, p := range loadPeers() {
			if p.Legacy || p.ID == 1 {
				excludeID = p.ID
				c := p
				legacyPeer = &c
				break
			}
		}
		if body.Ports != nil {
			if clash := peerPortClash(*body.Ports, excludeID); clash != "" {
				writeAPIError(w, r, "E-PEER-02", "port "+clash+" is already served by another tunnel")
				return
			}
		}
		warnMsg, err := editMainTunnelDirect(body, legacyPeer)
		if err != nil {
			writeAPIError(w, r, "E-PEER-07", err.Error())
			return
		}
		bundle := generateMainBundle(body, legacyPeer)
		resp := map[string]any{"status": "ok", "output": "main tunnel configuration updated"}
		if bundle != "" {
			resp["bundle"] = bundle
		}
		if warnMsg != "" {
			resp["warning"] = warnMsg
		}
		writeJSON(w, resp)
		return
	}

	// Case 2: ID > 0 -> Edit Peer Tunnel
	peer := findPeer(body.ID)
	if peer == nil {
		writeAPIError(w, r, "E-PEER-05", fmt.Sprintf("peer %d not found", body.ID))
		return
	}
	if body.Ports != nil {
		if clash := peerPortClash(*body.Ports, body.ID); clash != "" {
			writeAPIError(w, r, "E-PEER-02", "port "+clash+" is already served by another tunnel")
			return
		}
	}

	// 1. Try installer if it supports edit-peer or edit-peer-ports
	if out, err := editPeerViaInstaller(body); err == nil {
		bundle := generatePeerBundle(peer, body)
		resp := map[string]any{"status": "ok", "output": out}
		if bundle != "" {
			resp["bundle"] = bundle
		}
		writeJSON(w, resp)
		return
	}

	// 2. Direct edit: update peers.json, reconfigure GRE endpoint, systemd unit, carrier, toml, firewall
	warnMsg, err := editPeerDirect(peer, body)
	if err != nil {
		writeAPIError(w, r, "E-PEER-07", err.Error())
		return
	}
	bundle := generatePeerBundle(peer, body)
	resp := map[string]any{"status": "ok", "output": fmt.Sprintf("peer %d configuration updated", body.ID)}
	if bundle != "" {
		resp["bundle"] = bundle
	}
	if warnMsg != "" {
		resp["warning"] = warnMsg
	}
	writeJSON(w, resp)
}

// editPeerViaInstaller tries the installer subcommand for peer editing.
func editPeerViaInstaller(body peerPatchRequest) (string, error) {
	script, err := greScriptPath()
	if err != nil {
		return "", err
	}
	args := []string{script, "edit-peer", "--id", strconv.Itoa(body.ID)}
	if body.Name != "" {
		args = append(args, "--name", body.Name)
	}
	if body.RemotePub != "" {
		args = append(args, "--remote-pub", body.RemotePub)
	}
	if body.Carrier != "" {
		args = append(args, "--carrier", body.Carrier)
	}
	if body.Ports != nil {
		strs := make([]string, len(*body.Ports))
		for i, p := range *body.Ports {
			strs[i] = strconv.Itoa(p)
		}
		args = append(args, "--ports", strings.Join(strs, ","))
	}
	cmd := exec.Command("bash", args...)
	cmd.Env = append(os.Environ(), "GRE_SKIP_PANEL=1")
	out, runErr := cmd.CombinedOutput()
	o := strings.TrimSpace(stripANSI(string(out)))
	if runErr != nil {
		lower := strings.ToLower(o)
		if strings.Contains(lower, "unknown command") || strings.Contains(lower, "unknown flag") {
			// If edit-peer wasn't found but only ports are being updated, try legacy edit-peer-ports
			if body.Ports != nil && body.Name == "" && body.RemotePub == "" && body.Carrier == "" {
				strs := make([]string, len(*body.Ports))
				for i, p := range *body.Ports {
					strs[i] = strconv.Itoa(p)
				}
				cmdLegacy := exec.Command("bash", script, "edit-peer-ports", "--id", strconv.Itoa(body.ID), "--ports", strings.Join(strs, ","))
				cmdLegacy.Env = append(os.Environ(), "GRE_SKIP_PANEL=1")
				outLegacy, errLegacy := cmdLegacy.CombinedOutput()
				if errLegacy == nil {
					return strings.TrimSpace(stripANSI(string(outLegacy))), nil
				}
			}
			return "", fmt.Errorf("installer lacks edit-peer")
		}
		return o, runErr
	}
	return o, nil
}

// editPeerDirect updates peers.json, GRE remote endpoint, systemd unit, carrier, and TOML proxy blocks.
func editPeerDirect(peer *peerRecord, req peerPatchRequest) (string, error) {
	var warning string

	// 1. Rewrite peers.json
	peers := loadPeers()
	for i := range peers {
		if peers[i].ID == peer.ID {
			if req.Name != "" {
				peers[i].Name = req.Name
			}
			if req.RemotePub != "" {
				peers[i].RemotePub = req.RemotePub
			}
			if req.Carrier != "" {
				peers[i].Carrier = req.Carrier
			}
			if req.Engine != "" {
				peers[i].Engine = req.Engine
			}
			if req.Transport != "" {
				peers[i].Transport = req.Transport
			}
			if req.RawPorts != nil {
				peers[i].RawPorts = *req.RawPorts
				peers[i].Ports = extractNumericPorts(*req.RawPorts)
			} else if req.Ports != nil {
				peers[i].Ports = *req.Ports
				var rps []string
				for _, p := range *req.Ports {
					rps = append(rps, strconv.Itoa(p))
				}
				peers[i].RawPorts = rps
			}
		}
	}
	data, err := json.MarshalIndent(map[string]any{"peers": peers}, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal peers: %w", err)
	}
	if err := os.WriteFile(peersFile(), append(data, '\n'), 0600); err != nil {
		return "", fmt.Errorf("write peers.json: %w", err)
	}

	greIf := peer.GreIf
	if greIf == "" {
		if peer.ID > 1 {
			greIf = fmt.Sprintf("gre-t%d", peer.ID)
		} else {
			greIf = "gre-tunnel"
		}
	}

	// 2. Reconfigure GRE remote endpoint if remote_pub changed (only if not standalone Backhaul)
	if !peer.NoGre && peer.Engine != "backhaul" && req.RemotePub != "" && req.RemotePub != peer.RemotePub {
		exec.Command("ip", "tunnel", "change", greIf, "remote", req.RemotePub).CombinedOutput()
		svcFile := fmt.Sprintf("/etc/systemd/system/%s.service", greIf)
		updateServiceRemoteIP(svcFile, peer.RemotePub, req.RemotePub)
		exec.Command("systemctl", "daemon-reload").CombinedOutput()
		exec.Command("systemctl", "restart", greIf+".service").CombinedOutput()

		if !testPing(req.RemotePub, 2) {
			warning = fmt.Sprintf("New remote IP %s did not reply to ping (peer may be offline or firewalling ICMP)", req.RemotePub)
		}
	}

	// 3. Apply carrier if changed
	if req.Carrier != "" && req.Carrier != peer.Carrier {
		_ = applyPeerCarrierDirect(greIf, req.Carrier)
	}

	// 4. Update ports if changed
	if req.RawPorts != nil || req.Ports != nil {
		var rawList []string
		if req.RawPorts != nil {
			rawList = *req.RawPorts
		} else if req.Ports != nil {
			for _, p := range *req.Ports {
				rawList = append(rawList, strconv.Itoa(p))
			}
		}
		var numList []int
		if req.Ports != nil {
			numList = *req.Ports
		} else {
			numList = extractNumericPorts(rawList)
		}

		if peer.Engine == "backhaul" || peer.Engine == "gre-backhaul" {
			tomlPath := fmt.Sprintf("/etc/backhaul/server-%d.toml", peer.ID)
			if _, err := os.Stat(tomlPath); err != nil && peer.ID <= 1 {
				tomlPath = "/etc/backhaul/config.toml"
			}
			if rawToml, err := os.ReadFile(tomlPath); err == nil {
				updated := rewriteBackhaulTomlPorts(string(rawToml), rawList)
				_ = os.WriteFile(tomlPath, []byte(updated), 0644)
			}
			svc := peer.FrpsSvc
			if svc == "" {
				if peer.ID > 1 {
					svc = fmt.Sprintf("backhaul-server-%d", peer.ID)
				} else {
					svc = "backhaul-server"
				}
			}
			exec.Command("systemctl", "reload-or-restart", svc).CombinedOutput()
			allowUFWPorts(numList)
		} else {
			tomlPath := fmt.Sprintf("/etc/frp/frps-%d.toml", peer.ID)
			if _, err := os.Stat(tomlPath); err != nil && peer.ID == 1 {
				tomlPath = "/etc/frp/frps.toml"
			}
			if rawToml, err := os.ReadFile(tomlPath); err == nil {
				updated := rewriteTomlPorts(string(rawToml), numList)
				_ = os.WriteFile(tomlPath, []byte(updated), 0644)
			}
			svc := peer.FrpsSvc
			if svc == "" {
				if peer.ID > 1 {
					svc = fmt.Sprintf("frps-%d", peer.ID)
				} else {
					svc = "frps"
				}
			}
			exec.Command("systemctl", "reload-or-restart", svc).CombinedOutput()
			allowUFWPorts(numList)
		}
	}

	return warning, nil
}

// editMainTunnelDirect updates configuration for the main tunnel (id 0).
func editMainTunnelDirect(req peerPatchRequest, legacyPeer *peerRecord) (string, error) {
	var warning string

	// 1. If legacy peer exists in peers.json, update it
	if legacyPeer != nil {
		peers := loadPeers()
		for i := range peers {
			if peers[i].Legacy || peers[i].ID == 1 {
				if req.Name != "" {
					peers[i].Name = req.Name
				}
				if req.RemotePub != "" {
					peers[i].RemotePub = req.RemotePub
				}
				if req.Carrier != "" {
					peers[i].Carrier = req.Carrier
				}
				if req.Engine != "" {
					peers[i].Engine = req.Engine
				}
				if req.Transport != "" {
					peers[i].Transport = req.Transport
				}
				if req.RawPorts != nil {
					peers[i].RawPorts = *req.RawPorts
					peers[i].Ports = extractNumericPorts(*req.RawPorts)
				} else if req.Ports != nil {
					peers[i].Ports = *req.Ports
					var rps []string
					for _, p := range *req.Ports {
						rps = append(rps, strconv.Itoa(p))
					}
					peers[i].RawPorts = rps
				}
			}
		}
		if data, err := json.MarshalIndent(map[string]any{"peers": peers}, "", "  "); err == nil {
			_ = os.WriteFile(peersFile(), append(data, '\n'), 0600)
		}
	}

	// 2. Remote IP change for gre-tunnel
	if req.RemotePub != "" {
		exec.Command("ip", "tunnel", "change", "gre-tunnel", "remote", req.RemotePub).CombinedOutput()
		oldIP := ""
		if legacyPeer != nil {
			oldIP = legacyPeer.RemotePub
		}
		updateServiceRemoteIP("/etc/systemd/system/gre-tunnel.service", oldIP, req.RemotePub)
		exec.Command("systemctl", "daemon-reload").CombinedOutput()
		exec.Command("systemctl", "restart", "gre-tunnel.service").CombinedOutput()

		if !testPing(req.RemotePub, 2) {
			warning = fmt.Sprintf("New remote IP %s did not reply to ping", req.RemotePub)
		}
	}

	// 3. Carrier mode change
	if req.Carrier != "" {
		_ = applyPeerCarrierDirect("gre-tunnel", req.Carrier)
		_, _ = runHashemCarrierCmd("set", req.Carrier)
	}

	// 4. Ports change
	if req.RawPorts != nil {
		if rawToml, err := os.ReadFile("/etc/backhaul/config.toml"); err == nil {
			updated := rewriteBackhaulTomlPorts(string(rawToml), *req.RawPorts)
			_ = os.WriteFile("/etc/backhaul/config.toml", []byte(updated), 0644)
			exec.Command("systemctl", "reload-or-restart", "backhaul-server").CombinedOutput()
			allowUFWPorts(extractNumericPorts(*req.RawPorts))
		}
	}
	if req.Ports != nil {
		_ = editMainTunnelPortsDirect(*req.Ports)
	}

	return warning, nil
}

func generatePeerBundle(peer *peerRecord, req peerPatchRequest) string {
	if peer == nil {
		return ""
	}
	localPub := peer.LocalPub
	if localPub == "" {
		localPub = detectPublicIP()
	}
	ports := peer.Ports
	if req.Ports != nil {
		ports = *req.Ports
	}
	rawPorts := peer.RawPorts
	if req.RawPorts != nil {
		rawPorts = *req.RawPorts
	} else if len(rawPorts) == 0 && len(ports) > 0 {
		rawPorts = make([]string, len(ports))
		for i, p := range ports {
			rawPorts[i] = strconv.Itoa(p)
		}
	}
	token := peer.Token
	if token == "" {
		tomlPath := fmt.Sprintf("/etc/frp/frps-%d.toml", peer.ID)
		if _, err := os.Stat(tomlPath); err != nil && peer.ID == 1 {
			tomlPath = "/etc/frp/frps.toml"
		}
		if raw, err := os.ReadFile(tomlPath); err == nil {
			for _, line := range strings.Split(string(raw), "\n") {
				if strings.Contains(line, "auth.token") || strings.Contains(line, "token =") {
					parts := strings.Split(line, "=")
					if len(parts) == 2 {
						token = strings.Trim(strings.TrimSpace(parts[1]), `"'`)
					}
				}
			}
		}
		if token == "" {
			bhPath := fmt.Sprintf("/etc/backhaul/server-%d.toml", peer.ID)
			if _, err := os.Stat(bhPath); err != nil && peer.ID == 1 {
				bhPath = "/etc/backhaul/config.toml"
			}
			if raw, err := os.ReadFile(bhPath); err == nil {
				for _, line := range strings.Split(string(raw), "\n") {
					if strings.Contains(line, "token =") {
						parts := strings.Split(line, "=")
						if len(parts) == 2 {
							token = strings.Trim(strings.TrimSpace(parts[1]), `"'`)
						}
					}
				}
			}
		}
	}
	transport := peer.Transport
	if req.Transport != "" {
		transport = req.Transport
	}
	engine := peer.Engine
	if req.Engine != "" {
		engine = req.Engine
	}

	if localPub == "" || token == "" || peer.FrpPort <= 0 {
		return ""
	}
	if engine == "backhaul" {
		return MakeBackhaulBundle(localPub, peer.FrpPort, transport, token, rawPorts)
	}
	if engine == "gre-backhaul" {
		if peer.LocalGre == "" || peer.PeerGre == "" {
			return ""
		}
		return MakeGreBackhaulBundle(localPub, peer.FrpPort, peer.LocalGre, peer.PeerGre, transport, token, rawPorts)
	}
	if peer.LocalGre == "" || peer.PeerGre == "" {
		return ""
	}
	return MakeBundle(localPub, peer.FrpPort, peer.LocalGre, peer.PeerGre, token, ports)
}

func generateMainBundle(req peerPatchRequest, legacyPeer *peerRecord) string {
	iranPub := detectPublicIP()
	frpPort := 7000
	iranGre := defaultIranGRE
	foreignGre := defaultForeignGRE
	token := ""
	engine := ""
	transport := "tcpmux"
	var ports []int
	var rawPorts []string
	if req.Ports != nil {
		ports = *req.Ports
	}
	if req.RawPorts != nil {
		rawPorts = *req.RawPorts
	}
	if legacyPeer != nil {
		if legacyPeer.LocalPub != "" {
			iranPub = legacyPeer.LocalPub
		}
		if legacyPeer.FrpPort > 0 {
			frpPort = legacyPeer.FrpPort
		}
		if legacyPeer.LocalGre != "" {
			iranGre = legacyPeer.LocalGre
		}
		if legacyPeer.PeerGre != "" {
			foreignGre = legacyPeer.PeerGre
		}
		token = legacyPeer.Token
		engine = legacyPeer.Engine
		if legacyPeer.Transport != "" {
			transport = legacyPeer.Transport
		}
		if len(ports) == 0 {
			ports = legacyPeer.Ports
		}
		if len(rawPorts) == 0 {
			rawPorts = legacyPeer.RawPorts
		}
	}
	if len(rawPorts) == 0 && len(ports) > 0 {
		rawPorts = make([]string, len(ports))
		for i, p := range ports {
			rawPorts[i] = strconv.Itoa(p)
		}
	}
	if token == "" {
		if raw, err := os.ReadFile("/etc/frp/frps.toml"); err == nil {
			for _, line := range strings.Split(string(raw), "\n") {
				if strings.Contains(line, "auth.token") || strings.Contains(line, "token =") {
					parts := strings.Split(line, "=")
					if len(parts) == 2 {
						token = strings.Trim(strings.TrimSpace(parts[1]), `"'`)
					}
				}
			}
		}
		if token == "" {
			if raw, err := os.ReadFile("/etc/backhaul/config.toml"); err == nil {
				for _, line := range strings.Split(string(raw), "\n") {
					if strings.Contains(line, "token =") {
						parts := strings.Split(line, "=")
						if len(parts) == 2 {
							token = strings.Trim(strings.TrimSpace(parts[1]), `"'`)
						}
					}
				}
			}
		}
	}
	if iranPub == "" || token == "" {
		return ""
	}
	if engine == "backhaul" {
		return MakeBackhaulBundle(iranPub, frpPort, transport, token, rawPorts)
	}
	if engine == "gre-backhaul" {
		return MakeGreBackhaulBundle(iranPub, frpPort, iranGre, foreignGre, transport, token, rawPorts)
	}
	return MakeBundle(iranPub, frpPort, iranGre, foreignGre, token, ports)
}

func applyPeerCarrierDirect(greIf string, carrier string) error {
	script, err := greScriptPath()
	if err == nil {
		cmd := exec.Command("bash", script, "carrier", "set", carrier)
		cmd.Env = append(os.Environ(), "GRE_SKIP_PANEL=1")
		_ = cmd.Run()
	}
	if carrier == "direct" {
		exec.Command("ip", "link", "set", "dev", greIf, "type", "gre", "encap", "none").CombinedOutput()
	} else if strings.HasPrefix(carrier, "fou:") {
		port := strings.TrimPrefix(carrier, "fou:")
		exec.Command("ip", "fou", "add", "port", port, "ipproto", "47").CombinedOutput()
		exec.Command("ip", "link", "set", "dev", greIf, "type", "gre", "encap", "fou", "encap-sport", "auto", "encap-dport", port).CombinedOutput()
	} else if strings.HasPrefix(carrier, "wss") {
		exec.Command("ip", "fou", "add", "port", "19998", "ipproto", "47").CombinedOutput()
		exec.Command("ip", "link", "set", "dev", greIf, "type", "gre", "encap", "fou", "encap-sport", "auto", "encap-dport", "19998").CombinedOutput()
	}
	return nil
}

func updateServiceRemoteIP(serviceFile string, oldIP string, newIP string) {
	data, err := os.ReadFile(serviceFile)
	if err != nil {
		return
	}
	content := string(data)
	if oldIP != "" && strings.Contains(content, oldIP) {
		content = strings.ReplaceAll(content, oldIP, newIP)
	} else {
		re := regexp.MustCompile(`remote\s+\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}`)
		content = re.ReplaceAllString(content, "remote "+newIP)
	}
	_ = os.WriteFile(serviceFile, []byte(content), 0644)
}

func testPing(ip string, timeoutSec int) bool {
	if ip == "" {
		return false
	}
	cmd := exec.Command("ping", "-c", "1", "-W", strconv.Itoa(timeoutSec), ip)
	return cmd.Run() == nil
}

// editPeerPortsDirect updates peers.json and rewrites the frps-N.toml [[proxies]]
// blocks in place, then reloads frps via systemctl so ports take effect immediately.
func editPeerPortsDirect(peer *peerRecord, newPorts []int) error {
	portsPtr := &newPorts
	_, err := editPeerDirect(peer, peerPatchRequest{ID: peer.ID, Ports: portsPtr})
	return err
}


// editMainTunnelPortsDirect updates configuration for the main tunnel (id 0)
// across peers.json (if legacy peer 1 exists), /etc/frp/frpc.toml, /etc/frp/frps.toml,
// and system firewall.
func editMainTunnelPortsDirect(newPorts []int) error {
	editedAny := false

	// 1. If legacy peer exists in peers.json, update it
	peers := loadPeers()
	for i := range peers {
		if peers[i].Legacy || peers[i].ID == 1 {
			peers[i].Ports = newPorts
			editedAny = true
		}
	}
	if editedAny {
		data, err := json.MarshalIndent(map[string]any{"peers": peers}, "", "  ")
		if err == nil {
			_ = os.WriteFile(peersFile(), append(data, '\n'), 0600)
		}
	}

	// 2. Foreign client toml (/etc/frp/frpc.toml)
	if rawToml, err := os.ReadFile("/etc/frp/frpc.toml"); err == nil {
		updated := rewriteTomlPorts(string(rawToml), newPorts)
		_ = os.WriteFile("/etc/frp/frpc.toml", []byte(updated), 0644)
		exec.Command("systemctl", "reload-or-restart", "frpc").CombinedOutput()
		editedAny = true
	}

	// 3. Server toml (/etc/frp/frps.toml)
	if rawToml, err := os.ReadFile("/etc/frp/frps.toml"); err == nil {
		if strings.Contains(string(rawToml), "[[proxies]]") {
			updated := rewriteTomlPorts(string(rawToml), newPorts)
			_ = os.WriteFile("/etc/frp/frps.toml", []byte(updated), 0644)
		}
		exec.Command("systemctl", "reload-or-restart", "frps").CombinedOutput()
		editedAny = true
	}

	// 4. Open ports in UFW if active
	allowUFWPorts(newPorts)

	if !editedAny {
		// Still create a minimal peers record or return ok if it was applied
		return nil
	}
	return nil
}

// allowUFWPorts opens both tcp and udp ports in UFW firewall if UFW is active.
func allowUFWPorts(ports []int) {
	if out, err := exec.Command("ufw", "status").CombinedOutput(); err == nil && strings.Contains(string(out), "Status: active") {
		for _, p := range ports {
			exec.Command("ufw", "allow", fmt.Sprintf("%d/tcp", p)).CombinedOutput()
			exec.Command("ufw", "allow", fmt.Sprintf("%d/udp", p)).CombinedOutput()
		}
	}
}

// rewriteTomlPorts rebuilds the [[proxies]] sections of an frps/frpc TOML file
// with the new port list. Non-proxy lines (header, [server], bindPort, etc.)
// are kept verbatim; only the [[proxies]] blocks are replaced.
func rewriteTomlPorts(src string, ports []int) string {
	var header strings.Builder
	inProxy := false
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[[proxies]]") {
			inProxy = true
			continue
		}
		if strings.HasPrefix(trimmed, "[") && !strings.HasPrefix(trimmed, "[[proxies]]") {
			inProxy = false
		}
		if !inProxy {
			header.WriteString(line)
			header.WriteByte('\n')
		}
	}
	result := strings.TrimRight(header.String(), "\n") + "\n"
	for _, port := range ports {
		result += fmt.Sprintf("\n[[proxies]]\nname = \"tcp_%d\"\ntype = \"tcp\"\nlocalIP = \"127.0.0.1\"\nlocalPort = %d\nremotePort = %d\n\n[[proxies]]\nname = \"udp_%d\"\ntype = \"udp\"\nlocalIP = \"127.0.0.1\"\nlocalPort = %d\nremotePort = %d\n", port, port, port, port, port, port)
	}
	return result
}

func isV4(s string) bool {
	ip := net.ParseIP(s)
	return ip != nil && ip.To4() != nil
}

// peerPortClash reports "PORT (peer NAME)" for the first requested port that
// another registered tunnel already serves. excludeID skips one peer (<0 = none).
func peerPortClash(want []int, excludeID int) string {
	claimed := map[int]string{}
	for _, p := range loadPeers() {
		if p.ID == excludeID {
			continue
		}
		for _, port := range p.Ports {
			claimed[port] = p.Name
		}
	}
	for _, port := range want {
		if name, ok := claimed[port]; ok {
			return fmt.Sprintf("%d (peer %s)", port, name)
		}
	}
	return ""
}

// peerDuplicateIPClash reports the name of an existing peer that already
// uses the given remote IP, skipping excludeID.
func peerDuplicateIPClash(remoteIP string, excludeID int) string {
	for _, p := range loadPeers() {
		if p.ID == excludeID {
			continue
		}
		if p.RemotePub == remoteIP {
			return p.Name
		}
	}
	return ""
}

func parsePorts(s string) []int {
	var out []int
	seen := map[int]bool{}
	for _, p := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	}) {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || n < 1 || n > 65535 || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out
}

func isValidPortOrRangeOrMapping(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	// Case 1: single port "443"
	if p, err := strconv.Atoi(s); err == nil {
		return p >= 1 && p <= 65535
	}
	// Case 2: range "10000-10050"
	if strings.Contains(s, "-") {
		parts := strings.Split(s, "-")
		if len(parts) == 2 {
			p1, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
			p2, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
			return err1 == nil && err2 == nil && p1 >= 1 && p2 <= 65535 && p1 < p2
		}
	}
	// Case 3: mapping "2083=8443"
	if strings.Contains(s, "=") {
		parts := strings.Split(s, "=")
		if len(parts) == 2 {
			p1, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
			p2, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
			return err1 == nil && err2 == nil && p1 >= 1 && p1 <= 65535 && p2 >= 1 && p2 <= 65535
		}
	}
	return false
}

func parseRawPorts(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, p := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	}) {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		if isValidPortOrRangeOrMapping(p) {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

func extractNumericPorts(rawPorts []string) []int {
	var out []int
	seen := map[int]bool{}
	for _, rp := range rawPorts {
		if p, err := strconv.Atoi(rp); err == nil && p >= 1 && p <= 65535 {
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		} else if strings.Contains(rp, "=") {
			parts := strings.Split(rp, "=")
			if p, err := strconv.Atoi(strings.TrimSpace(parts[0])); err == nil && p >= 1 && p <= 65535 {
				if !seen[p] {
					seen[p] = true
					out = append(out, p)
				}
			}
		} else if strings.Contains(rp, "-") {
			parts := strings.Split(rp, "-")
			if p, err := strconv.Atoi(strings.TrimSpace(parts[0])); err == nil && p >= 1 && p <= 65535 {
				if !seen[p] {
					seen[p] = true
					out = append(out, p)
				}
			}
		}
	}
	return out
}

func rewriteBackhaulTomlPorts(src string, ports []string) string {
	lines := strings.Split(src, "\n")
	var out []string
	inPorts := false
	portsAdded := false
	var portsBlock []string
	portsBlock = append(portsBlock, "ports = [")
	for i, p := range ports {
		comma := ","
		if i == len(ports)-1 {
			comma = ""
		}
		portsBlock = append(portsBlock, fmt.Sprintf("  %q%s", strings.TrimSpace(p), comma))
	}
	portsBlock = append(portsBlock, "]")

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "ports =") || strings.HasPrefix(trimmed, "ports=") {
			inPorts = true
			if !portsAdded {
				out = append(out, portsBlock...)
				portsAdded = true
			}
			if strings.HasSuffix(trimmed, "]") && strings.Contains(trimmed, "[") {
				inPorts = false
			}
			continue
		}
		if inPorts {
			if strings.HasPrefix(trimmed, "]") || strings.Contains(trimmed, "]") {
				inPorts = false
			}
			continue
		}
		out = append(out, line)
	}
	if !portsAdded {
		out = append(out, "")
		out = append(out, portsBlock...)
	}
	return strings.Join(out, "\n")
}

func writeBackhaulServerConfig(path string, bindAddr string, transport string, token string, ports []string) error {
	if transport == "" {
		transport = "tcpmux"
	}
	var sb strings.Builder
	sb.WriteString("[server]\n")
	sb.WriteString(fmt.Sprintf("bind_addr = %q\n", bindAddr))
	sb.WriteString(fmt.Sprintf("transport = %q\n", transport))
	if token != "" {
		sb.WriteString(fmt.Sprintf("token = %q\n", token))
	}
	sb.WriteString("keepalive_period = 75\n")
	sb.WriteString("nodelay = true\n")
	sb.WriteString("heartbeat = 40\n")
	sb.WriteString("channel_size = 2048\n")
	sb.WriteString("sniffer = false\n")
	sb.WriteString("web_port = 0\n")
	sb.WriteString("sniffer_log = \"\"\n")
	sb.WriteString("log_level = \"info\"\n")
	if transport == "wss" || transport == "wssmux" {
		sb.WriteString("tls_cert = \"/etc/backhaul/server.crt\"\n")
		sb.WriteString("tls_key = \"/etc/backhaul/server.key\"\n")
	}
	sb.WriteString("ports = [\n")
	for i, p := range ports {
		comma := ","
		if i == len(ports)-1 {
			comma = ""
		}
		sb.WriteString(fmt.Sprintf("  %q%s\n", strings.TrimSpace(p), comma))
	}
	sb.WriteString("]\n")

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(sb.String()), 0644)
}

func writeBackhaulClientConfig(path string, remoteAddr string, transport string, token string) error {
	if transport == "" {
		transport = "tcpmux"
	}
	var sb strings.Builder
	sb.WriteString("[client]\n")
	sb.WriteString(fmt.Sprintf("remote_addr = %q\n", remoteAddr))
	sb.WriteString(fmt.Sprintf("transport = %q\n", transport))
	if token != "" {
		sb.WriteString(fmt.Sprintf("token = %q\n", token))
	}
	sb.WriteString("connection_pool = 8\n")
	sb.WriteString("nodelay = true\n")
	sb.WriteString("retry_interval = 3\n")
	sb.WriteString("keepalive_period = 75\n")
	sb.WriteString("sniffer = false\n")
	sb.WriteString("web_port = 0\n")
	sb.WriteString("sniffer_log = \"\"\n")
	sb.WriteString("log_level = \"info\"\n")

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(sb.String()), 0644)
}

func randomToken(n int) string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("token-%d", n)
	}
	for i := range b {
		b[i] = chars[int(b[i])%len(chars)]
	}
	return string(b)
}

func randomFrpPort() int {
	var b [2]byte
	if _, err := rand.Read(b[:]); err != nil {
		return defaultFrpPortMin + (os.Getpid() % (defaultFrpPortMax - defaultFrpPortMin + 1))
	}
	val := int(b[0])<<8 | int(b[1])
	return defaultFrpPortMin + (val % (defaultFrpPortMax - defaultFrpPortMin + 1))
}

// ---- setup bundle: one readable string with everything foreign needs ----

// setupBundle is a parsed
// hsh1_<IRAN_PUB>_<FRP_PORT>_<IRAN_GRE>_<FOREIGN_GRE>_<TOKEN>[_<PORTS>][_fou<P1>-<P2>]
type setupBundle struct {
	Engine     string   // "frp" | "backhaul" | "gre-backhaul"
	Transport  string   // for backhaul: "tcpmux", "tcp", "ws", "wss", "wsmux", "wssmux"
	IranPub    string
	FrpPort    int
	IranGre    string
	ForeignGre string
	Token      string
	Ports      []int
	RawPorts   []string
	FouPorts   []int
}

// MakeBundle builds the single foreign-setup string. Ports may be empty
// (base setup-iran omits them); when present they join with '-'.
func MakeBundle(iranPub string, frpPort int, iranGre, foreignGre, token string, ports []int) string {
	cfg := loadCarrierConfig()
	p1, p2 := cfg.FOUPort1, cfg.FOUPort2
	if p1 <= 0 {
		p1 = 443
	}
	if p2 <= 0 {
		p2 = 55555
	}
	s := bundlePrefix + iranPub + "_" + strconv.Itoa(frpPort) + "_" + iranGre + "_" + foreignGre + "_" + token
	portsPart := ""
	if len(ports) > 0 {
		strs := make([]string, len(ports))
		for i, p := range ports {
			strs[i] = strconv.Itoa(p)
		}
		portsPart = strings.Join(strs, "-")
	}
	fouPart := fmt.Sprintf("fou%d-%d", p1, p2)
	if portsPart != "" {
		s += "_" + portsPart + "_" + fouPart
	} else {
		s += "__" + fouPart
	}
	return s
}

// MakeBackhaulBundle builds the Standalone Backhaul setup bundle (No-GRE).
// Format: bh1_<IRAN_PUB>_<BH_PORT>_<TRANSPORT>_<TOKEN>[_<PORTS>]
func MakeBackhaulBundle(iranPub string, bhPort int, transport, token string, ports []string) string {
	if transport == "" {
		transport = "tcpmux"
	}
	s := bundlePrefixBackhaul + iranPub + "_" + strconv.Itoa(bhPort) + "_" + transport + "_" + token
	if len(ports) > 0 {
		s += "_" + strings.Join(ports, ",")
	}
	return s
}

// MakeGreBackhaulBundle builds the GRE + Backhaul setup bundle.
// Format: gh1_<IRAN_PUB>_<BH_PORT>_<IRAN_GRE>_<FOREIGN_GRE>_<TRANSPORT>_<TOKEN>[_<PORTS>]
func MakeGreBackhaulBundle(iranPub string, bhPort int, iranGre, foreignGre, transport, token string, ports []string) string {
	if transport == "" {
		transport = "tcpmux"
	}
	s := bundlePrefixGreBackhaul + iranPub + "_" + strconv.Itoa(bhPort) + "_" + iranGre + "_" + foreignGre + "_" + transport + "_" + token
	if len(ports) > 0 {
		s += "_" + strings.Join(ports, ",")
	}
	return s
}

// ParseBackhaulBundle parses a Standalone Backhaul bundle (bh1_...).
func ParseBackhaulBundle(s string) (setupBundle, error) {
	var b setupBundle
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, bundlePrefixBackhaul) {
		return b, fmt.Errorf("not a backhaul bundle (must start with bh1_)")
	}
	rest := strings.TrimPrefix(s, bundlePrefixBackhaul)
	parts := strings.Split(rest, "_")
	if len(parts) < 4 || len(parts) > 5 {
		return b, fmt.Errorf("backhaul bundle must have 4 or 5 underscore parts (got %d)", len(parts))
	}
	iranPub, portS, transport, token := parts[0], parts[1], parts[2], parts[3]
	if net.ParseIP(iranPub) == nil || !isV4(iranPub) {
		return b, fmt.Errorf("bad Iran public IP in bundle: %q", iranPub)
	}
	port, err := strconv.Atoi(portS)
	if err != nil || port < 1 || port > 65535 {
		return b, fmt.Errorf("bad control port in bundle: %q", portS)
	}
	if transport == "" {
		transport = "tcpmux"
	}
	if len(token) == 0 || len(token) > 128 {
		return b, fmt.Errorf("bad token in bundle (length 1-128)")
	}
	b = setupBundle{
		Engine:    "backhaul",
		Transport: transport,
		IranPub:   iranPub,
		FrpPort:   port,
		Token:     token,
	}
	if len(parts) == 5 && parts[4] != "" {
		rawPorts := parseRawPorts(parts[4])
		b.RawPorts = rawPorts
		b.Ports = extractNumericPorts(rawPorts)
	}
	return b, nil
}

// ParseGreBackhaulBundle parses a GRE+Backhaul bundle (gh1_...).
func ParseGreBackhaulBundle(s string) (setupBundle, error) {
	var b setupBundle
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, bundlePrefixGreBackhaul) {
		return b, fmt.Errorf("not a gre-backhaul bundle (must start with gh1_)")
	}
	rest := strings.TrimPrefix(s, bundlePrefixGreBackhaul)
	parts := strings.Split(rest, "_")
	if len(parts) < 6 || len(parts) > 7 {
		return b, fmt.Errorf("gre-backhaul bundle must have 6 or 7 underscore parts (got %d)", len(parts))
	}
	iranPub, portS, iranGre, foreignGre, transport, token := parts[0], parts[1], parts[2], parts[3], parts[4], parts[5]
	if net.ParseIP(iranPub) == nil || !isV4(iranPub) {
		return b, fmt.Errorf("bad Iran public IP in bundle: %q", iranPub)
	}
	port, err := strconv.Atoi(portS)
	if err != nil || port < 1 || port > 65535 {
		return b, fmt.Errorf("bad control port in bundle: %q", portS)
	}
	if net.ParseIP(iranGre) == nil || !isV4(iranGre) {
		return b, fmt.Errorf("bad Iran GRE IP in bundle: %q", iranGre)
	}
	if net.ParseIP(foreignGre) == nil || !isV4(foreignGre) {
		return b, fmt.Errorf("bad foreign GRE IP in bundle: %q", foreignGre)
	}
	if transport == "" {
		transport = "tcpmux"
	}
	if len(token) == 0 || len(token) > 128 {
		return b, fmt.Errorf("bad token in bundle (length 1-128)")
	}
	b = setupBundle{
		Engine:     "gre-backhaul",
		Transport:  transport,
		IranPub:    iranPub,
		FrpPort:    port,
		IranGre:    iranGre,
		ForeignGre: foreignGre,
		Token:      token,
	}
	if len(parts) == 7 && parts[6] != "" {
		rawPorts := parseRawPorts(parts[6])
		b.RawPorts = rawPorts
		b.Ports = extractNumericPorts(rawPorts)
	}
	return b, nil
}

// ParseBundle validates and parses any bundle (hsh1_..., bh1_..., or gh1_...).
func ParseBundle(s string) (setupBundle, error) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, bundlePrefixBackhaul) {
		return ParseBackhaulBundle(s)
	}
	if strings.HasPrefix(s, bundlePrefixGreBackhaul) {
		return ParseGreBackhaulBundle(s)
	}
	var b setupBundle
	if !strings.HasPrefix(s, bundlePrefix) {
		return b, fmt.Errorf("not a bundle (must start with hsh1_, bh1_, or gh1_)")
	}
	rest := strings.TrimPrefix(s, bundlePrefix)
	parts := strings.Split(rest, "_")
	if len(parts) < 5 || len(parts) > 7 {
		return b, fmt.Errorf("bundle must have 5, 6, or 7 underscore parts (got %d)", len(parts))
	}
	iranPub, portS, iranGre, foreignGre, token := parts[0], parts[1], parts[2], parts[3], parts[4]
	if net.ParseIP(iranPub) == nil || !isV4(iranPub) {
		return b, fmt.Errorf("bad Iran public IP in bundle: %q", iranPub)
	}
	port, err := strconv.Atoi(portS)
	if err != nil || port < 1 || port > 65535 {
		return b, fmt.Errorf("bad control port in bundle: %q", portS)
	}
	if net.ParseIP(iranGre) == nil || !isV4(iranGre) {
		return b, fmt.Errorf("bad Iran GRE IP in bundle: %q", iranGre)
	}
	if net.ParseIP(foreignGre) == nil || !isV4(foreignGre) {
		return b, fmt.Errorf("bad foreign GRE IP in bundle: %q", foreignGre)
	}
	if len(token) == 0 || len(token) > 128 {
		return b, fmt.Errorf("bad token in bundle (length 1-128)")
	}
	b = setupBundle{Engine: "frp", IranPub: iranPub, FrpPort: port, IranGre: iranGre, ForeignGre: foreignGre, Token: token}
	for i := 5; i < len(parts); i++ {
		p := parts[i]
		if p == "" {
			continue
		}
		if strings.HasPrefix(p, "fou") {
			fouRaw := strings.TrimPrefix(p, "fou")
			for _, portStr := range strings.Split(fouRaw, "-") {
				if fp, err := strconv.Atoi(portStr); err == nil && fp >= 1 && fp <= 65535 {
					b.FouPorts = append(b.FouPorts, fp)
				}
			}
		} else {
			ports := parsePorts(strings.ReplaceAll(p, "-", ","))
			if len(ports) > 0 {
				b.Ports = ports
				for _, num := range ports {
					b.RawPorts = append(b.RawPorts, strconv.Itoa(num))
				}
			}
		}
	}
	return b, nil
}

// isBundle reports whether a token field holds a foreign-setup bundle
// (vs a legacy 32-char token, which must keep working as-is).
func isBundle(s string) bool {
	t := strings.TrimSpace(s)
	return strings.HasPrefix(t, bundlePrefix) || strings.HasPrefix(t, bundlePrefixBackhaul) || strings.HasPrefix(t, bundlePrefixGreBackhaul)
}

// isBackhaulBundle reports whether s starts with bh1_.
func isBackhaulBundle(s string) bool {
	return strings.HasPrefix(strings.TrimSpace(s), bundlePrefixBackhaul)
}

// isGreBackhaulBundle reports whether s starts with gh1_.
func isGreBackhaulBundle(s string) bool {
	return strings.HasPrefix(strings.TrimSpace(s), bundlePrefixGreBackhaul)
}

// applyBundle fills foreign-setup fields from a parsed bundle.
// Bundle is the authoritative Source of Truth for tunnel parameters.
// Explicit body.Ports (from the user's Reverse Ports field) are preserved
// if the bundle itself has no ports segment (e.g. hsh1_...token__fou...).
// body.OrigBundle must be set BEFORE calling applyBundle so runInstaller
// can still pass --bundle to hashem.sh for carrier/FOU configuration.
func applyBundle(body *setupRequest, b setupBundle) {
	if b.Engine != "" {
		body.Engine = b.Engine
	}
	if b.Transport != "" {
		body.Transport = b.Transport
	}
	body.RemotePub = b.IranPub
	body.FrpPort = b.FrpPort

	if b.Engine == "backhaul" {
		body.LocalGre = ""
		body.PeerGre = ""
	} else {
		body.LocalGre = b.ForeignGre
		body.PeerGre = b.IranGre
	}

	// Only override ports from bundle when bundle actually has ports;
	// if the bundle's ports segment was empty, keep what the user typed.
	if len(b.RawPorts) > 0 {
		body.Ports = strings.Join(b.RawPorts, ", ")
	} else if len(b.Ports) > 0 {
		strs := make([]string, len(b.Ports))
		for i, p := range b.Ports {
			strs[i] = strconv.Itoa(p)
		}
		body.Ports = strings.Join(strs, ", ")
	}
	if len(b.FouPorts) >= 2 {
		cfg := loadCarrierConfig()
		cfg.FOUPort1 = b.FouPorts[0]
		cfg.FOUPort2 = b.FouPorts[1]
		cfg.Candidates = []string{"direct", fmt.Sprintf("wss:%d", cfg.WSSPort)}
		_ = saveCarrierConfig(cfg)
		_, _ = runHashemCarrierCmd("set-ports", strconv.Itoa(cfg.FOUPort1), strconv.Itoa(cfg.FOUPort2))
	}
	// Replace body.Token with the inner token (not the full bundle string).
	// The caller must have already saved OrigBundle = body.Token before calling.
	body.Token = b.Token
}
