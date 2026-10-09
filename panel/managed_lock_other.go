//go:build !linux

package main

import (
	"fmt"
	"os"
	"time"
)

// Development hosts use an exclusive lock file. Production Linux uses flock,
// which releases even after SIGKILL. Never deploy this fallback on a VPS.
func lockPeers() (func(), error) {
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return nil, err
	}
	path := peersFile() + ".dev-lock"
	deadline := time.Now().Add(30 * time.Second)
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err == nil {
			_ = f.Close()
			return func() { _ = os.Remove(path) }, nil
		}
		if !os.IsExist(err) || time.Now().After(deadline) {
			return nil, fmt.Errorf("peer registry lock: %w", err)
		}
		time.Sleep(25 * time.Millisecond)
	}
}
