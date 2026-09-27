package main

// Auth: cookie session + login/logout/password.
// Session token = sha256(nonce + pass_hash); nonce is generated at startup
// (see main.go). Changing the password invalidates all sessions.

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	mu    sync.Mutex
	nonce [32]byte
)

const (
	loginFailWindow = 15 * time.Minute
	loginLockoutDur = 15 * time.Minute
	loginMaxFails   = 5
)

type ipAttempts struct {
	fails       []time.Time
	lockedUntil time.Time
}

var (
	attemptMu sync.Mutex
	attempts  = map[string]*ipAttempts{}
)

func clientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		ip := strings.TrimSpace(parts[0])
		if ip != "" {
			if host, _, err := net.SplitHostPort(ip); err == nil {
				return host
			}
			return strings.Trim(ip, "[]")
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return strings.Trim(r.RemoteAddr, "[]")
}

func pruneAttemptsLocked(now time.Time) {
	cutoff := now.Add(-loginFailWindow)
	for ip, a := range attempts {
		if now.Before(a.lockedUntil) {
			continue
		}
		n := 0
		for _, t := range a.fails {
			if t.After(cutoff) {
				a.fails[n] = t
				n++
			}
		}
		a.fails = a.fails[:n]
		if len(a.fails) == 0 {
			delete(attempts, ip)
		}
	}
}

func checkLocked(ip string) bool {
	attemptMu.Lock()
	defer attemptMu.Unlock()
	now := time.Now()
	pruneAttemptsLocked(now)
	a, ok := attempts[ip]
	if !ok {
		return false
	}
	return now.Before(a.lockedUntil)
}

func recordLoginFailure(ip string) {
	attemptMu.Lock()
	now := time.Now()
	pruneAttemptsLocked(now)
	a, ok := attempts[ip]
	if !ok {
		a = &ipAttempts{}
		attempts[ip] = a
	}
	a.fails = append(a.fails, now)
	shouldLog := false
	if len(a.fails) >= loginMaxFails && (a.lockedUntil.IsZero() || !now.Before(a.lockedUntil)) {
		a.lockedUntil = now.Add(loginLockoutDur)
		shouldLog = true
	}
	attemptMu.Unlock()

	if shouldLog {
		recordError("E-AUTH-02", "login", "brute-force lockout for IP "+ip+" (5 failures)")
	}
}

func recordLoginSuccess(ip string) {
	attemptMu.Lock()
	delete(attempts, ip)
	attemptMu.Unlock()
}

func sessionCookie(value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     "gre_session",
		Value:    value,
		Path:     "/" + cfg.BasePath + "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	}
}

func authed(r *http.Request) bool {
	c, err := r.Cookie("gre_session")
	if err != nil || c.Value == "" {
		return false
	}
	mac := sha256.Sum256(append(nonce[:], []byte(cfg.PassHash)...))
	want := hex.EncodeToString(mac[:])
	if subtle.ConstantTimeCompare([]byte(c.Value), []byte(want)) == 1 {
		return true
	}
	// persistent server-side sessions (survive restarts, 24h absolute)
	return validSession(c.Value)
}

func requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("gre_session")
		if err != nil || c.Value == "" {
			writeAPIError(w, r, "E-AUTH-01", "")
			return
		}
		if !authed(r) {
			writeAPIError(w, r, "E-AUTH-06", "")
			return
		}
		next(w, r)
	}
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if checkLocked(ip) {
		writeAPIError(w, r, "E-AUTH-02", "")
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, r, "E-AUTH-03", "")
		return
	}
	h := sha256.Sum256([]byte(body.Password))
	got := hex.EncodeToString(h[:])
	if subtle.ConstantTimeCompare([]byte(body.Username), []byte(cfg.Username)) != 1 ||
		subtle.ConstantTimeCompare([]byte(got), []byte(cfg.PassHash)) != 1 {
		recordLoginFailure(ip)
		writeAPIError(w, r, "E-AUTH-02", "")
		return
	}
	recordLoginSuccess(ip)
	mac := sha256.Sum256(append(nonce[:], []byte(cfg.PassHash)...))
	tok := hex.EncodeToString(mac[:])
	addSession(tok) // persistent: survives restarts, 24h absolute expiry
	http.SetCookie(w, sessionCookie(tok, 86400))
	writeJSON(w, map[string]string{"status": "ok"})
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("gre_session"); err == nil {
		dropSession(c.Value)
	}
	http.SetCookie(w, sessionCookie("", -1))
	writeJSON(w, map[string]string{"status": "ok"})
}

func handlePassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Password) < 4 {
		writeAPIError(w, r, "E-AUTH-04", "")
		return
	}
	mu.Lock()
	h := sha256.Sum256([]byte(body.Password))
	cfg.PassHash = hex.EncodeToString(h[:])
	_ = os.WriteFile(cfgPath(), mustJSON(cfg), 0600)
	// keep plaintext copy in sync (user choice: viewable via script menu)
	_ = os.WriteFile(filepath.Join(configDir, "panel.pass"), []byte(body.Password), 0600)
	if _, err := rand.Read(nonce[:]); err != nil {
		mu.Unlock()
		writeAPIError(w, r, "E-AUTH-05", "")
		return
	}
	mu.Unlock()
	// password change invalidates all other sessions (user choice).
	dropAllSessions()
	mac := sha256.Sum256(append(nonce[:], []byte(cfg.PassHash)...))
	tok := hex.EncodeToString(mac[:])
	addSession(tok) // keep the changer logged in
	http.SetCookie(w, sessionCookie(tok, 86400))
	writeJSON(w, map[string]string{"status": "ok"})
}
