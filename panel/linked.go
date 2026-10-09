package main

import (
	"net"
	"os/exec"
	"strings"
)

// controlLinked reports whether a foreign client holds an ESTABLISHED TCP
// session to this hub's control port (or its WSS TLS front, port+2). It is the
// only honest "FRP is connected" signal: the per-peer frps unit being active
// says nothing about whether the client reached it. The client may arrive over
// the GRE inner address or the hub's public IP (dial route), so a match on
// either remote address counts.
func controlLinked(port int, remotePub, peerGre string) bool {
	if port <= 0 {
		return false
	}
	out, err := exec.Command("ss", "-Htn", "state", "established").Output()
	if err != nil {
		return false
	}
	return linkedFromSS(string(out), []int{port, port + 2, port - 2}, remotePub, peerGre)
}

// controlLinkedAny is controlLinked without a remote filter (main tunnel).
func controlLinkedAny(port int) bool {
	if port <= 0 {
		return false
	}
	out, err := exec.Command("ss", "-Htn", "state", "established").Output()
	if err != nil {
		return false
	}
	return linkedFromSS(string(out), []int{port, port + 2}, "", "")
}

// linkedFromSS parses `ss -Htn state established` output (Recv-Q Send-Q Local Peer).
func linkedFromSS(out string, ports []int, remotes ...string) bool {
	want := map[string]bool{}
	for _, r := range remotes {
		if r = strings.TrimSpace(r); r != "" {
			want[r] = true
		}
	}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		_, lport, e1 := net.SplitHostPort(f[2])
		rhost, _, e2 := net.SplitHostPort(f[3])
		if e1 != nil || e2 != nil {
			continue
		}
		hit := false
		for _, p := range ports {
			if p > 0 && lport == itoa(p) {
				hit = true
				break
			}
		}
		if !hit {
			continue
		}
		if len(want) == 0 || want[strings.Trim(rhost, "[]")] {
			return true
		}
	}
	return false
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
