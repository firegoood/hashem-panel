package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRewriteTomlPorts(t *testing.T) {
	initial := `bindAddr = "0.0.0.0"
bindPort = 7000
auth.token = "secret123"

[[proxies]]
name = "tcp_80"
type = "tcp"
localIP = "127.0.0.1"
localPort = 80
remotePort = 80

[[proxies]]
name = "udp_80"
type = "udp"
localIP = "127.0.0.1"
localPort = 80
remotePort = 80
`

	updated := rewriteTomlPorts(initial, []int{443, 2083})

	if !strings.Contains(updated, "bindPort = 7000") {
		t.Errorf("expected header preserved, got:\n%s", updated)
	}
	if !strings.Contains(updated, "auth.token = \"secret123\"") {
		t.Errorf("expected auth preserved, got:\n%s", updated)
	}
	if strings.Contains(updated, "tcp_80") || strings.Contains(updated, "remotePort = 80") {
		t.Errorf("expected old port 80 removed, got:\n%s", updated)
	}
	if !strings.Contains(updated, "name = \"tcp_443\"") || !strings.Contains(updated, "remotePort = 443") {
		t.Errorf("expected port 443 present, got:\n%s", updated)
	}
	if !strings.Contains(updated, "name = \"udp_443\"") {
		t.Errorf("expected udp_443 present, got:\n%s", updated)
	}
	if !strings.Contains(updated, "name = \"tcp_2083\"") || !strings.Contains(updated, "remotePort = 2083") {
		t.Errorf("expected port 2083 present, got:\n%s", updated)
	}
	if !strings.Contains(updated, "name = \"udp_2083\"") {
		t.Errorf("expected udp_2083 present, got:\n%s", updated)
	}
}

func TestHandlePeersPatchValidation(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantCode   string
	}{
		{
			name:       "invalid json",
			body:       `{bad`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "E-PEER-07",
		},
		{
			name:       "negative id",
			body:       `{"id": -1, "ports": [443]}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "E-PEER-07",
		},
		{
			name:       "empty ports",
			body:       `{"id": 1, "ports": []}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "E-PEER-07",
		},
		{
			name:       "invalid port number low",
			body:       `{"id": 1, "ports": [0]}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "E-PEER-07",
		},
		{
			name:       "invalid port number high",
			body:       `{"id": 1, "ports": [70000]}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "E-PEER-07",
		},
		{
			name:       "peer not found",
			body:       `{"id": 9999, "ports": [443]}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "E-PEER-05",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("PATCH", "/api/peers", bytes.NewBufferString(tc.body))
			w := httptest.NewRecorder()
			handlePeersPatch(w, req)

			if w.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d (body: %s)", w.Code, tc.wantStatus, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), tc.wantCode) {
				t.Errorf("body %s does not contain code %s", w.Body.String(), tc.wantCode)
			}
		})
	}
}
