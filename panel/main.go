package main

// Entry point: config load, route table, static assets.
// Auth lives in auth.go, tunnel status/actions in tunnel.go,
// setup in setup.go, dashboard metrics in dashboard.go.

import (
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

//go:embed index.html fonts.css tokens.css base.css enterprise.css favicon.png xterm.js xterm-fit.js xterm-search.js xterm.css fonts/vazirmatn.woff2
var panelFS embed.FS

var configDir = "/etc/gre-panel"

type panelConfig struct {
	Username string `json:"username"`
	PassHash string `json:"pass_hash"`
	Port     int    `json:"port"`
	BasePath string `json:"base_path"`
	// TerminalEnabled gates the Phase 3 interactive terminal tab
	// (feature flag, default off; user enables after testing).
	TerminalEnabled bool `json:"terminal_enabled,omitempty"`
	// TLSPort is the HTTPS listener port (default 7443). HTTP stays on Port.
	TLSPort int `json:"tls_port,omitempty"`
}

func termEnabled() bool { return cfg.TerminalEnabled }

// effectiveTLSPort returns the HTTPS port (7443 default when unset).
func effectiveTLSPort() int {
	if cfg.TLSPort < 1 || cfg.TLSPort > 65535 {
		return 7443
	}
	return cfg.TLSPort
}

var (
	cfg panelConfig
	// panelVersion is set at release build time:
	// go build -ldflags "-X main.panelVersion=panel-rN"
	panelVersion = "dev"
	// panelMux is shared between the HTTP listener and the HTTPS listener.
	panelMux *http.ServeMux
)

func cfgPath() string { return filepath.Join(configDir, "panel.json") }

func saveCfg() { _ = os.WriteFile(cfgPath(), mustJSON(cfg), 0600) }

// ---- auto port: never fail install when the HTTP/TLS port is taken ----

// portFree reports whether TCP :port can be bound right now.
func portFree(port int) bool {
	ln, err := net.Listen("tcp", ":"+strconv.Itoa(port))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

// pickFreePort scans upward from start for the first bindable TCP port
// (max 100 tries). Falls back to start so the caller errors naturally.
func pickFreePort(start int) int {
	for p := start; p < start+100 && p <= 65535; p++ {
		if p < 1 {
			continue
		}
		if portFree(p) {
			return p
		}
	}
	return start
}

// envPanelPort reads GRE_PANEL_PORT (set by hashem.sh or the admin).
// Returns 0 when unset/invalid.
func envPanelPort() int {
	v := strings.TrimSpace(os.Getenv("GRE_PANEL_PORT"))
	if v == "" {
		return 0
	}
	p, err := strconv.Atoi(v)
	if err != nil || p < 1 || p > 65535 {
		log.Printf("ignoring invalid GRE_PANEL_PORT=%q (must be 1-65535)", v)
		return 0
	}
	return p
}

// ensureFreeHTTPPort guarantees cfg.Port is bindable: env override wins,
// otherwise the saved port (or 7777 fresh) is kept; when busy we scan
// upward and persist the new port so show_panel_url/displays stay correct.
func ensureFreeHTTPPort() {
	desired := cfg.Port
	if p := envPanelPort(); p != 0 {
		desired = p
	} else if desired < 1 || desired > 65535 {
		desired = 7777
	}
	if portFree(desired) {
		if cfg.Port != desired {
			cfg.Port = desired
			saveCfg()
		}
		return
	}
	next := pickFreePort(desired + 1)
	log.Printf("panel port %d busy — auto-switched to %d (saved to panel.json)", desired, next)
	cfg.Port = next
	saveCfg()
}

func loadOrInit() {
	_ = os.MkdirAll(configDir, 0700)
	data, err := os.ReadFile(cfgPath())
	if err == nil && json.Unmarshal(data, &cfg) == nil && cfg.PassHash != "" {
		// Test/dev override: fixed password via env (takes effect on restart).
		if pw := os.Getenv("GRE_PANEL_PASSWORD"); pw != "" {
			h := sha256.Sum256([]byte(pw))
			cfg.PassHash = hex.EncodeToString(h[:])
			_ = os.WriteFile(cfgPath(), mustJSON(cfg), 0600)
		}
		return
	}
	pass := os.Getenv("GRE_PANEL_PASSWORD")
	if pass == "" {
		pass = randomDigits(8)
	}
	h := sha256.Sum256([]byte(pass))
	cfg = panelConfig{
		Username: "admin",
		PassHash: hex.EncodeToString(h[:]),
		Port:     7777,
		BasePath: randomBase(12),
	}
	_ = os.WriteFile(cfgPath(), mustJSON(cfg), 0600)
	// plaintext copy so the server admin can view it later via script menu (user choice)
	_ = os.WriteFile(filepath.Join(configDir, "panel.pass"), []byte(pass), 0600)
	log.Printf("panel password: %s (user %s) — change it from Settings", pass, cfg.Username)
}

func mustJSON(v any) []byte {
	b, _ := json.MarshalIndent(v, "", "  ")
	return b
}

func randomDigits(n int) string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	digits := "0123456789"
	out := make([]byte, n)
	for i := range out {
		out[i] = digits[int(b[i])%10]
	}
	return string(out)
}

func randomBase(n int) string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = chars[int(b[i])%len(chars)]
	}
	return string(b)
}

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v" || os.Args[1] == "version") {
		fmt.Println(panelVersion)
		return
	}
	if v := os.Getenv("GRE_PANEL_DIR"); v != "" {
		configDir = v
	}
	loadOrInit()
	ensureFreeHTTPPort()
	if _, err := rand.Read(nonce[:]); err != nil {
		log.Fatal(err)
	}

	base := "/" + cfg.BasePath
	mux := http.NewServeMux()
	panelMux = mux
	mux.HandleFunc("GET "+base+"/", serveIndex)
	mux.HandleFunc("GET "+base+"/tokens.css", serveAsset("tokens.css", "text/css; charset=utf-8"))
	mux.HandleFunc("GET "+base+"/fonts.css", serveAsset("fonts.css", "text/css; charset=utf-8"))
	mux.HandleFunc("GET "+base+"/fonts/vazirmatn.woff2", serveAsset("fonts/vazirmatn.woff2", "font/woff2"))
	mux.HandleFunc("GET "+base+"/base.css", serveAsset("base.css", "text/css; charset=utf-8"))
	mux.HandleFunc("GET "+base+"/enterprise.css", serveAsset("enterprise.css", "text/css; charset=utf-8"))
	mux.HandleFunc("GET "+base+"/favicon.png", serveAsset("favicon.png", "image/png"))
	mux.HandleFunc("GET "+base+"/xterm.js", serveAsset("xterm.js", "text/javascript; charset=utf-8"))
	mux.HandleFunc("GET "+base+"/xterm-fit.js", serveAsset("xterm-fit.js", "text/javascript; charset=utf-8"))
	mux.HandleFunc("GET "+base+"/xterm-search.js", serveAsset("xterm-search.js", "text/javascript; charset=utf-8"))
	mux.HandleFunc("GET "+base+"/xterm.css", serveAsset("xterm.css", "text/css; charset=utf-8"))
	mux.HandleFunc("GET "+base+"/api/term/ws", requireAuth(handleTermWS))
	mux.HandleFunc("GET "+base+"/api/term/status", requireAuth(handleTermStatus))
	mux.HandleFunc("POST "+base+"/api/term/kill", requireAuth(handleTermKill))
	mux.HandleFunc("POST "+base+"/api/term/enable", requireAuth(handleTermEnable))
	mux.HandleFunc("GET "+base+"/api/health", handleHealth)
	mux.HandleFunc("GET "+base+"/api/status", requireAuth(handleStatus))
	mux.HandleFunc("GET "+base+"/api/dashboard", requireAuth(handleDashboard))
	mux.HandleFunc("POST "+base+"/api/login", handleLogin)
	mux.HandleFunc("POST "+base+"/api/logout", handleLogout)
	mux.HandleFunc("GET "+base+"/api/logs", requireAuth(handleLogs))
	mux.HandleFunc("GET "+base+"/api/errors", requireAuth(handleErrors))
	mux.HandleFunc("POST "+base+"/api/action", requireAuth(handleAction))
	mux.HandleFunc("POST "+base+"/api/password", requireAuth(handlePassword))
	mux.HandleFunc("GET "+base+"/api/setup", requireAuth(handleSetupGet))
	mux.HandleFunc("GET "+base+"/api/version", requireAuth(handleVersion))
	mux.HandleFunc("POST "+base+"/api/update", requireAuth(handleUpdate))
	mux.HandleFunc("POST "+base+"/api/setup", requireAuth(handleSetupPost))
	mux.HandleFunc("GET "+base+"/api/peers", requireAuth(handlePeersGet))
	mux.HandleFunc("POST "+base+"/api/peers", requireAuth(handlePeersPost))
	mux.HandleFunc("GET "+base+"/api/tls", requireAuth(handleTLSGet))
	mux.HandleFunc("POST "+base+"/api/tls", requireAuth(handleTLSIssue))
	mux.HandleFunc("POST "+base+"/api/tls/renew", requireAuth(handleTLSRenew))
	mux.HandleFunc("GET "+base+"/api/watchdog", requireAuth(handleWatchdogGet))
	mux.HandleFunc("POST "+base+"/api/watchdog", requireAuth(handleWatchdogPost))
	mux.HandleFunc("GET "+base+"/api/watchdog/backup-download", requireAuth(handleBackupDownload))
	mux.HandleFunc("POST "+base+"/api/watchdog/backup-download", requireAuth(handleBackupDownload))
	mux.HandleFunc("GET "+base+"/api/perf", requireAuth(handlePerfGet))
	mux.HandleFunc("POST "+base+"/api/perf", requireAuth(handlePerfPost))
	mux.HandleFunc("GET "+base+"/api/carrier", requireAuth(handleCarrierGet))
	mux.HandleFunc("POST "+base+"/api/carrier", requireAuth(handleCarrierPost))
	mux.HandleFunc("GET "+base+"/api/doctor", requireAuth(handleDoctorGet))
	mux.HandleFunc("POST "+base+"/api/doctor", requireAuth(handleDoctorPost))

	addr := fmt.Sprintf(":%d", cfg.Port)
	log.Printf("gre-panel listening on %s under /%s", addr, cfg.BasePath)
	go startHTTPSListener()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		next := pickFreePort(cfg.Port + 1)
		log.Printf("panel port %d busy — auto-switched to %d (saved to panel.json)", cfg.Port, next)
		cfg.Port = next
		saveCfg()
		addr = fmt.Sprintf(":%d", cfg.Port)
		log.Printf("gre-panel listening on %s under /%s", addr, cfg.BasePath)
		ln, err = net.Listen("tcp", addr)
		if err != nil {
			log.Fatal(err)
		}
	}
	log.Fatal(http.Serve(ln, mux))
}

func serveAsset(name, ctype string) http.HandlerFunc {
	// Pre-compress text assets once at first request: on lossy Iran links,
	// xterm.js (283KB) stalled mid-transfer while small files passed through.
	// gzip shrinks it to ~70KB so the panel loads instantly.
	type blob struct {
		raw []byte
		gz  []byte
	}
	var mu sync.Mutex
	cache := map[string]*blob{}
	gzOK := func(ct string) bool {
		return strings.HasPrefix(ct, "text/") || strings.Contains(ct, "javascript") || strings.HasSuffix(ct, ".woff2") || ct == "font/woff2"
	}
	return func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		b, ok := cache[name]
		if !ok {
			data, err := panelFS.ReadFile(name)
			mu.Unlock()
			if err != nil {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			b = &blob{raw: data}
			if gzOK(ctype) {
				var buf bytes.Buffer
				gw := gzip.NewWriter(&buf)
				_, _ = gw.Write(data)
				_ = gw.Close()
				b.gz = buf.Bytes()
			}
			mu.Lock()
			cache[name] = b
			mu.Unlock()
		} else {
			mu.Unlock()
		}
		w.Header().Set("Content-Type", ctype)
		w.Header().Set("Cache-Control", "public, max-age=86400")
		if len(b.gz) > 0 && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			w.Header().Set("Content-Encoding", "gzip")
			w.Header().Set("Vary", "Accept-Encoding")
			_, _ = w.Write(b.gz)
			return
		}
		_, _ = w.Write(b.raw)
	}
}

func serveIndex(w http.ResponseWriter, r *http.Request) {
	data, err := panelFS.ReadFile("index.html")
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	page := strings.ReplaceAll(string(data), "__BASE_PATH__", "/"+cfg.BasePath)
	body := []byte(page)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") && len(body) > 1024 {
		var buf bytes.Buffer
		gw := gzip.NewWriter(&buf)
		_, _ = gw.Write(body)
		_ = gw.Close()
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Vary", "Accept-Encoding")
		_, _ = w.Write(buf.Bytes())
		return
	}
	_, _ = w.Write(body)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// handleHealth is unauthenticated: lets browsers/proxies verify the panel
// is reachable without exposing any data.
func handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{"status": "ok", "version": panelVersion})
}
