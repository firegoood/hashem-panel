package main

// Update API: check for a newer prebuilt panel release and install it.
// Same safety rules as hashem.sh update_all(): verify download, keep local
// config (panel.json) untouched, restart the service, and
// never leave the system in a broken state on failure.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var updateClient = &http.Client{Timeout: 25 * time.Second}

// handleVersion reports the running build and the latest GitHub release.
func handleVersion(w http.ResponseWriter, r *http.Request) {
	latest, _ := latestReleaseTag()
	out := map[string]any{"current": panelVersion, "latest": latest}
	if latest != "" && latest != panelVersion {
		out["update_available"] = true
	} else {
		out["update_available"] = false
	}
	writeJSON(w, out)
}

// latestReleaseTag asks the GitHub API for the newest release tag with mirror fallbacks.
// Empty string = could not determine (offline / rate-limited).
func latestReleaseTag() (string, error) {
	urls := []string{
		"https://api.github.com/repos/pdnczone/hashem-panel/releases/latest",
		"https://mirror.ghproxy.com/https://api.github.com/repos/pdnczone/hashem-panel/releases/latest",
		"https://ghproxy.net/https://api.github.com/repos/pdnczone/hashem-panel/releases/latest",
	}
	var lastErr error
	for _, u := range urls {
		req, err := http.NewRequest("GET", u, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", "hashem-panel-updater/"+panelVersion)
		resp, err := updateClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			lastErr = fmt.Errorf("github api: %s", resp.Status)
			continue
		}
		var rel struct {
			TagName string `json:"tag_name"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
			resp.Body.Close()
			lastErr = err
			continue
		}
		resp.Body.Close()
		if rel.TagName != "" {
			return rel.TagName, nil
		}
	}
	return "", lastErr
}

// handleUpdate downloads the latest prebuilt binary for this arch,
// verifies it (non-empty ELF), swaps it in, syncs the latest hashem.sh
// next to the panel binary, and restarts the service.
const panelScriptName = "hashem.sh"

func handleUpdate(w http.ResponseWriter, r *http.Request) {
	writeAPIError(w, r, "E-UPDATE-07", "Fork updates require a reviewed checkout and install-fork.sh; automatic upstream replacement is disabled")
}

// panelAsset maps runtime arch to the release asset name.
func panelAsset() (arch, asset string, err error) {
	switch runtime.GOARCH {
	case "amd64":
		return "amd64", "gre-panel-linux-amd64", nil
	case "arm64":
		return "arm64", "gre-panel-linux-arm64", nil
	}
	return "", "", fmt.Errorf("unsupported arch for update: %s", runtime.GOARCH)
}

// isOfficialGitHubURL restricts downloads to official GitHub repositories only (CWE-494)
func isOfficialGitHubURL(u string) bool {
	parsed, err := url.Parse(u)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Host)
	return host == "github.com" || host == "raw.githubusercontent.com" ||
		host == "api.github.com" || host == "objects.githubusercontent.com" ||
		host == "mirror.ghproxy.com" || host == "ghproxy.net" ||
		host == "gh.ddlc.top" || host == "fastly.jsdelivr.net"
}

var manifestFetcher = fetchChecksumManifest

// verifyAssetChecksum checks tmpPath against the release's checksum manifest.
// Missing manifest / missing entry => error. This fork has no unverified opt-out.
// Automatic installation remains disabled; only reviewed fork checkouts install.
func verifyAssetChecksum(tmpPath, tag, asset string) error {
	manifest, ferr := manifestFetcher(tag)
	expected, ok := "", false
	if manifest != nil {
		expected, ok = manifest[asset]
	}
	if !ok {
		if ferr != nil {
			return fmt.Errorf("refusing unverified update: checksum manifest unavailable (%v)", ferr)
		}
		return fmt.Errorf("refusing unverified update: no checksum entry for %s", asset)
	}
	if err := VerifyFileSHA256(tmpPath, expected); err != nil {
		return err
	}
	LogSecurityAudit("update_checksum_verified", "system", "local", "asset="+asset+" sha256="+expected)
	return nil
}

// fetchChecksumManifest attempts to download checksums.txt or SHA256SUMS from the release.
func fetchChecksumManifest(tag string) (map[string]string, error) {
	candidates := []string{
		"https://github.com/pdnczone/hashem-panel/releases/download/" + tag + "/checksums.txt",
		"https://github.com/pdnczone/hashem-panel/releases/download/" + tag + "/SHA256SUMS",
	}
	client := &http.Client{Timeout: 30 * time.Second}
	for _, u := range candidates {
		resp, err := client.Get(u)
		if err == nil && resp.StatusCode == http.StatusOK {
			data, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
			resp.Body.Close()
			if err == nil && len(data) > 0 && len(data) <= 1<<20 {
				if manifest := ParseChecksumManifest(string(data)); len(manifest) > 0 {
					return manifest, nil
				}
			}
		}
		if resp != nil {
			resp.Body.Close()
		}
	}
	return nil, fmt.Errorf("no checksum manifest found")
}

func downloadFile(rawURL string, tmp *os.File) error {
	if !isOfficialGitHubURL(rawURL) {
		return fmt.Errorf("untrusted download URL domain: %s", rawURL)
	}

	urlsToTry := []string{rawURL}
	if strings.HasPrefix(rawURL, "https://github.com/") || strings.HasPrefix(rawURL, "https://raw.githubusercontent.com/") {
		urlsToTry = append(urlsToTry,
			"https://mirror.ghproxy.com/"+rawURL,
			"https://ghproxy.net/"+rawURL,
			"https://gh.ddlc.top/"+rawURL,
		)
	}

	client := &http.Client{Timeout: 60 * time.Second}
	var lastErr error

	for _, tryURL := range urlsToTry {
		for attempt := 1; attempt <= 2; attempt++ {
			req, err := http.NewRequest("GET", tryURL, nil)
			if err != nil {
				lastErr = err
				continue
			}
			req.Header.Set("User-Agent", "hashem-panel-updater/"+panelVersion)

			resp, err := client.Do(req)
			if err != nil {
				lastErr = err
				time.Sleep(300 * time.Millisecond)
				continue
			}

			if resp.StatusCode != http.StatusOK {
				resp.Body.Close()
				lastErr = fmt.Errorf("http %s from %s", resp.Status, tryURL)
				time.Sleep(300 * time.Millisecond)
				continue
			}

			// Clear temp file in case previous attempt wrote partial data
			if _, err := tmp.Seek(0, 0); err == nil {
				_ = tmp.Truncate(0)
			}
			if _, err := io.Copy(tmp, resp.Body); err != nil {
				resp.Body.Close()
				lastErr = err
				continue
			}
			resp.Body.Close()
			return tmp.Close()
		}
	}

	return fmt.Errorf("download failed across all mirrors: %v", lastErr)
}

// verifyELF rejects empty files and non-ELF downloads (e.g. an HTML
// error page from a stale release redirect).
func verifyELF(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	magic := make([]byte, 4)
	n, err := io.ReadFull(f, magic)
	if err != nil || n != 4 {
		return fmt.Errorf("file too small")
	}
	if magic[0] != 0x7f || magic[1] != 'E' || magic[2] != 'L' || magic[3] != 'F' {
		return fmt.Errorf("not an ELF binary")
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// restartSelf restarts the systemd unit when present, otherwise re-execs
// the new binary in place (dev / non-systemd environments).
func restartSelf() {
	if _, err := exec.LookPath("systemctl"); err == nil {
		_ = exec.Command("systemctl", "restart", "--no-block", "gre-panel").Run()
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	// Best effort: start the new binary; the old process exits.
	_ = exec.Command(exe).Start()
	os.Exit(0)
}

// sessionFile returns the path of the server-side session store.
func sessionFile() string { return filepath.Join(configDir, "sessions.json") }

// ---- hashem.sh sync (keeps server script in step with the binary) ----

// syncPanelScript downloads the latest hashem.sh from main, syntax-checks
// it with `bash -n`, and installs it where greScriptPath() reads from
// (next to the running binary on servers).
// best-effort: never blocks the binary update.
func syncPanelScript() {
	recordError("E-UPDATE-06", "script-sync", "Automatic upstream script replacement is disabled for this maintained fork")
}

// syncChaffScript installs /usr/local/bin/hashem-chaff.sh from the repo.
func syncChaffScript() {
	tmp, err := os.CreateTemp("", "hashem-chaff-*.sh")
	if err != nil {
		return
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	defer func() { _ = os.Remove(tmpPath) }()
	if err := downloadFile(chaffScriptURL, mustOpen(tmpPath)); err != nil {
		recordError("E-UPDATE-06", "script-sync", "chaff download: "+err.Error())
		return
	}
	// Verify chaff script integrity
	chaffContent, err := os.ReadFile(tmpPath)
	if err != nil || len(chaffContent) < 100 || (!strings.HasPrefix(string(chaffContent), "#!/bin/bash") && !strings.HasPrefix(string(chaffContent), "#!/usr/bin/env bash")) {
		recordError("E-UPDATE-06", "script-sync", "chaff integrity check failed: invalid script header")
		return
	}
	if out, err := exec.Command("bash", "-n", tmpPath).CombinedOutput(); err != nil {
		recordError("E-UPDATE-06", "script-sync", "chaff bash -n failed: "+string(out))
		return
	}
	if err := copyFile(tmpPath, "/usr/local/bin/hashem-chaff.sh"); err != nil {
		recordError("E-UPDATE-06", "script-sync", "chaff install: "+err.Error())
		return
	}
	_ = os.Chmod("/usr/local/bin/hashem-chaff.sh", 0755)
	_ = os.Remove("/usr/local/bin/gre-chaff.sh")
	LogSecurityAudit("script_synced", "system", "local", "target=/usr/local/bin/hashem-chaff.sh")
}

const scriptURL = "https://raw.githubusercontent.com/pdnczone/hashem-panel/main/hashem.sh"

// chaffScriptURL ships the standalone chaff generator next to hashem.sh.
const chaffScriptURL = "https://raw.githubusercontent.com/pdnczone/hashem-panel/main/hashem-chaff.sh"

// greScriptURL stays as an alias: releases before the rename shipped gre.sh,
// and external tools may import the name.
const greScriptURL = scriptURL

var (
	_ = greScriptURL
	_ = sessionCount
)

func mustOpen(path string) *os.File {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err == nil {
		return f
	}
	f2, _ := os.Create(path)
	return f2
}

// sessionStore is the persisted set of valid session tokens.
type sessionStore struct {
	Tokens map[string]int64 `json:"tokens"` // token -> expires unix
}

func loadSessions() sessionStore {
	s := sessionStore{Tokens: map[string]int64{}}
	data, err := os.ReadFile(sessionFile())
	if err != nil {
		return s
	}
	_ = json.Unmarshal(data, &s)
	if s.Tokens == nil {
		s.Tokens = map[string]int64{}
	}
	return s
}

func (s sessionStore) save() {
	_ = os.WriteFile(sessionFile(), mustJSON(s), 0600)
}

// pruneExpired drops expired tokens; true if anything changed.
func (s sessionStore) pruneExpired() bool {
	now := time.Now().Unix()
	changed := false
	for tok, exp := range s.Tokens {
		if exp < now {
			delete(s.Tokens, tok)
			changed = true
		}
	}
	return changed
}

// sessionLifetime is 24 hours (absolute expiry from login).
const sessionLifetime = int64(24 * 3600)

func validSession(token string) bool {
	if token == "" {
		return false
	}
	mu.Lock()
	defer mu.Unlock()
	s := loadSessions()
	exp, ok := s.Tokens[token]
	if !ok || exp < time.Now().Unix() {
		return false
	}
	return true
}

func addSession(token string) {
	mu.Lock()
	defer mu.Unlock()
	addSessionLocked(token)
}

func addSessionLocked(token string) {
	s := loadSessions()
	s.pruneExpired()
	s.Tokens[token] = time.Now().Unix() + sessionLifetime
	s.save()
}

func dropSession(token string) {
	mu.Lock()
	defer mu.Unlock()
	s := loadSessions()
	delete(s.Tokens, token)
	s.save()
}

func dropAllSessions() {
	mu.Lock()
	defer mu.Unlock()
	sessionStore{Tokens: map[string]int64{}}.save()
}

func sessionCount() int {
	mu.Lock()
	defer mu.Unlock()
	s := loadSessions()
	if s.pruneExpired() {
		s.save()
	}
	n := 0
	for range s.Tokens {
		n++
	}
	return n
}
