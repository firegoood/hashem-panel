package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func legacyConfigPath(p peerRecord) string {
	if p.ID == 1 && p.FrpsSvc == "frps" {
		return "/etc/frp/frps.toml"
	}
	return fmt.Sprintf("/etc/frp/frps-%d.toml", p.ID)
}

func validateLegacyNames(p peerRecord) error {
	if p.ID < 1 || p.ID > 5 {
		return fmt.Errorf("invalid legacy peer id")
	}
	gif, svc := fmt.Sprintf("gre-t%d", p.ID), fmt.Sprintf("frps-%d", p.ID)
	if p.ID == 1 {
		gif, svc = "gre-tunnel", "frps"
	}
	if (p.GreIf != gif || p.FrpsSvc != svc) && !(p.ID == 1 && p.GreIf == "gre-t1" && p.FrpsSvc == "frps-1") {
		return fmt.Errorf("legacy resource names do not establish ownership")
	}
	return nil
}

func validateLegacyOwnership(p peerRecord) error {
	if p.Carrier != "" && p.Carrier != "direct" {
		return fmt.Errorf("alternate legacy carriers require their own verified migration; refusing a silent switch to direct GRE")
	}
	if err := validateLegacyNames(p); err != nil {
		return err
	}
	data, err := os.ReadFile(legacyConfigPath(p))
	if err != nil {
		return fmt.Errorf("cannot inspect owned legacy FRPS configuration")
	}
	token := ""
	for _, line := range strings.Split(string(data), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(k) == "auth.token" {
			s, e := strconv.Unquote(strings.TrimSpace(v))
			if e == nil {
				token = s
			}
		}
	}
	if token == "" || token != p.Token {
		return fmt.Errorf("legacy FRPS credentials do not match the registry; migration refused")
	}
	unit, err := os.ReadFile(filepath.Join(managedUnitDir, p.GreIf+".service"))
	if err != nil {
		return fmt.Errorf("cannot inspect owned legacy GRE unit")
	}
	if !strings.Contains(string(unit), "local "+p.LocalPub) || !strings.Contains(string(unit), "remote "+p.RemotePub) {
		return fmt.Errorf("legacy GRE unit does not match registry ownership")
	}
	return nil
}

// Called with the registry lock held. No first-peer global teardown.
func removeLegacyPeer(p peerRecord) error {
	if err := validateLegacyOwnership(p); err != nil {
		return err
	}
	peers, err := readPeerRegistry()
	if err != nil {
		return err
	}
	p.Managed, p.Legacy, p.Role = true, true, "iran"
	return removeManagedPeer(p, peers)
}
