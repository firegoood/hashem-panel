package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func withTempConfigDir(t *testing.T) func() {
	t.Helper()
	old := configDir
	configDir = t.TempDir()
	return func() { configDir = old }
}

func TestGlobalPerfCannotMutateManagedPeers(t *testing.T) {
	defer withTempConfigDir(t)()
	for _, enabled := range []string{"true", "false"} {
		rec := httptest.NewRecorder()
		handlePerfPost(rec, httptest.NewRequest("POST", "/api/perf", strings.NewReader(`{"action":"update","tcp_mux":`+enabled+`}`)))
		if rec.Code != http.StatusOK || loadPerfConfig().TCPMux == nil || *loadPerfConfig().TCPMux != (enabled == "true") {
			t.Fatal("legacy API mux update did not persist")
		}
	}
	if err := writePeerRegistry([]peerRecord{{ID: 1, Managed: true, Revision: 7}}); err != nil {
		t.Fatal(err)
	}
	perfBefore, _ := os.ReadFile(perfConfigPath())
	registryBefore, _ := os.ReadFile(peersFile())
	for _, action := range []string{"update", "apply", "reset", "set-tuning", "set-chaff", "set-dpi"} {
		rec := httptest.NewRecorder()
		handlePerfPost(rec, httptest.NewRequest("POST", "/api/perf", strings.NewReader(`{"action":"`+action+`","tcp_mux":true}`)))
		if rec.Code == http.StatusOK || !strings.Contains(rec.Body.String(), "managed peers") {
			t.Fatal("global performance mutation was not refused", action)
		}
	}
	if _, err := runPerfCmd("perf", "apply"); err == nil {
		t.Fatal("direct performance command bypassed the API guard")
	}
	perfAfter, _ := os.ReadFile(perfConfigPath())
	registryAfter, _ := os.ReadFile(peersFile())
	if string(perfBefore) != string(perfAfter) || string(registryBefore) != string(registryAfter) {
		t.Fatal("refused mutation changed saved state")
	}
	if err := os.WriteFile(peersFile(), []byte(`{"peers":`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := legacyPerfGuard(); err == nil {
		t.Fatal("corrupt registry allowed global mutation")
	}
}

func TestPerfTCPMuxNativeFRPValidation(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("native Linux FRP validation")
	}
	if _, err := os.Stat(filepath.Join(managedBinaryDir, "frps")); err != nil {
		t.Skip("pinned FRP binaries not installed")
	}
	defer withTempConfigDir(t)()
	for _, enabled := range []bool{false, true} {
		c := defaultPerfConfig()
		c.TCPMux = boolPtr(enabled)
		if err := savePerfConfig(c); err != nil {
			t.Fatal(err)
		}
		configs := map[string]string{
			"frps": rescueFrpsToml(17001, "synthetic-native-token"),
			"frpc": rescueOriginFrpcToml(17001, "synthetic-native-token", "synthetic-native-secret", []int{8888}),
		}
		for binary, config := range configs {
			path := filepath.Join(configDir, binary+".toml")
			if err := os.WriteFile(path, []byte(config), 0600); err != nil {
				t.Fatal(err)
			}
			if out, err := exec.Command(filepath.Join(managedBinaryDir, binary), "verify", "-c", path).CombinedOutput(); err != nil {
				t.Fatalf("native %s mux=%v rejected config: %v %s", binary, enabled, err, out)
			}
		}
	}
}

func TestTCPMuxDefaultIsOff(t *testing.T) {
	defer withTempConfigDir(t)()
	if tcpMuxEnabled() {
		t.Fatal("tcpMux must default to OFF (speed-first) when perf.json has no tcp_mux key")
	}
	got := tcpMuxTomlLines()
	if !strings.Contains(got, "transport.tcpMux = false") || strings.Contains(got, "KeepaliveInterval") {
		t.Fatalf("unexpected default lines: %q", got)
	}
	if loadPerfConfig().TCPMux != nil {
		t.Fatal("unset tcp_mux must stay nil so apply leaves live tomls alone")
	}
}

func TestTCPMuxOnWritesKeepalive(t *testing.T) {
	defer withTempConfigDir(t)()
	c := loadPerfConfig()
	c.TCPMux = boolPtr(true)
	if err := savePerfConfig(c); err != nil {
		t.Fatal(err)
	}
	if !tcpMuxEnabled() {
		t.Fatal("expected enabled after save")
	}
	got := tcpMuxTomlLines()
	if !strings.Contains(got, "transport.tcpMux = true") || !strings.Contains(got, "tcpMuxKeepaliveInterval = 30") {
		t.Fatalf("unexpected lines: %q", got)
	}
}

func TestTCPMuxRoundTripJSON(t *testing.T) {
	defer withTempConfigDir(t)()
	c := loadPerfConfig()
	c.TCPMux = boolPtr(false)
	if err := savePerfConfig(c); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(configDir, "perf.json"))
	if !strings.Contains(string(data), `"tcp_mux": false`) {
		t.Fatalf("explicit false must be persisted, got %s", data)
	}
	if c2 := loadPerfConfig(); c2.TCPMux == nil || *c2.TCPMux {
		t.Fatal("explicit false must load back as non-nil false")
	}
}

func TestDefaultConfigLeavesMuxUnset(t *testing.T) {
	if defaultPerfConfig().TCPMux != nil {
		t.Fatal("default config must leave tcp_mux unset so existing live tomls are never silently rewritten")
	}
}
