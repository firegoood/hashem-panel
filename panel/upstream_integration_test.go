package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestManagedFleetRequiresAuthenticationAndRegistration(t *testing.T) {
	l := peerLive{peerRecord: peerRecord{ID: 1, Managed: true, State: "PENDING"}, GreUp: true, FrpUp: true, PingOK: true, Linked: true}
	if fleetHealth(l) != fleetDeg {
		t.Fatal("processes, ping and a TCP socket cannot authenticate a managed peer")
	}
	l.State, l.Authenticated, l.ProxyRegistration = "CONNECTED", true, "REGISTERED"
	l.PingOK = false
	if fleetHealth(l) != fleetOK {
		t.Fatal("real registered KCP connection must not depend on ICMP")
	}
	l.Disabled = true
	if fleetHealth(l) != fleetDown {
		t.Fatal("disabled peer cannot appear healthy from leftover processes")
	}
	l = peerLive{FrpUp: true, Linked: true}
	if fleetHealth(l) != fleetDeg {
		t.Fatal("legacy established socket alone cannot prove authentication")
	}
}

func TestLegacyDialCannotMutateManagedPeers(t *testing.T) {
	oldDir := configDir
	configDir = t.TempDir()
	t.Cleanup(func() { configDir = oldDir })
	if err := writePeerRegistry([]peerRecord{{ID: 1, Managed: true}}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(peersFile())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runHashemDial("public"); err == nil || !strings.Contains(err.Error(), "managed peers") {
		t.Fatal("legacy route must refuse managed configuration")
	}
	rec := httptest.NewRecorder()
	handleDialGet(rec, httptest.NewRequest("GET", "/api/dial", nil))
	if !strings.Contains(rec.Body.String(), `"available":false`) {
		t.Fatal("unsupported legacy dial route must remain hidden")
	}
	rec = httptest.NewRecorder()
	handleDialPost(rec, httptest.NewRequest("POST", "/api/dial", strings.NewReader(`{"mode":"public"}`)))
	if rec.Code == http.StatusOK {
		t.Fatal("legacy public-route mutation unexpectedly allowed")
	}
	after, _ := os.ReadFile(peersFile())
	if string(before) != string(after) {
		t.Fatal("legacy route refusal changed the managed registry")
	}
}
