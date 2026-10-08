package main

// Connectivity Rescue: automatic workaround for one-way blocked paths.
//
// Problem it solves: server X can ping server Y but every TCP/UDP flow X->Y
// is dropped upstream, while Y->X works. The normal tunnel needs X to dial Y,
// so it never comes up. The rescue reverses the dial direction:
//
//	origin (X, the blocked side)  runs frps (control, firewalled to Y only)
//	                              + frpc exposing the chosen ports as secret
//	                              (stcp/sudp) proxies pointing at 127.0.0.1
//	entry  (Y, the reachable side) runs frpc *visitors* that listen on the
//	                              public ports and tunnel to X over Y->X.
//
// Detection is passive-cheap (a few TCP dials + one ping per minute) and only
// ever raises a banner; nothing is changed until the user clicks. Everything
// created is namespaced hashem-rescue-* so Disable removes it completely.

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	rescueCodePrefix = "hrc1_"
	rescueMaxPorts   = 64
	rescueStrikes    = 3 // consecutive blocked probes before the banner appears
)

var (
	rescueUnitDir = "/etc/systemd/system"
	rescueBinDir  = "/usr/local/bin"
	rescueMu      sync.Mutex

	// Swappable for tests.
	runCmd = func(name string, args ...string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
		return string(out), err
	}
	rescueDial = func(addr string, d time.Duration) (net.Conn, error) {
		return net.DialTimeout("tcp", addr, d)
	}
)

func init() {
	if v := os.Getenv("GRE_RESCUE_UNIT_DIR"); v != "" {
		rescueUnitDir = v
	}
	if v := os.Getenv("GRE_RESCUE_BIN_DIR"); v != "" {
		rescueBinDir = v
	}
}

type rescueState struct {
	Role           string `json:"role"` // "" | "origin" | "entry"
	RemoteIP       string `json:"remote_ip,omitempty"`
	CtrlPort       int    `json:"ctrl_port,omitempty"`
	Token          string `json:"token,omitempty"`
	Secret         string `json:"secret,omitempty"`
	Ports          []int  `json:"ports,omitempty"`
	Skipped        []int  `json:"skipped,omitempty"`
	Since          int64  `json:"since,omitempty"`
	Consumed       bool   `json:"consumed,omitempty"`
	Strikes        int    `json:"strikes,omitempty"`
	SuspectedSince int64  `json:"suspected_since,omitempty"`
	LiftedSince    int64  `json:"lifted_since,omitempty"`
	LastVerdict    string `json:"last_verdict,omitempty"`
	LastProbe      int64  `json:"last_probe,omitempty"`
}

func rescueStatePath() string { return filepath.Join(configDir, "rescue.json") }
func rescueDir() string       { return filepath.Join(configDir, "rescue") }

func loadRescueLocked() rescueState {
	var st rescueState
	if b, err := os.ReadFile(rescueStatePath()); err == nil {
		_ = json.Unmarshal(b, &st)
	}
	return st
}

func saveRescueLocked(st rescueState) error {
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return err
	}
	tmp := rescueStatePath() + ".tmp"
	if err := os.WriteFile(tmp, mustJSON(st), 0600); err != nil {
		return err
	}
	return os.Rename(tmp, rescueStatePath())
}

// ---------------------------------------------------------------- validation

// parsePortList accepts "1020, 1030 1040" or ranges "2000-2005".
func parsePortList(s string) ([]int, error) {
	seen := map[int]bool{}
	var out []int
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == ';' || r == '\n' || r == '\t' }) {
		lo, hi := 0, 0
		if i := strings.Index(f, "-"); i > 0 {
			a, e1 := strconv.Atoi(f[:i])
			b, e2 := strconv.Atoi(f[i+1:])
			if e1 != nil || e2 != nil || a > b || b-a >= rescueMaxPorts {
				return nil, fmt.Errorf("bad port range %q", f)
			}
			lo, hi = a, b
		} else {
			a, err := strconv.Atoi(f)
			if err != nil {
				return nil, fmt.Errorf("bad port %q", f)
			}
			lo, hi = a, a
		}
		for p := lo; p <= hi; p++ {
			if p < 1 || p > 65535 {
				return nil, fmt.Errorf("port %d out of range", p)
			}
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
		if len(out) > rescueMaxPorts {
			return nil, fmt.Errorf("too many ports (max %d)", rescueMaxPorts)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("no ports given")
	}
	sort.Ints(out)
	return out, nil
}

// validRemoteIP: a routable IPv4 only. The firewall rule and bind address are
// IPv4; loopback/unspecified/multicast/link-local would be nonsense or unsafe.
func validRemoteIP(s string) (string, error) {
	ip := net.ParseIP(strings.TrimSpace(s))
	if ip == nil || ip.To4() == nil {
		return "", errors.New("remote address must be an IPv4 address")
	}
	if ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() {
		return "", errors.New("remote address is not a routable host")
	}
	return ip.To4().String(), nil
}

// reservedPorts: never forwarded or shadowed on this host.
func rescueReserved(extra ...int) map[int]bool {
	r := map[int]bool{22: true}
	for _, p := range []int{cfg.Port, effectiveTLSPort()} {
		if p > 0 {
			r[p] = true
		}
	}
	for _, p := range extra {
		r[p] = true
	}
	return r
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("rescue: entropy unavailable: " + err.Error())
	}
	return hex.EncodeToString(b)
}

func pickCtrlPort(avoid map[int]bool) int {
	for i := 0; i < 200; i++ {
		b := make([]byte, 2)
		_, _ = rand.Read(b)
		p := 20000 + (int(b[0])<<8|int(b[1]))%40000
		if !avoid[p] && portFree(p) {
			return p
		}
	}
	return 0
}

// ------------------------------------------------------------ file rendering

func rescueFrpsToml(cport int, token string) string {
	return fmt.Sprintf("bindAddr = \"0.0.0.0\"\nbindPort = %d\nauth.method = \"token\"\nauth.token = %q\nlog.to = \"console\"\nlog.level = \"info\"\n", cport, token)
}

func rescueOriginFrpcToml(cport int, token, secret string, ports []int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "serverAddr = \"127.0.0.1\"\nserverPort = %d\nauth.method = \"token\"\nauth.token = %q\nloginFailExit = false\nlog.to = \"console\"\nlog.level = \"info\"\n", cport, token)
	for _, p := range ports {
		for _, t := range [][2]string{{"tcp", "stcp"}, {"udp", "sudp"}} {
			fmt.Fprintf(&b, "\n[[proxies]]\nname = \"rescue-%d-%s\"\ntype = %q\nsecretKey = %q\nlocalIP = \"127.0.0.1\"\nlocalPort = %d\n", p, t[0], t[1], secret, p)
		}
	}
	return b.String()
}

func rescueEntryFrpcToml(originIP string, cport int, token, secret string, ports []int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "serverAddr = %q\nserverPort = %d\nauth.method = \"token\"\nauth.token = %q\nloginFailExit = false\nlog.to = \"console\"\nlog.level = \"info\"\n", originIP, cport, token)
	for _, p := range ports {
		for _, t := range [][2]string{{"tcp", "stcp"}, {"udp", "sudp"}} {
			fmt.Fprintf(&b, "\n[[visitors]]\nname = \"rescue-v-%d-%s\"\ntype = %q\nserverName = \"rescue-%d-%s\"\nsecretKey = %q\nbindAddr = \"0.0.0.0\"\nbindPort = %d\n", p, t[0], t[1], p, t[0], secret, p)
		}
	}
	return b.String()
}

func rescueFwNft(cport int, entryIP string) string {
	return fmt.Sprintf("table inet hashem_rescue {\n  chain input {\n    type filter hook input priority -5; policy accept;\n    iifname \"lo\" accept\n    tcp dport %d ip saddr != %s counter drop\n  }\n}\n", cport, entryIP)
}

func rescueUnit(desc, exec, after, before string) string {
	s := "[Unit]\nDescription=" + desc + "\nAfter=network-online.target" + after + "\nWants=network-online.target\nStartLimitIntervalSec=0\n"
	if before != "" {
		s += "Before=" + before + "\n"
	}
	return s + "\n[Service]\nType=simple\nExecStart=" + exec + "\nRestart=always\nRestartSec=3\nLimitNOFILE=1048576\n\n[Install]\nWantedBy=multi-user.target\n"
}

func rescueFwUnit(nftFile string) string {
	return "[Unit]\nDescription=Hashem rescue: restrict control port to the peer\nAfter=network-pre.target\nBefore=hashem-rescue-frps.service\n\n[Service]\nType=oneshot\nRemainAfterExit=yes\nExecStart=/bin/sh -c \"nft delete table inet hashem_rescue 2>/dev/null; nft -f " + nftFile + "\"\nExecStop=/bin/sh -c \"nft delete table inet hashem_rescue 2>/dev/null; true\"\n\n[Install]\nWantedBy=multi-user.target\n"
}

func writeSecretFile(path, content string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), mode)
}

func rescueUnitPath(name string) string { return filepath.Join(rescueUnitDir, name+".service") }

func sysctl(args ...string) error {
	out, err := runCmd("systemctl", args...)
	if err != nil {
		return fmt.Errorf("systemctl %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(out))
	}
	return nil
}

func rescueHaveFrp() error {
	for _, b := range []string{"frps", "frpc"} {
		if _, err := os.Stat(filepath.Join(rescueBinDir, b)); err != nil {
			return fmt.Errorf("%s not found in %s", b, rescueBinDir)
		}
	}
	return nil
}

// --------------------------------------------------------------------- codes

type rescueCode struct {
	V      int    `json:"v"`
	IP     string `json:"ip"`
	CPort  int    `json:"c"`
	Token  string `json:"t"`
	Secret string `json:"s"`
	Ports  []int  `json:"p"`
}

func rescueEncode(st rescueState, originPub string) string {
	b, _ := json.Marshal(rescueCode{V: 1, IP: originPub, CPort: st.CtrlPort, Token: st.Token, Secret: st.Secret, Ports: st.Ports})
	return rescueCodePrefix + base64.RawURLEncoding.EncodeToString(b)
}

func rescueDecode(code string) (rescueCode, error) {
	var c rescueCode
	code = strings.TrimSpace(code)
	if len(code) > 4096 || !strings.HasPrefix(code, rescueCodePrefix) {
		return c, errors.New("not a rescue code")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(code, rescueCodePrefix))
	if err != nil {
		return c, errors.New("rescue code is corrupted")
	}
	if err := json.Unmarshal(raw, &c); err != nil || c.V != 1 {
		return c, errors.New("rescue code is corrupted")
	}
	ip, err := validRemoteIP(c.IP)
	if err != nil {
		return c, err
	}
	c.IP = ip
	if c.CPort < 1 || c.CPort > 65535 || len(c.Token) < 16 || len(c.Token) > 128 || len(c.Secret) < 8 || len(c.Secret) > 128 ||
		!isHex(c.Token) || !isHex(c.Secret) || len(c.Ports) == 0 || len(c.Ports) > rescueMaxPorts {
		return c, errors.New("rescue code is invalid")
	}
	for _, p := range c.Ports {
		if p < 1 || p > 65535 {
			return c, errors.New("rescue code has an invalid port")
		}
	}
	return c, nil
}

func isHex(s string) bool {
	_, err := hex.DecodeString(s)
	return err == nil && s != ""
}

// ------------------------------------------------------------------- actions

// rescuePublicIP is this host's address as the peer should dial it.
func rescuePublicIP() string {
	if v := detectPublicIPCached(); v != "" {
		return v
	}
	return ""
}

// rescueEnableOrigin: run on the BLOCKED side. entryIP = the reachable peer.
func rescueEnableOrigin(entryIP string, ports []int) (rescueState, error) {
	rescueMu.Lock()
	defer rescueMu.Unlock()
	var st rescueState
	ip, err := validRemoteIP(entryIP)
	if err != nil {
		return st, err
	}
	if cur := loadRescueLocked(); cur.Role != "" {
		return st, errors.New("rescue is already active — disable it first")
	}
	reserved := rescueReserved()
	var use []int
	for _, p := range ports {
		if reserved[p] {
			return st, fmt.Errorf("port %d is reserved on this server", p)
		}
		use = append(use, p)
	}
	if len(use) == 0 || len(use) > rescueMaxPorts {
		return st, errors.New("choose between 1 and 64 ports")
	}
	if err := rescueHaveFrp(); err != nil {
		return st, err
	}
	avoid := map[int]bool{}
	for _, p := range use {
		avoid[p] = true
	}
	cp := pickCtrlPort(avoid)
	if cp == 0 {
		return st, errors.New("no free control port")
	}
	st = rescueState{Role: "origin", RemoteIP: ip, CtrlPort: cp, Token: randHex(24), Secret: randHex(16), Ports: use, Since: time.Now().Unix()}

	d := rescueDir()
	frps, frpc := filepath.Join(rescueBinDir, "frps"), filepath.Join(rescueBinDir, "frpc")
	write := func() error {
		if err := os.MkdirAll(d, 0700); err != nil {
			return err
		}
		files := []struct {
			p, c string
			m    os.FileMode
		}{
			{filepath.Join(d, "frps.toml"), rescueFrpsToml(cp, st.Token), 0600},
			{filepath.Join(d, "frpc.toml"), rescueOriginFrpcToml(cp, st.Token, st.Secret, use), 0600},
			{filepath.Join(d, "fw.nft"), rescueFwNft(cp, ip), 0600},
			{rescueUnitPath("hashem-rescue-fw"), rescueFwUnit(filepath.Join(d, "fw.nft")), 0644},
			{rescueUnitPath("hashem-rescue-frps"), rescueUnit("Hashem rescue control (frps)", frps+" -c "+filepath.Join(d, "frps.toml"), " hashem-rescue-fw.service", ""), 0644},
			{rescueUnitPath("hashem-rescue-frpc"), rescueUnit("Hashem rescue origin (frpc)", frpc+" -c "+filepath.Join(d, "frpc.toml"), " hashem-rescue-frps.service", ""), 0644},
		}
		for _, f := range files {
			if err := writeSecretFile(f.p, f.c, f.m); err != nil {
				return err
			}
		}
		if err := saveRescueLocked(st); err != nil {
			return err
		}
		if err := sysctl("daemon-reload"); err != nil {
			return err
		}
		for _, s := range []string{"hashem-rescue-fw", "hashem-rescue-frps", "hashem-rescue-frpc"} {
			if err := sysctl("enable", "--now", s+".service"); err != nil {
				return err
			}
		}
		return nil
	}
	if err := write(); err != nil {
		rescueTeardownLocked()
		return rescueState{}, err
	}
	LogSecurityAudit("rescue_enabled", "admin", "", fmt.Sprintf("role=origin entry=%s ports=%d", ip, len(use)))
	return st, nil
}

// rescueApplyCode: run on the REACHABLE side (auto from peer pull, or pasted).
func rescueApplyCode(code string) (rescueState, error) {
	rescueMu.Lock()
	defer rescueMu.Unlock()
	var st rescueState
	c, err := rescueDecode(code)
	if err != nil {
		return st, err
	}
	if cur := loadRescueLocked(); cur.Role != "" {
		return st, errors.New("rescue is already active — disable it first")
	}
	if err := rescueHaveFrp(); err != nil {
		return st, err
	}
	reserved := rescueReserved(c.CPort)
	var use, skipped []int
	for _, p := range c.Ports {
		if reserved[p] || !portFree(p) {
			skipped = append(skipped, p)
			continue
		}
		use = append(use, p)
	}
	if len(use) == 0 {
		return st, errors.New("none of the ports can be used here (reserved or already in use)")
	}
	st = rescueState{Role: "entry", RemoteIP: c.IP, CtrlPort: c.CPort, Token: c.Token, Secret: c.Secret, Ports: use, Skipped: skipped, Since: time.Now().Unix()}
	d := rescueDir()
	frpc := filepath.Join(rescueBinDir, "frpc")
	write := func() error {
		for _, f := range []struct {
			p, c string
			m    os.FileMode
		}{
			{filepath.Join(d, "frpc.toml"), rescueEntryFrpcToml(c.IP, c.CPort, c.Token, c.Secret, use), 0600},
			{rescueUnitPath("hashem-rescue-frpc"), rescueUnit("Hashem rescue entry (frpc visitors)", frpc+" -c "+filepath.Join(d, "frpc.toml"), "", ""), 0644},
		} {
			if err := writeSecretFile(f.p, f.c, f.m); err != nil {
				return err
			}
		}
		if err := os.Chmod(d, 0700); err != nil {
			return err
		}
		if err := saveRescueLocked(st); err != nil {
			return err
		}
		if err := sysctl("daemon-reload"); err != nil {
			return err
		}
		return sysctl("enable", "--now", "hashem-rescue-frpc.service")
	}
	if err := write(); err != nil {
		rescueTeardownLocked()
		return rescueState{}, err
	}
	LogSecurityAudit("rescue_enabled", "admin", "", fmt.Sprintf("role=entry origin=%s ports=%d skipped=%d", c.IP, len(use), len(skipped)))
	return st, nil
}

// rescueTeardownLocked removes everything this feature ever created.
func rescueTeardownLocked() {
	for _, s := range []string{"hashem-rescue-frpc", "hashem-rescue-frps", "hashem-rescue-fw"} {
		_, _ = runCmd("systemctl", "disable", "--now", s+".service")
		_ = os.Remove(rescueUnitPath(s))
	}
	_, _ = runCmd("systemctl", "daemon-reload")
	_, _ = runCmd("nft", "delete", "table", "inet", "hashem_rescue")
	_ = os.RemoveAll(rescueDir())
	cur := loadRescueLocked()
	_ = os.Remove(rescueStatePath())
	// Keep the detector's memory of "this was fine" but drop the active role.
	_ = cur
}

func rescueDisable() {
	rescueMu.Lock()
	defer rescueMu.Unlock()
	rescueTeardownLocked()
	LogSecurityAudit("rescue_disabled", "admin", "", "")
}

// ----------------------------------------------------------------- detection

type rescueTarget struct {
	Host  string `json:"host"`
	Ports []int  `json:"ports"`
}

// rescueTargets lists hosts whose reachability matters, with ports that
// should normally be open (their panel). Ports come from config, never guessed
// open services.
func rescueTargets() []rescueTarget {
	byHost := map[string]*rescueTarget{}
	var order []string
	add := func(host string, port int) {
		ip, err := validRemoteIP(host)
		if err != nil || port < 1 || port > 65535 {
			return
		}
		t, ok := byHost[ip]
		if !ok {
			t = &rescueTarget{Host: ip}
			byHost[ip] = t
			order = append(order, ip)
		}
		for _, p := range t.Ports {
			if p == port {
				return
			}
		}
		t.Ports = append(t.Ports, port)
	}
	if pc := loadPeerConfig(); pc.PeerURL != "" {
		u := strings.TrimSpace(pc.PeerURL)
		if !strings.Contains(u, "://") {
			u = "http://" + u
		}
		if pu, err := url.Parse(u); err == nil {
			port := cfg.Port
			if pp, e := strconv.Atoi(pu.Port()); e == nil {
				port = pp
			}
			add(pu.Hostname(), port)
		}
	}
	for _, p := range loadPeers() {
		add(p.RemotePub, cfg.Port)
	}
	out := make([]rescueTarget, 0, len(order))
	for _, h := range order {
		out = append(out, *byHost[h])
	}
	return out
}

type rescueProbeResult struct {
	Host    string         `json:"host"`
	Verdict string         `json:"verdict"`
	PingOK  bool           `json:"ping_ok"`
	Ports   map[int]string `json:"ports"`
}

func classifyDial(err error) string {
	if err == nil {
		return "open"
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return "refused"
	}
	return "timeout"
}

// rescueVerdict is pure so the decision rule is unit-tested in isolation.
//
//	ok               at least one port answers
//	service_down     remote is reachable (it sent RST) but nothing listens
//	one_way_block    ping works, every TCP SYN vanishes -> this is our case
//	host_unreachable neither ping nor TCP work (outage, not a block)
func rescueVerdict(pingOK bool, ports map[int]string) string {
	if len(ports) == 0 {
		return "unknown"
	}
	refused := false
	for _, s := range ports {
		switch s {
		case "open":
			return "ok"
		case "refused":
			refused = true
		}
	}
	if refused {
		return "service_down"
	}
	if pingOK {
		return "one_way_block"
	}
	return "host_unreachable"
}

var rescueProbeFn = rescueProbeReal

func rescueProbeReal(t rescueTarget) rescueProbeResult {
	res := rescueProbeResult{Host: t.Host, Ports: map[int]string{}}
	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, p := range t.Ports {
		wg.Add(1)
		go func(p int) {
			defer wg.Done()
			c, err := rescueDial(net.JoinHostPort(t.Host, strconv.Itoa(p)), 4*time.Second)
			if c != nil {
				_ = c.Close()
			}
			mu.Lock()
			res.Ports[p] = classifyDial(err)
			mu.Unlock()
		}(p)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err := runCmd("ping", "-c", "2", "-W", "2", t.Host)
		mu.Lock()
		res.PingOK = err == nil
		mu.Unlock()
	}()
	wg.Wait()
	res.Verdict = rescueVerdict(res.PingOK, res.Ports)
	return res
}

// rescueObserve folds one probe into the persisted state. It only ever sets
// the banner after rescueStrikes consecutive blocked results.
func rescueObserve(res rescueProbeResult, now time.Time) rescueState {
	rescueMu.Lock()
	defer rescueMu.Unlock()
	st := loadRescueLocked()
	before := st
	st.LastVerdict, st.LastProbe = res.Verdict, now.Unix()
	if st.Role == "" {
		if res.Verdict == "one_way_block" {
			st.Strikes++
			if st.Strikes >= rescueStrikes && st.SuspectedSince == 0 {
				st.SuspectedSince = now.Unix()
			}
		} else {
			st.Strikes, st.SuspectedSince = 0, 0
		}
	} else if st.Role == "origin" {
		// While rescued, a working direct route means the block is gone.
		if res.Verdict == "ok" {
			if st.LiftedSince == 0 {
				st.LiftedSince = now.Unix()
			}
		} else {
			st.LiftedSince = 0
		}
	}
	// Persist only when something other than the probe timestamp changed, so a
	// healthy idle panel does not rewrite rescue.json every minute.
	if st.Role != before.Role || st.Strikes != before.Strikes || st.SuspectedSince != before.SuspectedSince ||
		st.LiftedSince != before.LiftedSince || st.LastVerdict != before.LastVerdict {
		_ = saveRescueLocked(st)
	}
	return st
}

func startRescueMonitor() {
	go func() {
		time.Sleep(20 * time.Second)
		tick := 0
		for {
			st := func() rescueState { rescueMu.Lock(); defer rescueMu.Unlock(); return loadRescueLocked() }()
			if st.Role != "entry" {
				if ts := rescueTargets(); len(ts) > 0 {
					rescueObserve(rescueProbeFn(ts[0]), time.Now())
				}
			}
			if st.Role == "" && tick%5 == 0 {
				rescuePullOffer()
			}
			tick++
			time.Sleep(60 * time.Second)
		}
	}()
}

// rescuePullOffer asks every known peer panel whether it published an offer
// for us. Only hosts we already trust as peers are contacted, with the shared
// peer secret; ports are re-validated locally in rescueApplyCode.
func rescuePullOffer() {
	pc := loadPeerConfig()
	if pc.PeerSecret == "" {
		return
	}
	// Ports to try per peer host: the port its peer_url names first, then this
	// panel's own port and the two usual defaults (panels on one fleet often sit
	// on 7777 on one box and 7778 on another when 7777 is taken).
	hosts := map[string][]int{}
	for _, t := range rescueTargets() {
		seen := map[int]bool{}
		var ps []int
		for _, p := range append(append([]int{}, t.Ports...), cfg.Port, 7777, 7778) {
			if p >= 1 && p <= 65535 && !seen[p] {
				seen[p] = true
				ps = append(ps, p)
			}
		}
		hosts[t.Host] = ps
	}
	client := &http.Client{Timeout: 4 * time.Second}
	// Peer panels normally share one secret path (same convention as
	// sendToPeer); a path written in peer_url wins when it differs.
	bases := []string{""}
	if cfg.BasePath != "" {
		bases[0] = "/" + strings.Trim(cfg.BasePath, "/")
	}
	if u, err := url.Parse(strings.TrimSpace(pc.PeerURL)); err == nil && strings.Trim(u.Path, "/") != "" {
		if pb := "/" + strings.Trim(u.Path, "/"); pb != bases[0] {
			bases = append([]string{pb}, bases...)
		}
	}
	for h, ports := range hosts {
		for _, port := range ports {
			for _, base := range bases {
				req, err := http.NewRequest("GET", fmt.Sprintf("http://%s:%d%s/api/peer/rescue-offer", h, port, base), nil)
				if err != nil {
					continue
				}
				req.Header.Set("X-Peer-Secret", pc.PeerSecret)
				resp, err := client.Do(req)
				if err != nil {
					continue
				}
				var body struct {
					Offer bool   `json:"offer"`
					Code  string `json:"code"`
				}
				err = json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 8192)).Decode(&body)
				_ = resp.Body.Close()
				if err != nil || !body.Offer {
					continue
				}
				c, derr := rescueDecode(body.Code)
				if derr != nil || c.IP != h { // the offer must come from the host that issued it
					continue
				}
				if _, aerr := rescueApplyCode(body.Code); aerr == nil {
					ack, _ := http.NewRequest("POST", fmt.Sprintf("http://%s:%d%s/api/peer/rescue-ack", h, port, base), nil)
					ack.Header.Set("X-Peer-Secret", pc.PeerSecret)
					if r2, e := client.Do(ack); e == nil {
						_ = r2.Body.Close()
					}
					return
				}
			}
		}
	}
}

// ----------------------------------------------------------------- status/UI

func rescueUnitActive(name string) bool {
	out, err := runCmd("systemctl", "is-active", name+".service")
	return err == nil && strings.TrimSpace(out) == "active"
}

func rescueConnected(st rescueState) bool {
	if st.Role == "" || st.CtrlPort == 0 {
		return false
	}
	var filter string
	if st.Role == "origin" {
		filter = "( sport = :" + strconv.Itoa(st.CtrlPort) + " and dst " + st.RemoteIP + " )"
	} else {
		filter = "( dport = :" + strconv.Itoa(st.CtrlPort) + " and dst " + st.RemoteIP + " )"
	}
	out, err := runCmd("ss", "-Htn", "state", "established", filter)
	return err == nil && strings.TrimSpace(out) != ""
}

func rescueStatusMap(full bool) map[string]any {
	rescueMu.Lock()
	st := loadRescueLocked()
	rescueMu.Unlock()
	m := map[string]any{
		"role": st.Role, "remote_ip": st.RemoteIP, "ports": st.Ports, "skipped": st.Skipped,
		"since": st.Since, "verdict": st.LastVerdict, "last_probe": st.LastProbe,
		"suspected":     st.Role == "" && st.SuspectedSince > 0,
		"block_lifted":  st.Role == "origin" && st.LiftedSince > 0,
		"offer_pending": st.Role == "origin" && !st.Consumed,
		"frp_ok":        rescueHaveFrp() == nil,
		"strikes":       st.Strikes,
	}
	if st.Role == "origin" {
		m["ctrl_port"] = st.CtrlPort
	}
	if st.Role != "" {
		svc := map[string]bool{"frpc": rescueUnitActive("hashem-rescue-frpc")}
		if st.Role == "origin" {
			svc["frps"] = rescueUnitActive("hashem-rescue-frps")
			svc["firewall"] = rescueUnitActive("hashem-rescue-fw")
		}
		m["services"] = svc
		m["connected"] = rescueConnected(st)
	}
	if full {
		ts := rescueTargets()
		m["targets"] = ts
		if len(ts) > 0 {
			m["suggest_remote"] = ts[0].Host
		}
		seen := map[int]bool{}
		var sp []int
		res := rescueReserved()
		add := func(p int) {
			if p > 0 && p < 65536 && !seen[p] && !res[p] {
				seen[p] = true
				sp = append(sp, p)
			}
		}
		ls := localStatus()
		for _, p := range ls.ProxyPorts {
			add(p)
		}
		for _, p := range ls.Ports {
			add(p)
		}
		for _, pr := range loadPeers() {
			for _, p := range pr.Ports {
				add(p)
			}
		}
		sort.Ints(sp)
		m["suggest_ports"] = sp
	}
	return m
}

// ------------------------------------------------------------------ handlers

func handleRescueGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, rescueStatusMap(r.URL.Query().Get("full") == "1"))
}

func handleRescuePost(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	var body struct {
		Action   string `json:"action"`
		RemoteIP string `json:"remote_ip"`
		Ports    string `json:"ports"`
		Code     string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, r, "E-RESCUE-01", "")
		return
	}
	switch body.Action {
	case "probe":
		ts := rescueTargets()
		if len(ts) == 0 {
			writeAPIError(w, r, "E-RESCUE-02", "")
			return
		}
		res := rescueProbeFn(ts[0])
		st := rescueObserve(res, time.Now())
		writeJSON(w, map[string]any{"result": res, "state": rescueStatusMapFrom(st)})
	case "enable":
		ports, err := parsePortList(body.Ports)
		if err != nil {
			writeAPIError(w, r, "E-RESCUE-03", err.Error())
			return
		}
		if _, err := rescueEnableOrigin(body.RemoteIP, ports); err != nil {
			writeAPIError(w, r, "E-RESCUE-04", err.Error())
			return
		}
		writeJSON(w, map[string]any{"status": "ok", "state": rescueStatusMap(false)})
	case "join":
		if _, err := rescueApplyCode(body.Code); err != nil {
			writeAPIError(w, r, "E-RESCUE-05", err.Error())
			return
		}
		writeJSON(w, map[string]any{"status": "ok", "state": rescueStatusMap(false)})
	case "disable":
		rescueDisable()
		writeJSON(w, map[string]any{"status": "ok", "state": rescueStatusMap(false)})
	case "code":
		rescueMu.Lock()
		st := loadRescueLocked()
		rescueMu.Unlock()
		if st.Role != "origin" {
			writeAPIError(w, r, "E-RESCUE-06", "")
			return
		}
		pub := rescuePublicIP()
		if pub == "" {
			writeAPIError(w, r, "E-RESCUE-06", "cannot determine this server's public IP")
			return
		}
		writeJSON(w, map[string]any{"code": rescueEncode(st, pub)})
	default:
		writeAPIError(w, r, "E-RESCUE-01", "unknown rescue action")
	}
}

func rescueStatusMapFrom(st rescueState) map[string]any {
	return map[string]any{"verdict": st.LastVerdict, "suspected": st.Role == "" && st.SuspectedSince > 0, "strikes": st.Strikes}
}

// Peer endpoints (X-Peer-Secret). The offer is only released to the exact
// entry IP the admin named when enabling the origin.
func handlePeerRescueOffer(w http.ResponseWriter, r *http.Request) {
	rescueMu.Lock()
	st := loadRescueLocked()
	rescueMu.Unlock()
	if st.Role != "origin" || st.Consumed {
		writeJSON(w, map[string]any{"offer": false})
		return
	}
	if !authed(r) && ClientIP(r) != st.RemoteIP {
		LogSecurityAudit("rescue_offer_denied", "peer", ClientIP(r), "")
		writeAPIError(w, r, "E-RESCUE-07", "")
		return
	}
	pub := rescuePublicIP()
	if pub == "" {
		writeJSON(w, map[string]any{"offer": false})
		return
	}
	writeJSON(w, map[string]any{"offer": true, "code": rescueEncode(st, pub)})
}

func handlePeerRescueAck(w http.ResponseWriter, r *http.Request) {
	rescueMu.Lock()
	defer rescueMu.Unlock()
	st := loadRescueLocked()
	if st.Role == "origin" && (authed(r) || ClientIP(r) == st.RemoteIP) {
		st.Consumed = true
		_ = saveRescueLocked(st)
	}
	writeJSON(w, map[string]any{"status": "ok"})
}
