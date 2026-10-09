package main

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func testManagedPeer(id int) peerRecord {
	p := peerRecord{ID: id, Name: fmt.Sprintf("peer-%d", id), Managed: true, Role: "iran", Engine: "frp", Carrier: "direct", LocalPub: "192.0.2.10", RemotePub: fmt.Sprintf("192.0.2.%d", id+20), LocalGre: fmt.Sprintf("10.70.%d.1", id), PeerGre: fmt.Sprintf("10.70.%d.2", id), FrpPort: 17000 + id, FRPTransport: "kcp", Token: fmt.Sprintf("synthetic-peer-%d-token-123456", id), ManagementSecret: strings.Repeat(fmt.Sprint(id), 64), RawPorts: []string{fmt.Sprint(8800 + id)}, Revision: 1}
	managedNames(&p)
	return p
}

func TestManagedControlRollbackAndRestartStatus(t *testing.T) {
	_, fail := testManagedSystem(t)
	p, err := managedPeerMutation("add", testManagedPeer(1))
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(peersFile())
	*fail = "systemctl disable"
	if _, err = managedPeerMutation("disable", p); err == nil {
		t.Fatal("failed disable accepted")
	}
	after, _ := os.ReadFile(peersFile())
	if !bytes.Equal(before, after) {
		t.Fatal("failed control changed registry")
	}
	p.State, p.LastVerified = "CONNECTED", time.Now().Unix()
	if err = writePeerRegistry([]peerRecord{p}); err != nil {
		t.Fatal(err)
	}
	result, err := managedPeerMutation("restart", p)
	if err != nil || result.State != "PENDING" || result.LastVerified != 0 {
		t.Fatal("restart reused stale connected status", err)
	}
}

func TestManagedInterruptedControlRecoversPreviousService(t *testing.T) {
	calls, _ := testManagedSystem(t)
	p, err := managedPeerMutation("add", testManagedPeer(1))
	if err != nil {
		t.Fatal(err)
	}
	desired := p
	desired.Disabled = true
	desired.State = "DISABLED"
	desired.OperationID = randomToken(32)
	snapshot := managedSnapshot{Peer: desired, Previous: &p, Control: true, Active: true, Files: map[string][]byte{}}
	if err = atomicPrivateFile(managedJournal(p), mustJSON(snapshot), 0600); err != nil {
		t.Fatal(err)
	}
	start := len(*calls)
	if err = recoverManagedTransactions(); err != nil {
		t.Fatal(err)
	}
	if current := findPeer(p.ID); current.Disabled || current.OperationID != p.OperationID {
		t.Fatal("recovery changed committed registry")
	}
	if _, err = os.Stat(managedJournal(p)); !os.IsNotExist(err) {
		t.Fatal("recovered journal remains")
	}
	found := false
	for _, call := range (*calls)[start:] {
		if strings.Contains(call, "systemctl restart "+p.GreIf) {
			found = true
		}
	}
	if !found {
		t.Fatal("previous active service was not restored")
	}
}

func TestManagedEncryptedBackupRestoreAndTamper(t *testing.T) {
	_, fail := testManagedSystem(t)
	p, err := managedPeerMutation("add", testManagedPeer(1))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(configDir, "backup.enc")
	if err = backupManagedPeers(path); err != nil {
		t.Fatal(err)
	}
	sealed, _ := os.ReadFile(path)
	if bytes.Contains(sealed, []byte(p.Token)) || bytes.Contains(sealed, []byte("PRIVATE KEY")) {
		t.Fatal("backup leaked plaintext")
	}
	if err = restoreManagedPeers(path, true); err != nil {
		t.Fatal(err)
	}
	if _, err = managedPeerMutation("remove", p); err != nil {
		t.Fatal(err)
	}
	if err = restoreManagedPeers(path, false); err != nil {
		t.Fatal(err)
	}
	if findPeer(1) == nil {
		t.Fatal("restore lost peer")
	}
	before, _ := os.ReadFile(peersFile())
	*fail = "systemctl restart " + p.FrpsSvc
	if err = restoreManagedPeers(path, false); err == nil {
		t.Fatal("failed restore accepted")
	}
	after, _ := os.ReadFile(peersFile())
	if !bytes.Equal(before, after) {
		t.Fatal("failed restore changed registry")
	}
	sealed[len(sealed)-1] ^= 1
	os.WriteFile(path, sealed, 0600)
	if err = restoreManagedPeers(path, false); err == nil {
		t.Fatal("tampered backup accepted")
	}
}

func TestManagedInputGuardsAndLogPath(t *testing.T) {
	for _, body := range []string{`{"id":1.5,"ports":[8888]}`, `{"id":1,"raw_ports":{}}`, `{"id":1,"ports":[8888.2]}`, `{"id":1,"ports":[true]}`} {
		var request peerPatchRequest
		if err := json.Unmarshal([]byte(body), &request); err == nil {
			t.Fatal("invalid input accepted", body)
		}
	}
	rr := httptest.NewRecorder()
	handleLogs(rr, httptest.NewRequest("GET", "/api/logs?svc=frps-../../etc/passwd", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatal("log path traversal accepted")
	}
	if _, err := runAction("restart-frpc", 99999); err == nil {
		t.Fatal("unknown ID targeted default service")
	}
}

func TestManagedPublicHTTPRedirect(t *testing.T) {
	called := false
	handler := securePanelHandler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	r := httptest.NewRequest("POST", "http://example.test:7777/private/api/login", strings.NewReader(`{"password":"synthetic"}`))
	r.RemoteAddr = "192.0.2.30:12345"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, r)
	if called || rr.Code != http.StatusSeeOther || !strings.HasPrefix(rr.Header().Get("Location"), "https://example.test:") {
		t.Fatal("public login reached HTTP handler")
	}
}

func TestManagedWSSFailsClosed(t *testing.T) {
	old := configDir
	configDir = t.TempDir()
	defer func() { configDir = old }()
	if defaultWSSConfig().InsecureTLS {
		t.Fatal("insecure WSS default")
	}
	if err := atomicPrivateFile(tlsCertFile(), []byte("invalid certificate"), 0600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	wss := &wssCarrierManager{cfg: wssConfig{UseTLS: true, ListenPort: port, LocalBridgeUDP: 19998}}
	done := make(chan struct{})
	go func() { wss.runServer(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("invalid TLS certificate opened plaintext server")
	}
	if wss.lastError == "" {
		t.Fatal("TLS error was ignored")
	}
	c, e := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
	if e == nil {
		c.Close()
		t.Fatal("server listened after TLS failure")
	}
}

func TestManagedPortNormalization(t *testing.T) {
	ms, err := normalizedPorts([]string{"tcp:8080=80,udp:8880-8882,8888"})
	if err != nil {
		t.Fatal(err)
	}
	want := []portMapping{{"tcp", 8080, 80}, {"udp", 8880, 8880}, {"udp", 8881, 8881}, {"udp", 8882, 8882}, {"tcp", 8888, 8888}, {"udp", 8888, 8888}}
	if !reflect.DeepEqual(ms, want) {
		t.Fatalf("got %#v", ms)
	}
	roundtrip, err := normalizedPorts(canonicalMappings(ms))
	if err != nil || !reflect.DeepEqual(ms, roundtrip) {
		t.Fatal("canonical mapping changed")
	}
	for _, bad := range []string{"0", "65536", "8080=0", "8-2", "80=90-91", "tcp:80,tcp:80", "sctp:80", "80,,90", "1-65535", "80=90=100", "80.5", "80;id"} {
		if _, err := normalizedPorts([]string{bad}); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if _, err := normalizedPorts([]string{"tcp:8080", "udp:8080"}); err != nil {
		t.Fatal("independent TCP/UDP listeners rejected")
	}
}

func TestManagedRegistryCompatibilityAndPartialErrors(t *testing.T) {
	old := configDir
	configDir = t.TempDir()
	defer func() { configDir = old }()
	data := []byte(`{"peers":[{"id":1,"raw_ports":"8080=80,8888"},{"id":2,"raw_ports":["udp:2052"]}]}`)
	if err := os.WriteFile(peersFile(), data, 0600); err != nil {
		t.Fatal(err)
	}
	peers, err := readPeerRegistry()
	if err != nil || len(peers) != 2 || len(peers[0].RawPorts) != 2 {
		t.Fatalf("legacy registry lost: %v", err)
	}
	bad := []byte(`{"peers":[{"id":1,"raw_ports":{}},{"id":2,"raw_ports":["2052"]}]}`)
	if err := os.WriteFile(peersFile(), bad, 0600); err != nil {
		t.Fatal(err)
	}
	peers, err = readPeerRegistry()
	if err == nil || len(peers) != 1 || peers[0].ID != 2 {
		t.Fatal("malformed record hid valid peers or was not reported")
	}
	if _, err := managedPeerMutation("add", testManagedPeer(3)); err == nil {
		t.Fatal("mutation silently discarded invalid record")
	}
	got, _ := os.ReadFile(peersFile())
	if !bytes.Equal(got, bad) {
		t.Fatal("corrupt registry was overwritten")
	}
}

func testManagedSystem(t *testing.T) (*[]string, *string) {
	t.Helper()
	oldDir, oldUnits, oldCheck, oldRun := configDir, managedUnitDir, managedCheck, managedRun
	configDir = t.TempDir()
	managedUnitDir = filepath.Join(configDir, "units")
	managedCheck = func(peerRecord, *peerRecord) error { return nil }
	calls := []string{}
	fail := ""
	rules := map[string]bool{}
	managedRun = func(name string, args ...string) ([]byte, error) {
		call := filepath.Base(name) + " " + strings.Join(args, " ")
		calls = append(calls, call)
		if fail != "" && strings.Contains(call, fail) {
			fail = ""
			return nil, fmt.Errorf("injected activation failure")
		}
		if name == "iptables" {
			key := strings.Join(args[4:], " ")
			switch args[2] {
			case "-C":
				if !rules[key] {
					return nil, fmt.Errorf("absent")
				}
			case "-I":
				rules[key] = true
			case "-D":
				delete(rules, key)
			}
		}
		return nil, nil
	}
	t.Cleanup(func() { configDir, managedUnitDir, managedCheck, managedRun = oldDir, oldUnits, oldCheck, oldRun })
	return &calls, &fail
}

func TestManagedRollbackAndPeerIsolation(t *testing.T) {
	calls, fail := testManagedSystem(t)
	p1, err := managedPeerMutation("add", testManagedPeer(1))
	if err != nil {
		t.Fatal(err)
	}
	p2, err := managedPeerMutation("add", testManagedPeer(2))
	if err != nil {
		t.Fatal(err)
	}
	firstBefore, _ := os.ReadFile(managedConfigPath(p1))
	secondBefore, _ := os.ReadFile(managedConfigPath(p2))
	registryBefore, _ := os.ReadFile(peersFile())
	p1.FRPTransport = "tcp"
	*fail = "systemctl restart " + p1.FrpsSvc
	if _, err = managedPeerMutation("update", p1); err == nil {
		t.Fatal("activation failure was reported as success")
	}
	firstAfter, _ := os.ReadFile(managedConfigPath(p1))
	secondAfter, _ := os.ReadFile(managedConfigPath(p2))
	registryAfter, _ := os.ReadFile(peersFile())
	if !bytes.Equal(firstBefore, firstAfter) || !bytes.Equal(secondBefore, secondAfter) || !bytes.Equal(registryBefore, registryAfter) {
		t.Fatal("failed update changed working files or registry")
	}
	start := len(*calls)
	if _, err = managedPeerMutation("remove", peerRecord{ID: 1}); err != nil {
		t.Fatal(err)
	}
	secondAfter, _ = os.ReadFile(managedConfigPath(p2))
	if !bytes.Equal(secondBefore, secondAfter) {
		t.Fatal("removing first peer changed second")
	}
	for _, call := range (*calls)[start:] {
		if strings.Contains(call, p2.FrpsSvc) || strings.Contains(call, p2.GreIf) {
			t.Fatalf("second peer was targeted: %s", call)
		}
	}
	if _, err = os.Stat(managedDir(1)); !os.IsNotExist(err) {
		t.Fatal("owned directory remains after deletion")
	}
}

func TestManagedFailedCreateCanRetry(t *testing.T) {
	_, fail := testManagedSystem(t)
	*fail = "iptables -w 5 -I"
	if _, err := managedPeerMutation("add", testManagedPeer(1)); err == nil {
		t.Fatal("firewall failure accepted")
	}
	if _, err := os.Stat(managedDir(1)); !os.IsNotExist(err) {
		t.Fatal("failed create left an orphan directory")
	}
	if len(loadPeers()) != 0 {
		t.Fatal("failed peer committed")
	}
	if _, err := managedPeerMutation("add", testManagedPeer(1)); err != nil {
		t.Fatalf("retry failed: %v", err)
	}
}

func TestManagedRemoveRollback(t *testing.T) {
	_, fail := testManagedSystem(t)
	p, err := managedPeerMutation("add", testManagedPeer(1))
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(managedConfigPath(p))
	*fail = "systemctl daemon-reload"
	if _, err = managedPeerMutation("remove", peerRecord{ID: 1}); err == nil {
		t.Fatal("failed remove reported success")
	}
	after, _ := os.ReadFile(managedConfigPath(p))
	if !bytes.Equal(before, after) || findPeer(1) == nil {
		t.Fatal("failed removal did not restore peer")
	}
}

func TestManagedCollisionValidation(t *testing.T) {
	p1, p2 := testManagedPeer(1), testManagedPeer(2)
	p1.MasterURL = "https://10.70.1.1:9443/api/managed-peer"
	rules := ownedFirewallRules(p1)
	foundPort := false
	for _, rule := range rules {
		if strings.Contains(strings.Join(rule, " "), "--dport 9443") {
			foundPort = true
		}
	}
	if !foundPort {
		t.Fatal("boot firewall ignored recorded management port")
	}
	if err := validateManagedPeer(p2, []peerRecord{p1}); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*peerRecord){func(p *peerRecord) { p.FrpPort = p1.FrpPort }, func(p *peerRecord) { p.LocalGre = p1.LocalGre; p.PeerGre = p1.PeerGre }, func(p *peerRecord) { p.RawPorts = p1.RawPorts }, func(p *peerRecord) { p.Token = p1.Token }, func(p *peerRecord) { p.RemotePub = p1.RemotePub }, func(p *peerRecord) { p.LocalGre = "10.70.2.0" }} {
		p := p2
		change(&p)
		if err := validateManagedPeer(p, []peerRecord{p1}); err == nil {
			t.Fatal("collision accepted")
		}
	}
	listener, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err = listenerAvailable("udp", "127.0.0.1", listener.LocalAddr().(*net.UDPAddr).Port); err == nil {
		t.Fatal("unrelated UDP listener not detected")
	}
	if _, err = listener.WriteTo([]byte("still alive"), listener.LocalAddr()); err != nil {
		t.Fatal("listener was disturbed")
	}
}

func TestManagedBundleTrustAndTransport(t *testing.T) {
	p := testManagedPeer(2)
	p.RawPorts = []string{"tcp:8880-8881=8080-8081", "udp:2052"}
	p.MasterURL = "https://10.70.2.1:7443/api/managed-peer"
	p.MasterPin = strings.Repeat("a", 64)
	p.ServerCA = "synthetic public CA fixture"
	bundle, err := makeManagedBundle(p)
	if err != nil {
		t.Fatal(err)
	}
	got, err := parseManagedBundle(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if got.FRPTransport != "kcp" || got.ID != 2 || got.Revision != 1 || !reflect.DeepEqual(got.RawPorts, p.RawPorts) {
		t.Fatal("bundle changed transport/mappings/revision")
	}
	p.MasterURL = "http://10.70.2.1:7443/api/managed-peer"
	bad, _ := makeManagedBundle(p)
	if _, err = parseManagedBundle(bad); err == nil {
		t.Fatal("plaintext management bundle accepted")
	}
	if _, err = parseManagedBundle("hsh2_bad"); err == nil {
		t.Fatal("bad bundle accepted")
	}
}

func TestManagedRequestTLSAndReplay(t *testing.T) {
	p := testManagedPeer(1)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := []byte{}
		if err := managedAuthenticate(r, body, p); err != nil {
			http.Error(w, "unauthorized", 401)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	}))
	defer server.Close()
	sum := sha256.Sum256(server.Certificate().Raw)
	p.MasterURL = server.URL
	p.MasterPin = hex.EncodeToString(sum[:])
	if _, err := managedRequest(p, "GET", nil); err != nil {
		t.Fatal(err)
	}
	p.MasterPin = strings.Repeat("0", 64)
	if _, err := managedRequest(p, "GET", nil); err == nil {
		t.Fatal("wrong TLS pin accepted")
	}
	r := httptest.NewRequest("GET", "https://example.invalid/api/managed-peer?id=1", nil)
	r.TLS = &tls.ConnectionState{}
	stamp, nonce := fmt.Sprint(time.Now().Unix()), strings.Repeat("n", 32)
	r.Header.Set("X-Hashem-Time", stamp)
	r.Header.Set("X-Hashem-Nonce", nonce)
	r.Header.Set("X-Hashem-Signature", managedSignature(p.ManagementSecret, "GET", r.URL.RequestURI(), stamp, nonce, nil))
	if err := managedAuthenticate(r, nil, p); err != nil {
		t.Fatal(err)
	}
	if err := managedAuthenticate(r, nil, p); err == nil {
		t.Fatal("replayed management request accepted")
	}
}

func TestManagedNativeFRPValidation(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("native Linux FRP validation")
	}
	if _, err := os.Stat(filepath.Join(managedBinaryDir, "frps")); err != nil {
		t.Skip("pinned FRP binaries not installed in this test environment")
	}
	old := configDir
	configDir = t.TempDir()
	defer func() { configDir = old }()
	for id := 1; id <= 3; id++ {
		for _, transport := range []string{"tcp", "kcp"} {
			p := testManagedPeer(id)
			p.FRPTransport = transport
			cert, key, err := makePeerCertificate(p.LocalGre)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.MkdirAll(managedDir(id), 0700); err != nil {
				t.Fatal(err)
			}
			for name, data := range map[string][]byte{"server.crt": cert, "server.key": key, "ca.crt": cert} {
				if err = atomicPrivateFile(filepath.Join(managedDir(id), name), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			for _, role := range []string{"iran", "foreign"} {
				if role == "foreign" {
					p = workerView(p)
				}
				data, err := managedFRPConfig(p)
				if err != nil {
					t.Fatal(err)
				}
				path := managedConfigPath(p)
				if err = atomicPrivateFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
				if out, err := exec.Command(managedFRPBinary(p), "verify", "-c", path).CombinedOutput(); err != nil {
					t.Fatalf("native FRP rejected %s/%s/id%d: %v %s", role, transport, id, err, out)
				}
			}
		}
	}
}

func TestManagedRegistryConcurrentMutation(t *testing.T) {
	if os.Getenv("HASHEM_REGISTRY_HELPER") == "1" {
		configDir = os.Getenv("HASHEM_REGISTRY_TEST_DIR")
		for i := 0; i < 50; i++ {
			unlock, err := lockPeers()
			if err != nil {
				t.Fatal(err)
			}
			ps, err := readPeerRegistry()
			if err != nil {
				unlock()
				t.Fatal(err)
			}
			ps[0].Revision++
			err = writePeerRegistry(ps)
			unlock()
			if err != nil {
				t.Fatal(err)
			}
		}
		return
	}
	old := configDir
	configDir = t.TempDir()
	defer func() { configDir = old }()
	if err := writePeerRegistry([]peerRecord{testManagedPeer(1)}); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestManagedRegistryConcurrentMutation$")
	cmd.Env = append(os.Environ(), "HASHEM_REGISTRY_HELPER=1", "HASHEM_REGISTRY_TEST_DIR="+configDir)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			unlock, err := lockPeers()
			if err != nil {
				t.Error(err)
				return
			}
			ps, err := readPeerRegistry()
			if err != nil {
				unlock()
				t.Error(err)
				return
			}
			ps[0].Revision++
			err = writePeerRegistry(ps)
			unlock()
			if err != nil {
				t.Error(err)
				return
			}
		}
	}()
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	ps, err := readPeerRegistry()
	if err != nil || ps[0].Revision != 101 {
		t.Fatalf("lost concurrent updates: %+v %v", ps, err)
	}
}

func TestManagedUDPRequiresResponse(t *testing.T) {
	s, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, _, _, loss, _, err := probeUDP(s.LocalAddr().String(), 1, 30*time.Millisecond); err == nil || loss != 100 {
		t.Fatal("send-only UDP counted as connectivity")
	}
	go func() {
		buf := make([]byte, 256)
		for {
			n, addr, err := s.ReadFrom(buf)
			if err != nil {
				return
			}
			_, _ = s.WriteTo(buf[:n], addr)
		}
	}()
	if _, _, _, loss, _, err := probeUDP(s.LocalAddr().String(), 2, time.Second); err != nil || loss != 0 {
		t.Fatalf("actual echo response rejected: %v", err)
	}
}

func TestManagedPasswordMigration(t *testing.T) {
	oldDir, oldCfg := configDir, cfg
	configDir = t.TempDir()
	defer func() { configDir, cfg = oldDir, oldCfg }()
	password := "Synthetic-login-Password-2026!"
	sum := sha256.Sum256([]byte(password))
	cfg = panelConfig{Username: "admin", PassHash: hex.EncodeToString(sum[:]), BasePath: "test"}
	r := httptest.NewRequest("POST", "/test/api/login", strings.NewReader(`{"username":"admin","password":"`+password+`"}`))
	w := httptest.NewRecorder()
	handleLogin(w, r)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	valid, legacy := verifyPassword(cfg.PassHash, password)
	if !valid || legacy {
		t.Fatal("legacy hash was not migrated")
	}
	if valid, _ := verifyPassword(cfg.PassHash, "incorrect-password"); valid {
		t.Fatal("wrong password accepted")
	}
	data, _ := os.ReadFile(cfgPath())
	if bytes.Contains(data, []byte(password)) {
		t.Fatal("plaintext password persisted")
	}
	if valid, _ := verifyPassword("$argon2id$v=19$m=99999999,t=99,p=99$bad$bad", password); valid {
		t.Fatal("unsafe PHC accepted")
	}
}

func TestManagedConcurrentLegacyLoginDoesNotOverwriteMigration(t *testing.T) {
	oldDir, oldCfg := configDir, cfg
	configDir = t.TempDir()
	defer func() { configDir, cfg = oldDir, oldCfg }()
	password := "Synthetic-concurrent-password-2026!"
	digest := sha256.Sum256([]byte(password))
	cfg = panelConfig{Username: "admin", PassHash: hex.EncodeToString(digest[:]), BasePath: "test"}
	if err := atomicPrivateFile(cfgPath(), mustJSON(cfg), 0600); err != nil {
		t.Fatal(err)
	}
	responses := make(chan *httptest.ResponseRecorder, 2)
	var group sync.WaitGroup
	for i := 0; i < 2; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			r := httptest.NewRequest("POST", "/test/api/login", strings.NewReader(`{"username":"admin","password":"`+password+`"}`))
			rr := httptest.NewRecorder()
			handleLogin(rr, r)
			responses <- rr
		}()
	}
	group.Wait()
	close(responses)
	success := 0
	for rr := range responses {
		if rr.Code == http.StatusOK {
			success++
			for _, cookie := range rr.Result().Cookies() {
				if cookie.Name == "gre_session" {
					r := httptest.NewRequest("GET", "/test/api/status", nil)
					r.AddCookie(cookie)
					if !authed(r) {
						t.Fatal("migration issued stale session")
					}
				}
			}
		}
	}
	if success == 0 {
		t.Fatal("both valid concurrent logins failed")
	}
	valid, legacy := verifyPassword(cfg.PassHash, password)
	if !valid || legacy {
		t.Fatal("migration lost under concurrent login")
	}
}

func TestManagedWatchdogIsIndependentAndBounded(t *testing.T) {
	calls, fail := testManagedSystem(t)
	for id := 1; id <= 2; id++ {
		if _, err := managedPeerMutation("add", testManagedPeer(id)); err != nil {
			t.Fatal(err)
		}
	}
	if err := saveWatchdogConfig(watchdogConfig{Enabled: true, AutoRestart: true, FailThreshold: 1}); err != nil {
		t.Fatal(err)
	}
	start := len(*calls)
	*fail = "systemctl is-active --quiet hsh-frps-1"
	states, err := managedHealthCheck()
	if err != nil {
		t.Fatal(err)
	}
	if states[1].Restarts != 1 || states[2].Restarts != 0 {
		t.Fatal("watchdog restart was not isolated")
	}
	for _, call := range (*calls)[start:] {
		if strings.Contains(call, "restart") && strings.Contains(call, "hsh-frps-2") {
			t.Fatal("unhealthy first peer restarted second")
		}
	}
	if managedBackoff(0) != 15*time.Second || managedBackoff(4) != 240*time.Second || managedBackoff(100) != 960*time.Second {
		t.Fatal("unbounded or incorrect retry backoff")
	}
}
