//go:build linux

package main

import (
	"fmt"
	"os"
	"syscall"
	"time"
)

func lockPeers() (func(), error) {
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(peersFile()+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
			break
		}
		if err != syscall.EWOULDBLOCK || time.Now().After(deadline) {
			_ = f.Close()
			return nil, fmt.Errorf("peer registry lock: %w", err)
		}
		time.Sleep(25 * time.Millisecond)
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
}
