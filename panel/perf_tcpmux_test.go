package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withTempConfigDir(t *testing.T) func() {
	t.Helper()
	old := configDir
	configDir = t.TempDir()
	return func() { configDir = old }
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
