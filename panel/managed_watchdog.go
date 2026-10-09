package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type managedHealth struct {
	Failures      int   `json:"failures"`
	Restarts      int   `json:"restarts"`
	NextRestart   int64 `json:"next_restart"`
	ProcessActive bool  `json:"process_active"`
	LastCheck     int64 `json:"last_check"`
}

func managedBackoff(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	if attempt > 6 {
		attempt = 6
	}
	return time.Duration(15*(1<<attempt)) * time.Second
}

func managedHealthCheck() (map[int]managedHealth, error) {
	unlock, err := lockPeers()
	if err != nil {
		return nil, err
	}
	defer unlock()
	peers, err := readPeerRegistry()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(configDir, "managed-health.json")
	states := map[int]managedHealth{}
	if data, err := os.ReadFile(path); err == nil {
		if err = json.Unmarshal(data, &states); err != nil {
			return nil, fmt.Errorf("invalid peer health state")
		}
	}
	policy := loadWatchdogConfig()
	now := time.Now().Unix()
	for _, p := range peers {
		if !p.Managed || p.Disabled {
			continue
		}
		state := states[p.ID]
		state.LastCheck = now
		_, err := managedRun("systemctl", "is-active", "--quiet", p.FrpsSvc+".service")
		state.ProcessActive = err == nil
		if state.ProcessActive {
			state.Failures = 0
			state.Restarts = 0
			state.NextRestart = 0
		} else {
			state.Failures++
			if policy.Enabled && policy.AutoRestart && state.Failures >= policy.FailThreshold && now >= state.NextRestart && state.Restarts < 5 {
				_, _ = managedRun("systemctl", "restart", p.GreIf+".service", p.FrpsSvc+".service")
				state.NextRestart = now + int64(managedBackoff(state.Restarts)/time.Second)
				state.Restarts++
			}
		}
		states[p.ID] = state
	}
	data, err := json.Marshal(states)
	if err != nil {
		return nil, err
	}
	return states, atomicPrivateFile(path, data, 0600)
}

func startManagedWatchdog() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		_, _ = managedHealthCheck()
	}
}
