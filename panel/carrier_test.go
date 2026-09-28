package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestCarrierDefaultsAndLoadSave(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "carrier-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	oldConfigDir := configDir
	configDir = tmpDir
	defer func() { configDir = oldConfigDir }()

	// Default when file is missing
	def := loadCarrierConfig()
	if def.Mode != "auto" {
		t.Fatalf("expected mode=auto, got %s", def.Mode)
	}
	if def.ActiveCarrier != "direct" {
		t.Fatalf("expected active_carrier=direct, got %s", def.ActiveCarrier)
	}
	if def.FOUPort1 != 443 || def.FOUPort2 != 55555 {
		t.Fatalf("expected fou_port1=443, fou_port2=55555, got %d, %d", def.FOUPort1, def.FOUPort2)
	}

	// Save and reload
	def.ActiveCarrier = "fou:443"
	def.SwitchCount = 1
	if err := saveCarrierConfig(def); err != nil {
		t.Fatal(err)
	}

	loaded := loadCarrierConfig()
	if loaded.ActiveCarrier != "fou:443" {
		t.Fatalf("expected active_carrier=fou:443, got %s", loaded.ActiveCarrier)
	}
	if loaded.SwitchCount != 1 {
		t.Fatalf("expected switch_count=1, got %d", loaded.SwitchCount)
	}
}

func TestCarrierAPIEndpoints(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "carrier-api-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	oldConfigDir := configDir
	configDir = tmpDir
	defer func() { configDir = oldConfigDir }()

	// Initial GET
	req := httptest.NewRequest("GET", "/api/carrier", nil)
	w := httptest.NewRecorder()
	handleCarrierGet(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp carrierStatusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Mode != "auto" || resp.ActiveCarrier != "direct" {
		t.Fatalf("unexpected response: %+v", resp)
	}

	// POST cycle_next
	postBody, _ := json.Marshal(carrierPostRequest{Action: "cycle_next"})
	req = httptest.NewRequest("POST", "/api/carrier", bytes.NewReader(postBody))
	w = httptest.NewRecorder()
	handleCarrierPost(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on cycle_next, got %d", w.Code)
	}

	var postResp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &postResp); err != nil {
		t.Fatal(err)
	}
	if postResp["status"] != "ok" {
		t.Fatalf("expected status=ok, got %+v", postResp)
	}

	// Verify carrier changed
	cfg := loadCarrierConfig()
	if cfg.ActiveCarrier != "fou:443" {
		t.Fatalf("expected active_carrier=fou:443 after cycle, got %s", cfg.ActiveCarrier)
	}
}
