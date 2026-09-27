package main

// Update API: check for a newer prebuilt panel release and install it.
// Same safety rules as hashem.sh update_all(): verify download, keep local
// config (panel.json / panel.pass) untouched, restart the service, and
// never leave the system in a broken state on failure.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

// latestReleaseTag asks the GitHub API for the newest panel-rN tag.
// Empty string = could not determine (offline / rate-limited); the
// frontend then shows "unknown" instead of failing.
func latestReleaseTag() (string, error) {
	resp, err := updateClient.Get("https://api.github.com/repos/pdnczone/hashem-panel/releases/latest")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github api: %s", resp.Status)
	}
	var rel struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", err
	}
	return rel.TagName, nil
}

// handleUpdate downloads the latest prebuilt binary for this arch,
// verifies it (non-empty ELF), swaps it in, syncs the latest hashem.sh
// next to the panel binary (the panel shells out to hashem.sh for all
// setup Peer/tunnel work — a stale hashem.sh would break add-peer with
// E-INSTALL-02 "Unknown command"), and restarts the service.
// panel.json / panel.pass are never touched, so local credentials survive.
const panelScriptName = "hashem.sh"

func handleUpdate(w http.ResponseWriter, r *http.Request) {
	arch, asset, err := panelAsset()
	if err != nil {
		writeAPIError(w, r, "E-UPDATE-05", err.Error())
		return
	}
	_ = arch
	latest, err := latestReleaseTag()
	if err != nil || latest == "" {
		writeAPIError(w, r, "E-UPDATE-01", "")
		return
	}
	if latest == panelVersion {
		writeJSON(w, map[string]string{"status": "ok", "detail": "already latest (" + panelVersion + ")"})
		return
	}
	dlURL := "https://github.com/pdnczone/hashem-panel/releases/download/" + latest + "/" + asset
	tmp, err := os.CreateTemp("", "gre-panel-update-*")
	if err != nil {
		writeAPIError(w, r, "E-UPDATE-04", "")
		return
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := downloadFile(dlURL, tmp); err != nil {
		writeAPIError(w, r, "E-UPDATE-02", err.Error())
		return
	}
	if err := verifyELF(tmpPath); err != nil {
		writeAPIError(w, r, "E-UPDATE-03", err.Error())
		return
	}
	exe, err := os.Executable()
	if err != nil {
		writeAPIError(w, r, "E-UPDATE-04", "")
		return
	}
	// Swap in the new binary. Keep a .bak so a bad binary can be rolled back.
	bak := exe + ".bak"
	_ = os.Remove(bak)
	if err := os.Rename(exe, bak); err != nil {
		writeAPIError(w, r, "E-UPDATE-04", err.Error())
		return
	}
	if err := copyFile(tmpPath, exe); err != nil {
		_ = os.Rename(bak, exe) // roll back
		writeAPIError(w, r, "E-UPDATE-04", err.Error())
		return
	}
	_ = os.Chmod(exe, 0755)
	// Keep hashem.sh in sync with the binary: find it next to the running
	// binary (servers: /usr/local/bin/hashem.sh) or via HASHEM_SCRIPT, download
	// the latest from main, syntax-check it, then replace. Best effort —
	// a failed script sync never blocks the binary update.
	syncPanelScript()
	writeJSON(w, map[string]string{"status": "ok", "detail": "updated to " + latest + " — restarting panel"})
	go func() {
		time.Sleep(500 * time.Millisecond)
		restartSelf()
	}()
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

func downloadFile(url string, tmp *os.File) error {
	client := &http.Client{Timeout: 90 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http %s", resp.Status)
	}
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		return err
	}
	return tmp.Close()
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
		_ = exec.Command("systemctl", "restart", "gre-panel").Run()
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
	target := greScriptTarget()
	if target == "" {
		return
	}
	dir := filepath.Dir(target)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			recordError("E-UPDATE-06", "script-sync", "mkdir "+dir+": "+err.Error())
			return
		}
	}
	tmp, err := os.CreateTemp("", "hashem-*.sh")
	if err != nil {
		recordError("E-UPDATE-06", "script-sync", "tmpfile: "+err.Error())
		return
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	defer func() { _ = os.Remove(tmpPath) }()
	if err := downloadFile(scriptURL, mustOpen(tmpPath)); err != nil {
		recordError("E-UPDATE-06", "script-sync", "download: "+err.Error())
		return
	}
	chk := exec.Command("bash", "-n", tmpPath)
	if out, err := chk.CombinedOutput(); err != nil {
		recordError("E-UPDATE-06", "script-sync", "bash -n failed: "+string(out))
		return
	}
	if err := copyFile(tmpPath, target); err != nil {
		recordError("E-UPDATE-06", "script-sync", "install: "+err.Error())
		return
	}
	_ = os.Chmod(target, 0755)
	// Migrate servers to the new name: refresh the hashem copies + legacy
	// gre.sh symlink, and drop a stale standalone gre.sh file (symlink wins
	// so old lookup paths keep working).
	for _, p := range []string{"/usr/local/bin/hashem.sh", "/usr/local/bin/hashem"} {
		if p == target {
			continue
		}
		if err := copyFile(tmpPath, p); err == nil {
			_ = os.Chmod(p, 0755)
		}
	}
	_ = os.Remove("/usr/local/bin/gre.sh")
	_ = os.Symlink("/usr/local/bin/hashem.sh", "/usr/local/bin/gre.sh")
	syncChaffScript()
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
}

const scriptURL = "https://raw.githubusercontent.com/pdnczone/hashem-panel/main/hashem.sh"

// chaffScriptURL ships the standalone chaff generator next to hashem.sh.
const chaffScriptURL = "https://raw.githubusercontent.com/pdnczone/hashem-panel/main/hashem-chaff.sh"

// greScriptURL stays as an alias: releases before the rename shipped gre.sh,
// and external tools may import the name.
const greScriptURL = scriptURL

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

// sessionLifetime is 30 days; each authenticated request extends it.
const sessionLifetime = int64(30 * 24 * 3600)

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
	// Sliding expiration: extend on every use.
	s.Tokens[token] = time.Now().Unix() + sessionLifetime
	s.save()
	return true
}

func addSession(token string) {
	mu.Lock()
	defer mu.Unlock()
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
