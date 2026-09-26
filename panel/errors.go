package main

// Error codes: every backend failure returns a stable E-XXXX code so the
// exact cause is identifiable from the UI or API. Codes are also recorded
// into a small on-disk journal (panel-errors.log) and served via
// GET /api/errors. Journald/frps lines are annotated with the same codes
// by matchLogCode() so raw logs and API errors speak one language.
//
// Families:
//   E-AUTH-xx    login / session / password
//   E-SETUP-xx   setup form validation
//   E-PEER-xx    multi-peer registry (full / clash / unknown peer)
//   E-INSTALL-xx gre.sh missing / installer failed
//   E-ACTION-xx  tunnel actions (restart/ping/remove/tune)
//   E-UPDATE-xx  version check / download / install
//   E-GRE-xx     GRE interface / ping diagnostics (log annotation)
//   E-FRP-xx     FRP diagnostics (log annotation)
//   E-SYS-xx     host tooling (journalctl/systemctl missing)

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type errInfo struct {
	Status int    `json:"-"`
	Msg    string `json:"msg"`
	Hint   string `json:"hint"`
}

var errCatalog = map[string]errInfo{
	// auth
	"E-AUTH-01": {401, "unauthorized (login required)", "Log in again from the login screen."},
	"E-AUTH-02": {401, "wrong username or password", "Check caps-lock; view the password via the server menu if needed."},
	"E-AUTH-03": {400, "bad login request", "Reload the page and try again."},
	"E-AUTH-04": {400, "password must be at least 4 characters", "Pick a longer password."},
	"E-AUTH-05": {500, "cannot switch password", "Disk write failed — check /etc/gre-panel permissions."},
	// setup validation
	"E-SETUP-01": {400, "bad setup request (invalid JSON)", "Reload the page and resubmit the form."},
	"E-SETUP-02": {400, "role must be iran, foreign or add-peer", "Pick the role from the Setup tab buttons."},
	"E-SETUP-03": {400, "invalid local public IP", "Use the server's real public IPv4 (Setup tab auto-detects it)."},
	"E-SETUP-04": {400, "invalid remote public IP", "Enter the other side's public IPv4 exactly."},
	"E-SETUP-05": {400, "invalid local GRE IP", "Use the suggested 10.x address or another private IPv4."},
	"E-SETUP-06": {400, "invalid peer GRE IP", "Use the suggested 10.x address or another private IPv4."},
	"E-SETUP-07": {400, "frp port must be 1-65535", "Use 7000 for the first tunnel, 7001+ for the next ones."},
	"E-SETUP-08": {400, "token from Iran side is required", "Copy the token shown on the Iran panel into this form."},
	"E-SETUP-09": {400, "token too long (max 128)", "Paste the token as-is; do not add extra text."},
	"E-SETUP-10": {400, "at least one reverse port is required (e.g. 443, 2083)", "Add the ports clients will connect to."},
	// peers
	"E-PEER-01": {409, "peer table full (max 5 foreign servers)", "Remove one peer card before adding another."},
	"E-PEER-02": {409, "reverse port already served by another tunnel", "Pick a different port — the conflicting peer is named in the message."},
	"E-PEER-03": {409, "tunnel already exists on this server", "Resubmit with force:true to overwrite, or remove it first."},
	"E-PEER-04": {404, "unknown peer id", "Refresh the page; the peer may have been removed."},
	"E-PEER-05": {400, "bad peer id", "Refresh the page and try again."},
	"E-PEER-06": {400, "use POST /api/setup with role=add-peer", "This endpoint is read-only; create peers from the Tunnel tab."},
	// installer
	"E-INSTALL-01": {500, "gre.sh installer not found", "Reinstall the panel or set GRE_SCRIPT=/path/to/gre.sh."},
	"E-INSTALL-02": {500, "installer (gre.sh) failed", "Open the steps output; the failing shell line is shown first."},
	// actions
	"E-ACTION-01": {400, "unknown action", "Reload the page; the button may be from an older version."},
	"E-ACTION-02": {400, "bad action request (invalid JSON)", "Reload the page and try again."},
	"E-ACTION-03": {400, "action command failed", "See the Logs tab for this service right after the failure."},
	// update
	"E-UPDATE-01": {502, "cannot check latest release", "Server has no GitHub access (filter/DNS) — retry later."},
	"E-UPDATE-02": {502, "panel download failed", "GitHub unreachable mid-download — retry; check Iran network filter."},
	"E-UPDATE-03": {502, "downloaded file failed verification", "Stale release redirect (HTML instead of ELF) — retry in a minute."},
	"E-UPDATE-04": {500, "cannot install new binary (rolled back)", "Disk full or /usr/local/bin not writable — old binary kept."},
	"E-UPDATE-05": {400, "unsupported arch for update", "Only amd64/arm64 prebuilt binaries are published."},
	// log diagnostics (annotation only, also reused by status hints)
	"E-GRE-01": {0, "GRE interface missing / down", "Tunnel setup did not create it, or it was deleted — reinstall that peer."},
	"E-GRE-02": {0, "GRE ping failed (100% loss / unreachable)", "Peers cannot reach each other: firewall, wrong public IP, or GRE blocked."},
	"E-FRP-01": {0, "frps service not active", "Start it from the peer card (Restart) or check its log."},
	"E-FRP-02": {0, "frp authentication failed (token mismatch)", "Re-copy the token from the Iran peer card to the Foreign side."},
	"E-FRP-03": {0, "frp bind conflict (address already in use)", "Another tunnel uses this port — change frp_port or the reverse port."},
	"E-FRP-04": {0, "frp cannot reach server (connection refused/timeout)", "Iran unreachable: wrong IP/port, firewall, or frps down."},
	"E-SYS-01": {0, "host tool unavailable", "journalctl/systemctl/ip missing on this host."},
}

type errEvent struct {
	Time     string `json:"time"`
	Code     string `json:"code"`
	Endpoint string `json:"endpoint"`
	Detail   string `json:"detail"`
}

var (
	errMu     sync.Mutex
	errEvents []errEvent // in-memory ring, capped
)

func errorLogPath() string { return filepath.Join(configDir, "panel-errors.log") }

// writeAPIError is the single way backend handlers report failures:
// stable JSON {error_code, error, hint} + journal entry.
func writeAPIError(w http.ResponseWriter, r *http.Request, code, detail string) {
	info, ok := errCatalog[code]
	if !ok {
		info = errInfo{Status: http.StatusBadRequest, Msg: "request failed", Hint: ""}
		code = "E-SYS-01"
	}
	msg := info.Msg
	if detail != "" {
		msg = detail
	}
	ep := ""
	if r != nil {
		ep = r.Method + " " + r.URL.Path
	}
	recordError(code, ep, msg)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(info.Status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error_code": code, "error": msg, "hint": info.Hint,
	})
}

func recordError(code, endpoint, detail string) {
	ev := errEvent{
		Time:     time.Now().Format("2006-01-02 15:04:05"),
		Code:     code,
		Endpoint: endpoint,
		Detail:   detail,
	}
	errMu.Lock()
	errEvents = append(errEvents, ev)
	if len(errEvents) > 200 {
		errEvents = errEvents[len(errEvents)-200:]
	}
	errMu.Unlock()
	line, _ := json.Marshal(ev)
	f, err := os.OpenFile(errorLogPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err == nil {
		_, _ = f.Write(append(line, '\n'))
		_ = f.Close()
	}
}

// GET /api/errors?n=100 — recent panel error events (newest last).
func handleErrors(w http.ResponseWriter, r *http.Request) {
	n := 100
	if v := r.URL.Query().Get("n"); v != "" {
		if q, err := strconv.Atoi(v); err == nil {
			if q < 10 {
				q = 10
			}
			if q > 200 {
				q = 200
			}
			n = q
		}
	}
	errMu.Lock()
	mem := append([]errEvent(nil), errEvents...)
	errMu.Unlock()
	// merge with on-disk journal (dedupe by full line, keep last 200)
	seen := map[string]bool{}
	for _, e := range mem {
		b, _ := json.Marshal(e)
		seen[string(b)] = true
	}
	if data, err := os.ReadFile(errorLogPath()); err == nil {
		for _, ln := range strings.Split(string(data), "\n") {
			ln = strings.TrimSpace(ln)
			if ln == "" || seen[ln] {
				continue
			}
			var e errEvent
			if json.Unmarshal([]byte(ln), &e) == nil {
				mem = append(mem, e)
				seen[ln] = true
			}
		}
	}
	if len(mem) > 200 {
		mem = mem[len(mem)-200:]
	}
	if len(mem) > n {
		mem = mem[len(mem)-n:]
	}
	writeJSON(w, map[string]any{"events": mem, "count": len(mem)})
}

// matchLogCode annotates one journald/frps log line with a stable code.
// Returns "" when the line carries no known failure signature.
func matchLogCode(line string) (code, hint string) {
	l := strings.ToLower(line)
	has := func(words ...string) bool {
		for _, wd := range words {
			if strings.Contains(l, wd) {
				return true
			}
		}
		return false
	}
	switch {
	case has("address already in use", "bind:") && has("7000", "bind", "listen", "error", "fail"):
		return "E-FRP-03", errCatalog["E-FRP-03"].Hint
	case has("address already in use"):
		return "E-FRP-03", errCatalog["E-FRP-03"].Hint
	case has("token", "auth") && has("mismatch", "invalid", "incorrect", "unauthorized", "failed", "reject", "denied"):
		return "E-FRP-02", errCatalog["E-FRP-02"].Hint
	case has("authentication failed", "login failed", "authorization failed"):
		return "E-FRP-02", errCatalog["E-FRP-02"].Hint
	case has("connection refused"):
		return "E-FRP-04", errCatalog["E-FRP-04"].Hint
	case has("i/o timeout", "dial timeout", "connect timeout", "deadline exceeded") && has("frp", "proxy", "tunnel", "server", "dial"):
		return "E-FRP-04", errCatalog["E-FRP-04"].Hint
	case has("cannot find device", "no such device", "network is unreachable") && has("gre"):
		return "E-GRE-01", errCatalog["E-GRE-01"].Hint
	case has("destination host unreachable", "100% packet loss", "packet loss 100%"):
		return "E-GRE-02", errCatalog["E-GRE-02"].Hint
	case has("permission denied") && has("frp", "gre", "tunnel", "bind"):
		return "E-SYS-01", errCatalog["E-SYS-01"].Hint
	case has("unauthorized", "wrong username or password") && has("login", "session", "panel"):
		return "E-AUTH-02", errCatalog["E-AUTH-02"].Hint
	case has("port", "already") && has("served", "peer", "tunnel"):
		return "E-PEER-02", errCatalog["E-PEER-02"].Hint
	case has("peer table full", "max 5"):
		return "E-PEER-01", errCatalog["E-PEER-01"].Hint
	}
	return "", ""
}

type logFinding struct {
	Code  string `json:"code"`
	Msg   string `json:"msg"`
	Hint  string `json:"hint"`
	Count int    `json:"count"`
	Line  string `json:"line"` // first matching line (trimmed)
}

// summarizeLogs folds raw log text into per-code findings for the UI header.
func summarizeLogs(raw string) []logFinding {
	by := map[string]*logFinding{}
	order := []string{}
	for _, ln := range strings.Split(raw, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		code, _ := matchLogCode(ln)
		if code == "" {
			continue
		}
		f, ok := by[code]
		if !ok {
			info := errCatalog[code]
			f = &logFinding{Code: code, Msg: info.Msg, Hint: info.Hint, Line: trimLen(ln, 220)}
			by[code] = f
			order = append(order, code)
		}
		f.Count++
	}
	out := []logFinding{}
	for _, c := range order {
		out = append(out, *by[c])
	}
	return out
}

func trimLen(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
