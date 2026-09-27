package main

// Panel TLS (Let's Encrypt): issue + serve HTTPS alongside plain HTTP.
// Design (user-confirmed): manual domain entry, HTTP stays on cfg.Port,
// HTTPS serves on cfg.TLSPort (default 7443) with the same base_path +
// same session cookie. Certbot does the ACME work (standalone http-01
// on :80); the panel shells out, copies fullchain+key into
// <configDir>/tls/, and serves them. Certbot's own timer handles
// renewal; the panel surfaces expiry + a manual Renew button.

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

func tlsDir() string      { return filepath.Join(configDir, "tls") }
func tlsCertFile() string { return filepath.Join(tlsDir(), "server.crt") }
func tlsKeyFile() string  { return filepath.Join(tlsDir(), "server.key") }
func tlsMetaFile() string { return filepath.Join(tlsDir(), "meta.json") }

type tlsMeta struct {
	Domain   string `json:"domain"`
	Email    string `json:"email,omitempty"`
	IssuedAt string `json:"issued_at"`
	Expiry   string `json:"expiry"`
	Issuer   string `json:"issuer,omitempty"`
}

var tlsDomainRe = regexp.MustCompile(`^(?i)([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,}$`)

func tlsMetaLoad() *tlsMeta {
	data, err := os.ReadFile(tlsMetaFile())
	if err != nil {
		return nil
	}
	var m tlsMeta
	if err := json.Unmarshal(data, &m); err != nil {
		return nil
	}
	return &m
}

func tlsHasCert() bool {
	if _, err := os.Stat(tlsCertFile()); err != nil {
		return false
	}
	if _, err := os.Stat(tlsKeyFile()); err != nil {
		return false
	}
	return true
}

func tlsCertExpiry() (time.Time, string, error) {
	data, err := os.ReadFile(tlsCertFile())
	if err != nil {
		return time.Time{}, "", err
	}
	var blk *pem.Block
	for {
		blk, data = pem.Decode(data)
		if blk == nil {
			break
		}
		if blk.Type == "CERTIFICATE" {
			c, err := x509.ParseCertificate(blk.Bytes)
			if err != nil {
				continue
			}
			return c.NotAfter, c.Issuer.CommonName, nil
		}
	}
	return time.Time{}, "", fmt.Errorf("no certificate found")
}

// tlsStatusJSON is shared by the API + the hashem.sh CLI banner.
func tlsStatusJSON() map[string]any {
	out := map[string]any{
		"http_port":  cfg.Port,
		"https_port": effectiveTLSPort(),
		"http_url":   tlsHTTPURL(),
		"enabled":    tlsHasCert(),
	}
	if m := tlsMetaLoad(); m != nil {
		out["domain"] = m.Domain
		out["issued_at"] = m.IssuedAt
	}
	if tlsHasCert() {
		exp, issuer, err := tlsCertExpiry()
		if err == nil {
			out["expiry"] = exp.Format("2006-01-02 15:04:05")
			out["issuer"] = issuer
			days := int(time.Until(exp).Hours() / 24)
			out["days_left"] = days
			out["expiring_soon"] = days < 15
			out["https_url"] = tlsHTTPSURL()
		} else {
			out["cert_error"] = err.Error()
		}
	}
	return out
}

func tlsHTTPURL() string {
	ip := detectPublicIP()
	if ip == "" {
		ip = "<this-server-ip>"
	}
	return fmt.Sprintf("http://%s:%d/%s", ip, cfg.Port, cfg.BasePath)
}

func tlsHTTPSURL() string {
	m := tlsMetaLoad()
	host := ""
	if m != nil {
		host = m.Domain
	}
	if host == "" {
		host = detectPublicIP()
		if host == "" {
			host = "<this-server-ip>"
		}
	}
	return fmt.Sprintf("https://%s:%d/%s", host, effectiveTLSPort(), cfg.BasePath)
}

// GET /api/tls — status (enabled/domain/expiry/urls).
func handleTLSGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, tlsStatusJSON())
}

// POST /api/tls {"domain":"panel.example.com","email":"..."} — certbot standalone.
func handleTLSIssue(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Domain string `json:"domain"`
		Email  string `json:"email"`
		Port   int    `json:"https_port"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, r, "E-TLS-01", "")
		return
	}
	body.Domain = strings.ToLower(strings.TrimSpace(body.Domain))
	body.Email = strings.TrimSpace(body.Email)
	if !tlsDomainRe.MatchString(body.Domain) {
		writeAPIError(w, r, "E-TLS-02", "")
		return
	}
	if body.Port != 0 {
		if body.Port < 1 || body.Port > 65535 {
			writeAPIError(w, r, "E-TLS-03", "")
			return
		}
		cfg.TLSPort = body.Port
		_ = os.WriteFile(cfgPath(), mustJSON(cfg), 0600)
	}
	if _, err := exec.LookPath("certbot"); err != nil {
		writeAPIError(w, r, "E-TLS-04", "")
		return
	}
	args := []string{"certonly", "--standalone", "--non-interactive", "--agree-tos",
		"--preferred-challenges", "http", "--http-01-port", "80",
		"-d", body.Domain}
	if body.Email != "" {
		args = append(args, "-m", body.Email)
	} else {
		args = append(args, "--register-unsafely-without-email")
	}
	out, err := exec.Command("certbot", args...).CombinedOutput()
	if err != nil {
		recordError("E-TLS-05", r.Method+" "+r.URL.Path, strings.TrimSpace(string(out)))
		info := errCatalog["E-TLS-05"]
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(info.Status)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error_code": "E-TLS-05", "error": strings.TrimSpace(string(out)), "hint": info.Hint,
		})
		return
	}
	live := filepath.Join("/etc/letsencrypt/live", body.Domain)
	if err := tlsInstallFrom(live, body.Domain, body.Email); err != nil {
		writeAPIError(w, r, "E-TLS-06", err.Error())
		return
	}
	go startHTTPSListener()
	writeJSON(w, tlsStatusJSON())
}

// POST /api/tls/renew — certbot renew --cert-name domain, re-install, reload.
func handleTLSRenew(w http.ResponseWriter, r *http.Request) {
	m := tlsMetaLoad()
	if m == nil || m.Domain == "" {
		writeAPIError(w, r, "E-TLS-07", "")
		return
	}
	if _, err := exec.LookPath("certbot"); err != nil {
		writeAPIError(w, r, "E-TLS-04", "")
		return
	}
	out, err := exec.Command("certbot", "renew", "--cert-name", m.Domain, "--quiet").CombinedOutput()
	if err != nil {
		recordError("E-TLS-05", r.Method+" "+r.URL.Path, strings.TrimSpace(string(out)))
		info := errCatalog["E-TLS-05"]
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(info.Status)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error_code": "E-TLS-05", "error": strings.TrimSpace(string(out)), "hint": info.Hint,
		})
		return
	}
	live := filepath.Join("/etc/letsencrypt/live", m.Domain)
	if err := tlsInstallFrom(live, m.Domain, m.Email); err != nil {
		writeAPIError(w, r, "E-TLS-06", err.Error())
		return
	}
	go startHTTPSListener()
	writeJSON(w, tlsStatusJSON())
}

// startHTTPSListener serves the same mux over TLS when a cert exists.
// Called at boot + after every issue/renew. Restart-safe: stops the
// previous listener (if any) before binding, so renew doesn't stack.
// Auto port: if the saved TLS port is busy (e.g. 7443 taken), scans
// upward and persists the new port so the API/CLI display the real one.
var httpsSrv *http.Server

func startHTTPSListener() {
	if !tlsHasCert() {
		return
	}
	if httpsSrv != nil {
		_ = httpsSrv.Close()
		httpsSrv = nil
	}
	port := effectiveTLSPort()
	if !portFree(port) {
		next := pickFreePort(port + 1)
		log.Printf("panel https port %d busy — auto-switched to %d (saved to panel.json)", port, next)
		cfg.TLSPort = next
		saveCfg()
		port = next
	}
	httpsSrv = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: panelMux,
	}
	go func(srv *http.Server) {
		if err := srv.ListenAndServeTLS(tlsCertFile(), tlsKeyFile()); err != nil && err != http.ErrServerClosed {
			recordError("E-TLS-08", "HTTPS listener", err.Error())
		}
	}(httpsSrv)
}

// tlsInstallFrom copies fullchain/privkey into <configDir>/tls + writes meta.
func tlsInstallFrom(liveDir, domain, email string) error {
	crt, err := os.ReadFile(filepath.Join(liveDir, "fullchain.pem"))
	if err != nil {
		return fmt.Errorf("read fullchain.pem: %w", err)
	}
	key, err := os.ReadFile(filepath.Join(liveDir, "privkey.pem"))
	if err != nil {
		return fmt.Errorf("read privkey.pem: %w", err)
	}
	if _, err := tls.X509KeyPair(crt, key); err != nil {
		return fmt.Errorf("keypair invalid: %w", err)
	}
	_ = os.MkdirAll(tlsDir(), 0700)
	if err := os.WriteFile(tlsCertFile(), crt, 0600); err != nil {
		return err
	}
	if err := os.WriteFile(tlsKeyFile(), key, 0600); err != nil {
		return err
	}
	exp, issuer, _ := tlsCertExpiry()
	meta := tlsMeta{
		Domain:   domain,
		Email:    email,
		IssuedAt: time.Now().Format("2006-01-02 15:04:05"),
		Expiry:   exp.Format("2006-01-02 15:04:05"),
		Issuer:   issuer,
	}
	_ = os.WriteFile(tlsMetaFile(), mustJSON(meta), 0600)
	recordError("E-TLS-00", "TLS "+domain, "certificate installed, expires "+meta.Expiry)
	return nil
}
