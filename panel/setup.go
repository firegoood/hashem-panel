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
	bundlePrefix = "hsh1_"
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

// ensureAddPeerScript checks the script on disk supports the add-peer
// CLI verb; if not (stale Sep-26 copy on older hosts), it pulls the
// latest hashem.sh from main (bash -n verified) and replaces it — so an
// add-peer request self-heals instead of failing with E-INSTALL-02.
func ensureAddPeerScript(script string) {
	data, err := os.ReadFile(script)
	if err != nil {
		return
	}
	if strings.Contains(string(data), "add-peer") {
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
		if len(loadPeers()) >= 5 {
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
	if net.ParseIP(body.LocalGre) == nil || !isV4(body.LocalGre) {
		writeAPIError(w, r, "E-SETUP-05", "")
		return
	}
	if net.ParseIP(body.PeerGre) == nil || !isV4(body.PeerGre) {
		writeAPIError(w, r, "E-SETUP-06", "")
		return
	}
	if body.FrpPort < 1 || body.FrpPort > 65535 {
		writeAPIError(w, r, "E-SETUP-07", "")
		return
	}

	var ports []int
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
		ports = parsePorts(body.Ports)
		if len(ports) == 0 {
			writeAPIError(w, r, "E-SETUP-10", "")
			return
		}
	}

	// add-peer pre-check: reject ports another tunnel already serves (409 + peer name)
	if body.Role == "add-peer" {
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
	token, bundle, steps, err := runInstaller(body, ports)
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
func runInstaller(b setupRequest, ports []int) (string, string, []string, error) {
	script, err := greScriptPath()
	if err != nil {
		return "", "", nil, err
	}
	// Self-healing: if add-peer is invoked on a host whose hashem.sh is
	// an old version (lacking the "add-peer" CLI verb), auto-sync the
	// script before shelling out so the user does not hit E-INSTALL-02.
	if b.Role == "add-peer" {
		ensureAddPeerScript(script)
	}
	token := ""
	args := []string{}
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
		return token, MakeBundle(b.LocalPub, b.FrpPort, b.LocalGre, b.PeerGre, token, ports), steps, nil
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
	IranPub    string
	FrpPort    int
	IranGre    string
	ForeignGre string
	Token      string
	Ports      []int
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

// ParseBundle validates a bundle pasted into the foreign token field.
// Supports 5, 6, or 7 parts (handles optional ports and optional _fou<P1>-<P2>).
// Legacy 32-char tokens are NOT bundles — isBundle() guards that first.
func ParseBundle(s string) (setupBundle, error) {
	var b setupBundle
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, bundlePrefix) {
		return b, fmt.Errorf("not a bundle (must start with hsh1_)")
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
	b = setupBundle{IranPub: iranPub, FrpPort: port, IranGre: iranGre, ForeignGre: foreignGre, Token: token}
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
			}
		}
	}
	return b, nil
}

// isBundle reports whether a token field holds a foreign-setup bundle
// (vs a legacy 32-char token, which must keep working as-is).
func isBundle(s string) bool { return strings.HasPrefix(strings.TrimSpace(s), bundlePrefix) }

// applyBundle fills empty foreign/add-peer fields from a parsed bundle.
// Explicit fields always win. Mapping (foreign side view): bundle's Iran
// public IP is OUR remote; bundle's foreign GRE is OUR local GRE;
// bundle's Iran GRE is OUR peer GRE.
func applyBundle(body *setupRequest, b setupBundle) {
	if body.RemotePub == "" {
		body.RemotePub = b.IranPub
	}
	if body.LocalGre == "" {
		body.LocalGre = b.ForeignGre
	}
	if body.PeerGre == "" {
		body.PeerGre = b.IranGre
	}
	if body.FrpPort == 0 {
		body.FrpPort = b.FrpPort
	}
	if body.Ports == "" && len(b.Ports) > 0 {
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
		cfg.Candidates = []string{"direct", fmt.Sprintf("fou:%d", cfg.FOUPort1), fmt.Sprintf("fou:%d", cfg.FOUPort2), fmt.Sprintf("wss:%d", cfg.WSSPort)}
		_ = saveCarrierConfig(cfg)
		_, _ = runHashemCarrierCmd("set-ports", strconv.Itoa(cfg.FOUPort1), strconv.Itoa(cfg.FOUPort2))
	}
	body.Token = b.Token
}
