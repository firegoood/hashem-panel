package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

var managedRun = func(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	// Native FRP diagnostics may print configuration secrets. Return the command
	// name and exit error, never unsanitized tool output to the browser/journal.
	if err != nil {
		return out, fmt.Errorf("%s failed: %w", filepath.Base(name), err)
	}
	return out, nil
}

func validateManagedPeer(p peerRecord, peers []peerRecord) error {
	if p.ID < 1 || p.ID > 5 {
		return fmt.Errorf("peer id must be 1..5")
	}
	expected := p
	managedNames(&expected)
	if p.Managed && (p.GreIf != expected.GreIf || p.FrpsSvc != expected.FrpsSvc) {
		return fmt.Errorf("managed resource names do not match peer ownership")
	}
	if p.Role != "iran" && p.Role != "foreign" {
		return fmt.Errorf("role must be iran or foreign")
	}
	if p.Engine != "" && p.Engine != "frp" {
		return fmt.Errorf("managed lifecycle supports FRP")
	}
	if p.FRPTransport != "tcp" && p.FRPTransport != "kcp" {
		return fmt.Errorf("managed GRE transport must be tcp or kcp")
	}
	if p.Carrier != "" && p.Carrier != "direct" {
		return fmt.Errorf("managed peers currently require the direct GRE carrier")
	}
	if len(p.Name) > 80 || strings.ContainsAny(p.Name, "\r\n\x00") {
		return fmt.Errorf("invalid peer name")
	}
	if len(p.Token) < 16 || len(p.Token) > 128 || strings.ContainsAny(p.Token, "\r\n\x00") {
		return fmt.Errorf("FRP token must contain 16..128 characters")
	}
	if p.ProxyProtocol != "" && p.ProxyProtocol != "off" && p.ProxyProtocol != "v1" && p.ProxyProtocol != "v2" {
		return fmt.Errorf("invalid proxy protocol")
	}
	for _, s := range []string{p.LocalPub, p.RemotePub, p.LocalGre, p.PeerGre} {
		a, err := netip.ParseAddr(s)
		if err != nil || !a.Is4() || a.IsUnspecified() || a.IsMulticast() {
			return fmt.Errorf("valid unicast IPv4 addresses required")
		}
	}
	local, _ := netip.ParseAddr(p.LocalGre)
	remote, _ := netip.ParseAddr(p.PeerGre)
	subnet := netip.PrefixFrom(local, 30).Masked()
	lb, rb := local.As4(), remote.As4()
	if !local.IsPrivate() || !remote.IsPrivate() || !subnet.Contains(remote) || local == remote || lb[3]%4 == 0 || lb[3]%4 == 3 || rb[3]%4 == 0 || rb[3]%4 == 3 {
		return fmt.Errorf("GRE addresses must be distinct usable private hosts in the same /30")
	}
	if p.LocalPub == p.RemotePub {
		return fmt.Errorf("GRE public endpoints must differ")
	}
	if p.FrpPort < 1 || p.FrpPort > 65535 {
		return fmt.Errorf("invalid control port")
	}
	ms, err := peerMappings(p)
	if err != nil {
		return err
	}
	used := map[string]bool{}
	for _, m := range ms {
		if m.Public == p.FrpPort {
			return fmt.Errorf("proxy port conflicts with control port")
		}
		used[fmt.Sprintf("%s:%d", m.Protocol, m.Public)] = true
	}
	for _, other := range peers {
		if other.ID == p.ID {
			continue
		}
		if other.FrpPort == p.FrpPort {
			return fmt.Errorf("control port belongs to peer %d", other.ID)
		}
		if other.RemotePub == p.RemotePub {
			return fmt.Errorf("remote endpoint belongs to peer %d", other.ID)
		}
		if addr, err := netip.ParseAddr(other.LocalGre); err == nil && subnet.Overlaps(netip.PrefixFrom(addr, 30).Masked()) {
			return fmt.Errorf("GRE subnet overlaps peer %d", other.ID)
		}
		oms, err := peerMappings(other)
		if err != nil {
			return fmt.Errorf("peer %d has invalid port state", other.ID)
		}
		for _, m := range oms {
			if used[fmt.Sprintf("%s:%d", m.Protocol, m.Public)] {
				return fmt.Errorf("public %s port %d belongs to peer %d", m.Protocol, m.Public, other.ID)
			}
		}
		if other.Token == p.Token {
			return fmt.Errorf("each peer requires an independent FRP token")
		}
	}
	return nil
}

func listenerAvailable(proto, ip string, port int) error {
	address := net.JoinHostPort(ip, strconv.Itoa(port))
	if proto == "udp" {
		c, err := net.ListenPacket("udp4", address)
		if err != nil {
			return fmt.Errorf("UDP port %d is occupied", port)
		}
		return c.Close()
	}
	c, err := net.Listen("tcp4", address)
	if err != nil {
		return fmt.Errorf("TCP port %d is occupied", port)
	}
	return c.Close()
}

func managedPreflight(p peerRecord, old *peerRecord) error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("peer lifecycle requires Linux; development tests must inject a disposable system")
	}
	if _, err := os.Stat(managedFRPBinary(p)); err != nil {
		return fmt.Errorf("verified FRP 0.71.0 binaries required; run install-fork.sh")
	}
	out, err := managedRun(managedFRPBinary(p), "--version")
	if err != nil || strings.TrimSpace(string(out)) != "0.71.0" {
		return fmt.Errorf("FRP 0.71.0 is required")
	}
	if _, err := exec.LookPath("iptables"); err != nil {
		return fmt.Errorf("iptables compatibility tools required; no firewall changes made")
	}
	if old == nil {
		if _, err := managedRun("ip", "link", "show", p.GreIf); err == nil {
			return fmt.Errorf("interface %s already exists without ownership", p.GreIf)
		}
		for path := range managedUnits(p) {
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				return fmt.Errorf("unit %s already exists without ownership", filepath.Base(path))
			}
		}
		if _, err := os.Stat(managedDir(p.ID)); !os.IsNotExist(err) {
			return fmt.Errorf("peer resource directory exists without registry ownership; inspect recovery journal")
		}
	}
	out, err = managedRun("ip", "-j", "-4", "route", "show", "table", "all")
	if err != nil {
		return err
	}
	var routes []struct {
		Dst string `json:"dst"`
		Dev string `json:"dev"`
	}
	if err = json.Unmarshal(out, &routes); err != nil {
		return fmt.Errorf("cannot inspect routes")
	}
	a, _ := netip.ParseAddr(p.LocalGre)
	subnet := netip.PrefixFrom(a, 30).Masked()
	for _, r := range routes {
		if old != nil && r.Dev == old.GreIf {
			continue
		}
		if existing, err := netip.ParsePrefix(r.Dst); err == nil && existing.Bits() > 0 && subnet.Overlaps(existing) {
			return fmt.Errorf("GRE subnet overlaps route on %s", r.Dev)
		}
	}
	oldMappings := map[string]bool{}
	if old != nil {
		ms, _ := peerMappings(*old)
		for _, m := range ms {
			oldMappings[fmt.Sprintf("%s:%d", m.Protocol, m.Public)] = true
		}
	}
	ms, _ := peerMappings(p)
	if p.Role == "iran" {
		for _, m := range ms {
			if !oldMappings[fmt.Sprintf("%s:%d", m.Protocol, m.Public)] {
				if err := listenerAvailable(m.Protocol, "0.0.0.0", m.Public); err != nil {
					return err
				}
			}
		}
		// Binding wildcard before GRE creation detects an unrelated TCP/UDP owner.
		if old == nil || old.FrpPort != p.FrpPort {
			for _, proto := range []string{"tcp", "udp"} {
				if err := listenerAvailable(proto, "0.0.0.0", p.FrpPort); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

var managedCheck = managedPreflight

func ownedFirewallRules(p peerRecord) [][]string {
	comment := fmt.Sprintf("hashem:peer:%d", p.ID)
	base := func(args ...string) []string {
		return append(args, "-m", "comment", "--comment", comment, "-j", "ACCEPT")
	}
	rules := [][]string{base("-p", "47", "-s", p.RemotePub, "-d", p.LocalPub)}
	for _, proto := range []string{"tcp", "udp"} {
		rules = append(rules, base("-i", p.GreIf, "-p", proto, "--dport", strconv.Itoa(p.FrpPort)))
	}
	if p.Role == "iran" {
		managementPort := effectiveTLSPort()
		if endpoint, err := url.Parse(p.MasterURL); err == nil {
			if port, err := strconv.Atoi(endpoint.Port()); err == nil && port > 0 && port <= 65535 {
				managementPort = port
			}
		}
		rules = append(rules, base("-i", p.GreIf, "-p", "tcp", "--dport", strconv.Itoa(managementPort)))
		rules = append(rules, base("-p", "tcp", "-s", p.RemotePub, "--dport", strconv.Itoa(managementPort)))
		ms, _ := peerMappings(p)
		for _, m := range ms {
			rules = append(rules, base("-p", m.Protocol, "--dport", strconv.Itoa(m.Public)))
		}
	}
	return rules
}

func firewallCommand(op string, rule []string) error {
	args := append([]string{"-w", "5", op, "INPUT"}, rule...)
	_, err := managedRun("iptables", args...)
	return err
}

type managedSnapshot struct {
	Peer         peerRecord        `json:"peer"`
	Previous     *peerRecord       `json:"previous,omitempty"`
	Files        map[string][]byte `json:"files"`
	Missing      []string          `json:"missing"`
	AddedRules   [][]string        `json:"added_rules"`
	RemovedRules [][]string        `json:"removed_rules"`
	Removing     bool              `json:"removing"`
	Control      bool              `json:"control"`
	Active       bool              `json:"active"`
}

func (s *managedSnapshot) restore() error {
	var failures []string
	p := s.Peer
	_, _ = managedRun("systemctl", "stop", p.FrpsSvc+".service", p.GreIf+".service")
	for path, data := range s.Files {
		if err := atomicPrivateFile(path, data, 0600); err != nil {
			failures = append(failures, err.Error())
		}
	}
	for _, path := range s.Missing {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			failures = append(failures, err.Error())
		}
	}
	for _, rule := range s.AddedRules {
		if firewallCommand("-C", rule) != nil {
			continue
		}
		if err := firewallCommand("-D", rule); err != nil {
			failures = append(failures, err.Error())
		}
	}
	for _, rule := range s.RemovedRules {
		if firewallCommand("-C", rule) != nil {
			if err := firewallCommand("-I", rule); err != nil {
				failures = append(failures, err.Error())
			}
		}
	}
	if s.Previous == nil {
		if err := os.Remove(managedDir(p.ID)); err != nil && !os.IsNotExist(err) {
			failures = append(failures, "owned staging directory could not be removed")
		}
	}
	if s.Previous == nil {
		_, _ = managedRun("systemctl", "disable", p.FrpsSvc+".service", p.GreIf+".service")
	}
	_, _ = managedRun("systemctl", "daemon-reload")
	if s.Previous != nil {
		verb := "enable"
		if s.Previous.Disabled {
			verb = "disable"
		}
		if _, err := managedRun("systemctl", verb, p.GreIf+".service", p.FrpsSvc+".service"); err != nil {
			failures = append(failures, err.Error())
		}
	}
	if s.Previous != nil && s.Active {
		if _, err := managedRun("systemctl", "restart", p.GreIf+".service", p.FrpsSvc+".service"); err != nil {
			failures = append(failures, err.Error())
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("rollback needs repair: %s", strings.Join(failures, "; "))
	}
	return nil
}

func managedJournal(p peerRecord) string {
	return filepath.Join(configDir, fmt.Sprintf("peer-%d.transaction.json", p.ID))
}

func managedApply(p peerRecord, old *peerRecord, peers []peerRecord) (err error) {
	return managedApplyFiles(p, old, peers, nil)
}

func managedApplyFiles(p peerRecord, old *peerRecord, peers []peerRecord, restored map[string][]byte) (err error) {
	p.OperationID = randomToken(32)
	if err = validateManagedPeer(p, peers); err != nil {
		return err
	}
	if err = managedCheck(p, old); err != nil {
		return err
	}
	files := managedUnits(p)
	files[filepath.Join(managedDir(p.ID), "peer.json")] = mustJSON(p)
	for path, data := range restored {
		files[path] = data
	}
	if p.Role == "iran" {
		if p.ServerCA == "" || (old != nil && old.LocalGre != p.LocalGre) {
			cert, key, e := makePeerCertificate(p.LocalGre)
			if e != nil {
				return e
			}
			p.ServerCA = string(cert)
			files[filepath.Join(managedDir(p.ID), "server.crt")] = cert
			files[filepath.Join(managedDir(p.ID), "server.key")] = key
		}
	} else {
		files[filepath.Join(managedDir(p.ID), "ca.crt")] = []byte(p.ServerCA)
	}
	config, err := managedFRPConfig(p)
	if err != nil {
		return err
	}
	files[managedConfigPath(p)] = config
	stage, err := os.CreateTemp(configDir, ".frp-verify-*.toml")
	if err != nil {
		return err
	}
	defer os.Remove(stage.Name())
	if _, err = stage.Write(config); err != nil {
		_ = stage.Close()
		return err
	}
	_ = stage.Close()
	if _, err = managedRun(managedFRPBinary(p), "verify", "-c", stage.Name()); err != nil {
		return fmt.Errorf("FRP rejected proposed configuration: %w", err)
	}
	snapshot := managedSnapshot{Peer: p, Previous: old, Files: map[string][]byte{}}
	if old != nil {
		_, e := managedRun("systemctl", "is-active", "--quiet", old.FrpsSvc+".service")
		snapshot.Active = e == nil
	}
	for path := range files {
		data, e := os.ReadFile(path)
		if e == nil {
			snapshot.Files[path] = data
		} else if os.IsNotExist(e) {
			snapshot.Missing = append(snapshot.Missing, path)
		} else {
			return e
		}
	}
	// Persist intent before the first system mutation. A subsequent operation
	// recovers an interrupted transaction instead of accepting orphaned resources.
	persist := func() error {
		data, e := json.Marshal(snapshot)
		if e != nil {
			return e
		}
		return atomicPrivateFile(managedJournal(p), data, 0600)
	}
	if err = persist(); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			if e := snapshot.restore(); e != nil {
				err = fmt.Errorf("%w; %v", err, e)
				return
			}
		}
		_ = os.Remove(managedJournal(p))
	}()
	for path, data := range files {
		if err = atomicPrivateFile(path, data, 0600); err != nil {
			return err
		}
	}
	for _, rule := range ownedFirewallRules(p) {
		if firewallCommand("-C", rule) == nil {
			continue
		}
		// Journal before insertion. Recovery checks existence before deletion.
		snapshot.AddedRules = append(snapshot.AddedRules, rule)
		if err = persist(); err != nil {
			return err
		}
		if err = firewallCommand("-I", rule); err != nil {
			return fmt.Errorf("firewall activation failed: %w", err)
		}
	}
	if _, err = managedRun("systemctl", "daemon-reload"); err != nil {
		return err
	}
	if _, err = managedRun("systemctl", "enable", p.GreIf+".service", p.FrpsSvc+".service"); err != nil {
		return err
	}
	if old == nil || old.LocalPub != p.LocalPub || old.RemotePub != p.RemotePub || old.LocalGre != p.LocalGre {
		if _, err = managedRun("systemctl", "stop", p.FrpsSvc+".service"); err != nil {
			return err
		}
		if _, err = managedRun("systemctl", "restart", p.GreIf+".service"); err != nil {
			return err
		}
	}
	if _, err = managedRun("systemctl", "restart", p.FrpsSvc+".service"); err != nil {
		return err
	}
	if _, err = managedRun("systemctl", "is-active", "--quiet", p.FrpsSvc+".service"); err != nil {
		return fmt.Errorf("FRP service did not activate: %w", err)
	}
	// Actual registration/forwarding is verified separately. Pending is never
	// synonymous with connected, even when systemd reports active.
	p.State = "PENDING"
	for i := range peers {
		if peers[i].ID == p.ID {
			peers[i] = p
			goto commit
		}
	}
	peers = append(peers, p)
commit:
	sort.Slice(peers, func(i, j int) bool { return peers[i].ID < peers[j].ID })
	if old != nil {
		newRules := ownedFirewallRules(p)
		for _, rule := range ownedFirewallRules(*old) {
			found := false
			for _, n := range newRules {
				if reflect.DeepEqual(rule, n) {
					found = true
					break
				}
			}
			if !found && firewallCommand("-C", rule) == nil {
				snapshot.RemovedRules = append(snapshot.RemovedRules, rule)
				if err = persist(); err != nil {
					return err
				}
				if err = firewallCommand("-D", rule); err != nil {
					return err
				}
			}
		}
	}
	if err = writePeerRegistry(peers); err != nil {
		return err
	}
	return nil
}

func recoverManagedTransactions() error {
	paths, err := filepath.Glob(filepath.Join(configDir, "peer-*.transaction.json"))
	if err != nil {
		return err
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var s managedSnapshot
		if err = json.Unmarshal(data, &s); err != nil {
			return fmt.Errorf("invalid transaction journal; recovery required")
		}
		// A registry commit is the commit marker. Do not roll back a completed
		// operation when the process died just before deleting its journal.
		p := findPeer(s.Peer.ID)
		committed := !s.Removing && p != nil && p.Revision == s.Peer.Revision && p.Managed && p.OperationID == s.Peer.OperationID
		if s.Control {
			committed = committed && p.Disabled == s.Peer.Disabled && p.State == s.Peer.State && p.LastVerified == 0
		}
		if (s.Removing && p == nil) || committed {
			_ = os.Remove(path)
			continue
		}
		if err = s.restore(); err != nil {
			return err
		}
		if err = os.Remove(path); err != nil {
			return err
		}
	}
	return nil
}

func removeManagedPeer(p peerRecord, peers []peerRecord) (err error) {
	if !p.Managed {
		return fmt.Errorf("managed peer required")
	}
	snapshot := managedSnapshot{Peer: p, Previous: &p, Removing: true, Files: map[string][]byte{}}
	_, e := managedRun("systemctl", "is-active", "--quiet", p.FrpsSvc+".service")
	snapshot.Active = e == nil
	paths := []string{managedConfigPath(p), filepath.Join(managedDir(p.ID), "peer.json"), filepath.Join(managedDir(p.ID), "ca.crt"), filepath.Join(managedDir(p.ID), "server.crt"), filepath.Join(managedDir(p.ID), "server.key")}
	for path := range managedUnits(p) {
		paths = append(paths, path)
	}
	if p.Legacy {
		paths = append(paths, legacyConfigPath(p))
	}
	for _, path := range paths {
		data, e := os.ReadFile(path)
		if e == nil {
			snapshot.Files[path] = data
		} else if !os.IsNotExist(e) {
			return e
		}
	}
	persist := func() error {
		data, e := json.Marshal(snapshot)
		if e != nil {
			return e
		}
		return atomicPrivateFile(managedJournal(p), data, 0600)
	}
	if err = persist(); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			if e := snapshot.restore(); e != nil {
				err = fmt.Errorf("%w; %v", err, e)
				return
			}
		}
		_ = os.Remove(managedJournal(p))
	}()
	for _, svc := range []string{p.FrpsSvc + ".service", p.GreIf + ".service"} {
		if _, err = managedRun("systemctl", "stop", svc); err != nil {
			return err
		}
		if _, err = managedRun("systemctl", "disable", svc); err != nil {
			return err
		}
	}
	for _, rule := range ownedFirewallRules(p) {
		if firewallCommand("-C", rule) == nil {
			snapshot.RemovedRules = append(snapshot.RemovedRules, rule)
			if err = persist(); err != nil {
				return err
			}
			if err = firewallCommand("-D", rule); err != nil {
				return err
			}
		}
	}
	for path := range snapshot.Files {
		if err = os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	// Refuse to recursively delete unexpected files, even inside an owned directory.
	if err = os.Remove(managedDir(p.ID)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("unexpected files remain in owned peer directory")
	}
	if _, err = managedRun("systemctl", "daemon-reload"); err != nil {
		return err
	}
	keep := make([]peerRecord, 0, len(peers))
	for _, other := range peers {
		if other.ID != p.ID {
			keep = append(keep, other)
		}
	}
	return writePeerRegistry(keep)
}

func managedPeerMutation(op string, p peerRecord) (peerRecord, error) {
	unlock, err := lockPeers()
	if err != nil {
		return p, err
	}
	defer unlock()
	if err = recoverManagedTransactions(); err != nil {
		return p, err
	}
	peers, err := readPeerRegistry()
	if err != nil {
		return p, err
	}
	var old *peerRecord
	for _, other := range peers {
		if other.ID == p.ID {
			c := other
			old = &c
		}
	}
	if op == "add" && p.ID == 0 {
		for id := 1; id <= 5; id++ {
			used := false
			for _, other := range peers {
				if other.ID == id {
					used = true
				}
			}
			if !used {
				p.ID = id
				break
			}
		}
	}
	if op == "add" && old != nil {
		candidate := p
		candidate.ID = old.ID
		candidate.Managed = true
		managedNames(&candidate)
		if candidate.Token == "" {
			candidate.Token = old.Token
		}
		if candidate.FRPTransport == "" {
			candidate.FRPTransport = "tcp"
		}
		candidateMappings, e := peerMappings(candidate)
		if e != nil {
			return p, e
		}
		oldMappings, e := peerMappings(*old)
		if e != nil {
			return p, e
		}
		if reflect.DeepEqual(candidateMappings, oldMappings) && candidate.LocalPub == old.LocalPub && candidate.RemotePub == old.RemotePub && candidate.LocalGre == old.LocalGre && candidate.PeerGre == old.PeerGre && candidate.FrpPort == old.FrpPort && candidate.Token == old.Token && candidate.FRPTransport == old.FRPTransport && candidate.UseEncryption == old.UseEncryption && candidate.UseCompression == old.UseCompression && candidate.ProxyProtocol == old.ProxyProtocol && (candidate.Name == "" || candidate.Name == old.Name) && (candidate.ManagementSecret == "" || candidate.ManagementSecret == old.ManagementSecret) {
			return *old, nil
		}
		return p, fmt.Errorf("peer id already exists; use edit-peer")
	}
	if op != "add" && old == nil {
		return p, fmt.Errorf("peer not found")
	}
	if old != nil && !old.Managed {
		if op == "remove" {
			return p, removeLegacyPeer(*old)
		}
		if op != "update" {
			return p, fmt.Errorf("legacy peer must be updated through the validated migration path")
		}
		if err = validateLegacyOwnership(*old); err != nil {
			return p, err
		}
		p.Legacy = true
	}
	if old != nil && old.Managed {
		if err = validateManagedPeer(*old, nil); err != nil {
			return p, err
		}
	}
	if op == "remove" {
		return p, removeManagedPeer(*old, peers)
	}
	if op == "disable" || op == "enable" || op == "restart" {
		return managedControl(op, *old, peers)
	}
	if op != "add" && op != "update" {
		return p, fmt.Errorf("unsupported peer operation")
	}
	if p.Token == "" {
		p.Token = randomToken(48)
	}
	if p.ManagementSecret == "" {
		p.ManagementSecret = randomToken(64)
	}
	if p.FRPTransport == "" {
		p.FRPTransport = "tcp"
	}
	if p.Role == "" {
		p.Role = "iran"
	}
	p.Managed = true
	p.Engine = "frp"
	p.Carrier = "direct"
	managedNames(&p)
	ms, err := peerMappings(p)
	if err != nil {
		return p, err
	}
	p.RawPorts = canonicalMappings(ms)
	p.Ports = nil
	seen := map[int]bool{}
	for _, m := range ms {
		if !seen[m.Public] {
			p.Ports = append(p.Ports, m.Public)
			seen[m.Public] = true
		}
	}
	if p.Name == "" {
		p.Name = fmt.Sprintf("peer-%d", p.ID)
	}
	if old == nil {
		if p.Role == "iran" {
			p.Revision = 1
		} else if p.Revision == 0 {
			return p, fmt.Errorf("foreign peer requires a desired revision")
		}
	} else {
		if p.Role == "iran" {
			p.Revision = old.Revision + 1
		} else if p.Revision == 0 {
			return p, fmt.Errorf("foreign desired revision required")
		}
	}
	if err = validateManagedPeer(p, peers); err != nil {
		return p, err
	}
	if p.Role == "iran" {
		pin, e := ensureManagementCertificate()
		if e != nil {
			return p, e
		}
		p.MasterPin = pin
		p.MasterURL = fmt.Sprintf("https://%s:%d/api/managed-peer", p.LocalGre, effectiveTLSPort())
	}
	if err = managedApply(p, old, peers); err != nil {
		return p, err
	}
	if committed := findPeer(p.ID); committed != nil {
		return *committed, nil
	}
	return p, fmt.Errorf("registry commit disappeared")
}
