package main

// Auth: cookie session + login/logout/password.
// Random tokens are validated against the private server-side session store.
// Changing the password invalidates all sessions.

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"net"
	"net/http"
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
	return ClientIP(r)
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
		LogSecurityAudit("lockout_triggered", "unknown", ip, "5 failed attempts within window")
	}
}

func recordLoginSuccess(ip string) {
	attemptMu.Lock()
	delete(attempts, ip)
	attemptMu.Unlock()
}

func isRequestHTTPS(r *http.Request) bool {
	if r == nil {
		return false
	}
	if r.TLS != nil {
		return true
	}
	remoteHost, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		remoteHost = strings.Trim(r.RemoteAddr, "[]")
	}
	if IsTrustedProxy(remoteHost) {
		proto := r.Header.Get("X-Forwarded-Proto")
		if strings.EqualFold(proto, "https") {
			return true
		}
	}
	return false
}

func sessionCookie(r *http.Request, value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     "gre_session",
		Value:    value,
		Path:     "/" + cfg.BasePath + "/",
		HttpOnly: true,
		Secure:   isRequestHTTPS(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	}
}

func csrfCookie(r *http.Request, value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     "gre_csrf",
		Value:    value,
		Path:     "/" + cfg.BasePath + "/",
		HttpOnly: false, // Accessible to frontend scripts to include in X-CSRF-Token header
		Secure:   isRequestHTTPS(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	}
}

func authed(r *http.Request) bool {
	c, err := r.Cookie("gre_session")
	if err != nil || c.Value == "" {
		return false
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
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	ip := clientIP(r)
	if checkLocked(ip) {
		writeAPIError(w, r, "E-AUTH-02", "Too many failed attempts. Temporarily locked.")
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
	mu.Lock()
	loginHash, loginUser := cfg.PassHash, cfg.Username
	mu.Unlock()
	valid, legacy := verifyPassword(loginHash, body.Password)
	if subtle.ConstantTimeCompare([]byte(body.Username), []byte(loginUser)) != 1 ||
		!valid {
		recordLoginFailure(ip)
		LogSecurityAudit("login_failed", body.Username, ip, "invalid credentials")
		writeAPIError(w, r, "E-AUTH-02", "")
		return
	}
	if legacy {
		hash, err := hashPassword(body.Password)
		if err != nil {
			writeAPIError(w, r, "E-AUTH-05", "")
			return
		}
		mu.Lock()
		if cfg.PassHash != loginHash {
			mu.Unlock()
			writeAPIError(w, r, "E-AUTH-05", "credentials changed; sign in again")
			return
		}
		previous := cfg.PassHash
		cfg.PassHash = hash
		if err = atomicPrivateFile(cfgPath(), mustJSON(cfg), 0600); err != nil {
			cfg.PassHash = previous
			mu.Unlock()
			writeAPIError(w, r, "E-AUTH-05", "")
			return
		}
		loginHash = hash
		sessionStore{Tokens: map[string]int64{}}.save()
		mu.Unlock()
	}
	recordLoginSuccess(ip)
	mu.Lock()
	if cfg.PassHash != loginHash {
		mu.Unlock()
		writeAPIError(w, r, "E-AUTH-05", "credentials changed; sign in again")
		return
	}
	tok, err := newSessionToken()
	if err != nil {
		mu.Unlock()
		writeAPIError(w, r, "E-AUTH-05", "")
		return
	}
	addSessionLocked(tok)
	mu.Unlock()

	csrfTok := GenerateCSRFToken(tok)
	http.SetCookie(w, sessionCookie(r, tok, 86400))
	http.SetCookie(w, csrfCookie(r, csrfTok, 86400))

	LogSecurityAudit("login_success", cfg.Username, ip, "authenticated successfully")
	writeJSON(w, map[string]string{
		"status":     "ok",
		"csrf_token": csrfTok,
	})
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("gre_session"); err == nil {
		dropSession(c.Value)
	}
	http.SetCookie(w, sessionCookie(r, "", -1))
	http.SetCookie(w, csrfCookie(r, "", -1))
	LogSecurityAudit("logout", cfg.Username, clientIP(r), "logged out")
	writeJSON(w, map[string]string{"status": "ok"})
}

func handlePassword(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	var body struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
		Password        string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, r, "E-AUTH-04", "invalid request body")
		return
	}

	ip := clientIP(r)

	// Verify current password
	mu.Lock()
	currentHash := cfg.PassHash
	mu.Unlock()
	valid, _ := verifyPassword(currentHash, body.CurrentPassword)
	if !valid {
		recordLoginFailure(ip)
		LogSecurityAudit("password_change_rejected", cfg.Username, ip, "incorrect current password")
		writeAPIError(w, r, "E-AUTH-07", "current password does not match")
		return
	}

	targetPass := strings.TrimSpace(body.NewPassword)
	if targetPass == "" {
		targetPass = strings.TrimSpace(body.Password)
	}

	// Password strength validation (NIST 800-63B)
	if err := ValidatePasswordStrength(targetPass); err != nil {
		LogSecurityAudit("password_change_rejected", cfg.Username, ip, "strength check failed: "+err.Error())
		writeAPIError(w, r, "E-AUTH-04", err.Error())
		return
	}

	hash, err := hashPassword(targetPass)
	if err != nil {
		writeAPIError(w, r, "E-AUTH-05", "")
		return
	}
	var replacementNonce [32]byte
	if _, err := rand.Read(replacementNonce[:]); err != nil {
		writeAPIError(w, r, "E-AUTH-05", "")
		return
	}
	mu.Lock()
	if cfg.PassHash != currentHash {
		mu.Unlock()
		writeAPIError(w, r, "E-AUTH-07", "credentials changed; sign in again")
		return
	}
	previous := cfg.PassHash
	cfg.PassHash = hash
	if err = atomicPrivateFile(cfgPath(), mustJSON(cfg), 0600); err != nil {
		cfg.PassHash = previous
		mu.Unlock()
		writeAPIError(w, r, "E-AUTH-05", "")
		return
	}
	nonce = replacementNonce
	sessionStore{Tokens: map[string]int64{}}.save()
	tok, err := newSessionToken()
	if err != nil {
		mu.Unlock()
		writeAPIError(w, r, "E-AUTH-05", "")
		return
	}
	addSessionLocked(tok)
	mu.Unlock()
	csrfTok := GenerateCSRFToken(tok)

	http.SetCookie(w, sessionCookie(r, tok, 86400))
	http.SetCookie(w, csrfCookie(r, csrfTok, 86400))

	LogSecurityAudit("password_changed", cfg.Username, ip, "password changed successfully")
	writeJSON(w, map[string]string{
		"status":     "ok",
		"csrf_token": csrfTok,
	})
}
