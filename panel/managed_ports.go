package main

import (
	"fmt"
	"strconv"
	"strings"
)

// A mapping owns one public listener, independently for TCP and UDP. Unqualified
// legacy input keeps both protocols, as the original installer did.
type portMapping struct {
	Protocol string `json:"protocol"`
	Public   int    `json:"public"`
	Local    int    `json:"local"`
}

func normalizedPorts(raw []string) ([]portMapping, error) {
	var out []portMapping
	seen := map[string]bool{}
	for _, group := range raw {
		for _, part := range strings.Split(group, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				return nil, fmt.Errorf("empty port entry")
			}
			protocols := []string{"tcp", "udp"}
			if proto, value, ok := strings.Cut(part, ":"); ok {
				if proto != "tcp" && proto != "udp" {
					return nil, fmt.Errorf("unsupported port protocol")
				}
				protocols, part = []string{proto}, value
			}
			public, local, mapped := strings.Cut(part, "=")
			if !mapped {
				local = public
			}
			parse := func(s string) (int, int, error) {
				a, b, ranged := strings.Cut(s, "-")
				lo, err := strconv.Atoi(a)
				if err != nil || lo < 1 || lo > 65535 {
					return 0, 0, fmt.Errorf("invalid port")
				}
				hi := lo
				if ranged {
					hi, err = strconv.Atoi(b)
				}
				if err != nil || hi < lo || hi > 65535 {
					return 0, 0, fmt.Errorf("invalid port range")
				}
				return lo, hi, nil
			}
			pl, ph, err := parse(public)
			if err != nil {
				return nil, err
			}
			ll, lh, err := parse(local)
			if err != nil {
				return nil, err
			}
			if ph-pl != lh-ll {
				return nil, fmt.Errorf("mapped ranges must have equal lengths")
			}
			if ph-pl > 1023 || len(out)+(ph-pl+1)*len(protocols) > 2048 {
				return nil, fmt.Errorf("too many port mappings (maximum 2048)")
			}
			for p := pl; p <= ph; p++ {
				for _, proto := range protocols {
					key := fmt.Sprintf("%s:%d", proto, p)
					if seen[key] {
						return nil, fmt.Errorf("duplicate public listener %s", key)
					}
					seen[key] = true
					out = append(out, portMapping{proto, p, ll + p - pl})
				}
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("ports list is empty")
	}
	return out, nil
}

func peerMappings(p peerRecord) ([]portMapping, error) {
	raw := p.RawPorts
	if len(raw) == 0 {
		for _, port := range p.Ports {
			raw = append(raw, strconv.Itoa(port))
		}
	}
	return normalizedPorts(raw)
}

func canonicalMappings(ms []portMapping) []string {
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, fmt.Sprintf("%s:%d=%d", m.Protocol, m.Public, m.Local))
	}
	return out
}
