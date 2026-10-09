package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// The registry lock is held by the caller. Control operations have a durable
// journal just like configuration changes; a failed enable cannot leave the
// registry claiming a disabled peer while its services keep running.
func managedControl(op string, previous peerRecord, peers []peerRecord) (p peerRecord, err error) {
	p = previous
	p.OperationID = randomToken(32)
	if op == "restart" && p.Disabled {
		return p, fmt.Errorf("peer is disabled; enable it first")
	}
	p.LastVerified, p.AppliedRevision = 0, 0
	p.State = "PENDING"
	if op == "disable" {
		p.Disabled, p.State = true, "DISABLED"
	} else if op == "enable" {
		p.Disabled = false
	}
	_, activeErr := managedRun("systemctl", "is-active", "--quiet", previous.FrpsSvc+".service")
	s := managedSnapshot{Peer: p, Previous: &previous, Control: true, Active: activeErr == nil, Files: map[string][]byte{}}
	data, err := json.Marshal(s)
	if err != nil {
		return p, err
	}
	if err = atomicPrivateFile(managedJournal(p), data, 0600); err != nil {
		return p, err
	}
	defer func() {
		if err != nil {
			if rollbackErr := s.restore(); rollbackErr != nil {
				err = fmt.Errorf("%w; %v", err, rollbackErr)
				return
			}
		}
		_ = os.Remove(managedJournal(p))
	}()
	verb := "restart"
	if op == "disable" {
		verb = "stop"
	}
	if _, err = managedRun("systemctl", verb, p.GreIf+".service", p.FrpsSvc+".service"); err != nil {
		return p, err
	}
	if op != "restart" {
		if _, err = managedRun("systemctl", op, p.GreIf+".service", p.FrpsSvc+".service"); err != nil {
			return p, err
		}
	}
	for i := range peers {
		if peers[i].ID == p.ID {
			peers[i] = p
		}
	}
	err = writePeerRegistry(peers)
	return p, err
}
