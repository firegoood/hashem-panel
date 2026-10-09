package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// Called by the owned GRE unit on boot. It deliberately does not acquire the
// lifecycle lock: systemctl start runs while that lock is held. The descriptor
// is atomically staged by that transaction and contains only this peer's rules.
func runManagedFirewallCLI(args []string) error {
	if os.Geteuid() != 0 || len(args) != 1 {
		return fmt.Errorf("root and one owned descriptor required")
	}
	data, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	if len(data) > 65536 {
		return fmt.Errorf("invalid firewall descriptor")
	}
	var p peerRecord
	if err = json.Unmarshal(data, &p); err != nil {
		return err
	}
	if !p.Managed {
		return fmt.Errorf("managed peer required")
	}
	if err = validateManagedPeer(p, nil); err != nil {
		return err
	}
	for _, rule := range ownedFirewallRules(p) {
		if firewallCommand("-C", rule) != nil {
			if err = firewallCommand("-I", rule); err != nil {
				return err
			}
		}
	}
	return nil
}
