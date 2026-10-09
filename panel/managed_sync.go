package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const managedBundlePrefix = "hsh2_"

func makeManagedBundle(p peerRecord) (string, error) {
	if !p.Managed || p.Role != "iran" || p.MasterPin == "" || p.ServerCA == "" {
		return "", fmt.Errorf("peer is not ready for pairing")
	}
	p.LastError = ""
	p.LastVerified = 0
	p.AppliedRevision = 0
	p.State = "PENDING"
	data, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	return managedBundlePrefix + base64.RawURLEncoding.EncodeToString(data), nil
}

func parseManagedBundle(s string) (peerRecord, error) {
	var p peerRecord
	if !strings.HasPrefix(s, managedBundlePrefix) || len(s) > 16384 {
		return p, fmt.Errorf("invalid managed bundle")
	}
	data, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(s, managedBundlePrefix))
	if err != nil {
		return p, fmt.Errorf("invalid managed bundle encoding")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&p); err != nil {
		return p, fmt.Errorf("invalid managed bundle schema")
	}
	if err = validateManagedPeer(p, nil); err != nil {
		return p, err
	}
	u, err := url.Parse(p.MasterURL)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Hostname() != p.LocalGre || u.Path != "/api/managed-peer" || len(p.MasterPin) != 64 || len(p.ManagementSecret) < 32 || !p.Managed || p.Revision == 0 || p.Role != "iran" {
		return p, fmt.Errorf("bundle has invalid management trust or revision")
	}
	if _, err = hex.DecodeString(p.MasterPin); err != nil {
		return p, fmt.Errorf("invalid certificate pin")
	}
	return p, nil
}

func workerView(master peerRecord) peerRecord {
	p := master
	p.Role = "foreign"
	p.LocalPub, p.RemotePub = master.RemotePub, master.LocalPub
	p.LocalGre, p.PeerGre = master.PeerGre, master.LocalGre
	managedNames(&p)
	return p
}

func managedSignature(secret, method, uri, stamp, nonce string, body []byte) string {
	digest := sha256.Sum256(body)
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%s\n%s\n%s\n%s\n%x", method, uri, stamp, nonce, digest)
	return hex.EncodeToString(mac.Sum(nil))
}

var managedReplay = struct {
	sync.Mutex
	seen map[string]int64
}{seen: map[string]int64{}}

func managedAuthenticate(r *http.Request, body []byte, p peerRecord) error {
	if r.TLS == nil {
		return fmt.Errorf("HTTPS required")
	}
	stamp, nonce := r.Header.Get("X-Hashem-Time"), r.Header.Get("X-Hashem-Nonce")
	ts, err := strconv.ParseInt(stamp, 10, 64)
	now := time.Now().Unix()
	if err != nil || ts < now-60 || ts > now+60 || len(nonce) != 32 {
		return fmt.Errorf("expired or invalid request")
	}
	want := managedSignature(p.ManagementSecret, r.Method, r.URL.RequestURI(), stamp, nonce, body)
	if !hmac.Equal([]byte(want), []byte(r.Header.Get("X-Hashem-Signature"))) {
		return fmt.Errorf("invalid request signature")
	}
	key := fmt.Sprintf("%d:%s", p.ID, nonce)
	managedReplay.Lock()
	defer managedReplay.Unlock()
	for key, expiry := range managedReplay.seen {
		if expiry < now {
			delete(managedReplay.seen, key)
		}
	}
	if _, found := managedReplay.seen[key]; found {
		return fmt.Errorf("replayed request")
	}
	if len(managedReplay.seen) > 10000 {
		return fmt.Errorf("request replay cache full")
	}
	managedReplay.seen[key] = now + 120
	return nil
}

func managedRequest(p peerRecord, method string, payload any) ([]byte, error) {
	data, err := managedRequestOnce(p, method, payload)
	if err == nil || p.Role != "foreign" {
		return data, err
	}
	u, parseErr := url.Parse(p.MasterURL)
	if parseErr != nil || u.Hostname() != p.PeerGre {
		return data, err
	}
	// A broken GRE path must not prevent authenticated repair. The same explicit
	// certificate pin and request signature apply to the public bootstrap path.
	u.Host = net.JoinHostPort(p.RemotePub, u.Port())
	p.MasterURL = u.String()
	return managedRequestOnce(p, method, payload)
}

func managedRequestOnce(p peerRecord, method string, payload any) ([]byte, error) {
	u, err := url.Parse(p.MasterURL)
	if err != nil || u.Scheme != "https" || u.User != nil {
		return nil, fmt.Errorf("trusted HTTPS management endpoint required")
	}
	q := u.Query()
	q.Set("id", strconv.Itoa(p.ID))
	u.RawQuery = q.Encode()
	var body []byte
	if payload != nil {
		body, err = json.Marshal(payload)
		if err != nil {
			return nil, err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	stamp, nonce := strconv.FormatInt(time.Now().Unix(), 10), randomToken(32)
	req.Header.Set("X-Hashem-Time", stamp)
	req.Header.Set("X-Hashem-Nonce", nonce)
	req.Header.Set("X-Hashem-Signature", managedSignature(p.ManagementSecret, method, u.RequestURI(), stamp, nonce, body))
	req.Header.Set("Content-Type", "application/json")
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: true, VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
		if len(raw) == 0 {
			return fmt.Errorf("missing TLS certificate")
		}
		sum := sha256.Sum256(raw[0])
		cert, err := x509.ParseCertificate(raw[0])
		if err != nil || time.Now().Before(cert.NotBefore) || time.Now().After(cert.NotAfter) {
			return fmt.Errorf("management TLS certificate expired or invalid")
		}
		got := hex.EncodeToString(sum[:])
		if len(p.MasterPin) != 64 || subtle.ConstantTimeCompare([]byte(got), []byte(strings.ToLower(p.MasterPin))) != 1 {
			return fmt.Errorf("management TLS pin mismatch")
		}
		return nil
	}}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("management redirects forbidden") }}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("management request failed: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 65537))
	if err != nil {
		return nil, err
	}
	if len(data) > 65536 {
		return nil, fmt.Errorf("management response too large")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("management endpoint returned HTTP %d", resp.StatusCode)
	}
	return data, nil
}

type managedAck struct {
	Revision   uint64 `json:"revision"`
	Transport  string `json:"transport"`
	Forwarding bool   `json:"forwarding"`
	Registered bool   `json:"registered"`
	Error      string `json:"error,omitempty"`
}

func handleManagedPeerSync(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if err != nil {
		http.Error(w, "invalid peer", 400)
		return
	}
	p := findPeer(id)
	if p == nil || !p.Managed || p.Role != "iran" {
		http.Error(w, "unknown peer", 404)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 65537))
	if err != nil || len(body) > 65536 {
		http.Error(w, "invalid body", 400)
		return
	}
	if err = managedAuthenticate(r, body, *p); err != nil {
		http.Error(w, "authentication failed", 401)
		return
	}
	authenticatedSecret := p.ManagementSecret
	if r.Method == "GET" {
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, p)
		return
	}
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	var ack managedAck
	if err = json.Unmarshal(body, &ack); err != nil {
		http.Error(w, "invalid acknowledgement", 400)
		return
	}
	unlock, err := lockPeers()
	if err != nil {
		http.Error(w, "busy", 503)
		return
	}
	defer unlock()
	peers, err := readPeerRegistry()
	if err != nil {
		http.Error(w, "invalid registry", 500)
		return
	}
	for i := range peers {
		p := &peers[i]
		if p.ID != id {
			continue
		}
		if p.ManagementSecret != authenticatedSecret || ack.Revision != p.Revision || ack.Transport != p.FRPTransport {
			http.Error(w, "stale revision", 409)
			return
		}
		if p.Disabled {
			p.State, p.LastVerified, p.AppliedRevision = "DISABLED", 0, 0
			if err = writePeerRegistry(peers); err != nil {
				http.Error(w, "commit failed", 500)
				return
			}
			writeJSON(w, map[string]any{"revision": p.Revision, "state": p.State})
			return
		}
		if ack.Error != "" {
			p.State = "DEGRADED"
			p.LastError = "worker could not activate revision"
		} else {
			p.AppliedRevision = ack.Revision
			p.LastError = ""
			p.State = "STARTING"
			if ack.Registered {
				p.State = "CONNECTED"
				p.LastVerified = time.Now().Unix()
			}
			if ack.Forwarding && verifyManagedForwarding(*p) == nil {
				p.State = "CONNECTED"
				p.LastVerified = time.Now().Unix()
			}
		}
		if err = writePeerRegistry(peers); err != nil {
			http.Error(w, "commit failed", 500)
			return
		}
		writeJSON(w, map[string]any{"revision": ack.Revision, "state": p.State})
		return
	}
	http.Error(w, "peer removed", 404)
}

// Verification uses a temporary echo target on the foreign node's configured
// local ports only when those ports are not occupied. Production services are
// never replaced by a test listener: their application response is unknown.
// Native FRPC's local status API provides proxy registration evidence below;
// forwarding requires an actual round trip, not a successful dial/send.
func verifyManagedForwarding(p peerRecord) error {
	ms, err := peerMappings(p)
	if err != nil {
		return err
	}
	for _, m := range ms {
		if m.Protocol == "udp" {
			return fmt.Errorf("UDP application response has not been verified")
		}
		c, err := net.DialTimeout("tcp", net.JoinHostPort(p.LocalPub, strconv.Itoa(m.Public)), time.Second)
		if err != nil {
			return fmt.Errorf("TCP forwarding port %d unavailable", m.Public)
		}
		_ = c.Close()
	}
	// Opening a socket is insufficient evidence of application data delivery.
	return fmt.Errorf("application response verification is not configured")
}

func reconcileManagedPeer(p peerRecord) error {
	data, err := managedRequest(p, "GET", nil)
	if err != nil {
		return err
	}
	var desired peerRecord
	if err = json.Unmarshal(data, &desired); err != nil {
		return err
	}
	if desired.ID != p.ID || desired.Revision < p.Revision || desired.ManagementSecret != p.ManagementSecret || desired.MasterPin != p.MasterPin {
		return fmt.Errorf("invalid/stale desired state")
	}
	worker := workerView(desired)
	if desired.Revision > p.Revision {
		unlock, err := lockPeers()
		if err != nil {
			return err
		}
		peers, e := readPeerRegistry()
		if e == nil {
			var current *peerRecord
			for _, record := range peers {
				if record.ID == p.ID {
					copy := record
					current = &copy
				}
			}
			if current == nil || current.Revision > worker.Revision || current.ManagementSecret != p.ManagementSecret {
				e = fmt.Errorf("peer removed or newer state already applied")
			} else if current.Revision < worker.Revision {
				e = managedApply(worker, current, peers)
			}
		}
		unlock()
		if e != nil {
			_, _ = managedRequest(p, "POST", managedAck{Revision: desired.Revision, Transport: desired.FRPTransport, Error: "activation failed"})
			return e
		}
	}
	registered := managedProxyRegistration(worker) == nil
	_, err = managedRequest(worker, "POST", managedAck{Revision: desired.Revision, Transport: desired.FRPTransport, Registered: registered})
	if err == nil && registered {
		unlock, e := lockPeers()
		if e != nil {
			return e
		}
		defer unlock()
		peers, e := readPeerRegistry()
		if e != nil {
			return e
		}
		for i := range peers {
			if peers[i].ID == worker.ID && peers[i].Revision == worker.Revision {
				peers[i].State = "CONNECTED"
				peers[i].AppliedRevision = worker.Revision
				peers[i].LastVerified = time.Now().Unix()
				peers[i].LastError = ""
			}
		}
		return writePeerRegistry(peers)
	}
	return err
}

func managedProxyRegistration(p peerRecord) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("http://127.0.0.1:%d/api/status", 27100+p.ID), nil)
	if err != nil {
		return err
	}
	r.SetBasicAuth("hashem", p.ManagementSecret)
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("local status redirect forbidden") }}
	resp, err := client.Do(r)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	client.CloseIdleConnections()
	if resp.StatusCode != 200 {
		return fmt.Errorf("FRPC status unavailable")
	}
	var statuses map[string][]struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 65536)).Decode(&statuses); err != nil {
		return err
	}
	ms, err := peerMappings(p)
	if err != nil {
		return err
	}
	for _, m := range ms {
		name := fmt.Sprintf("peer%d_%s_%d", p.ID, m.Protocol, m.Public)
		found := false
		for _, s := range statuses[m.Protocol] {
			if s.Name == name && s.Status == "running" {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("proxy %s has not registered", name)
		}
	}
	return nil
}

func startManagedReconciler() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		for _, p := range loadPeers() {
			if p.Managed && p.Role == "foreign" && !p.Disabled {
				_ = reconcileManagedPeer(p)
			}
		}
	}
}
