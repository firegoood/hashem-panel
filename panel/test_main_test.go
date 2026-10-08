package main

import (
	"fmt"
	"os"
	"testing"
)

// TestMain points configDir at a throwaway directory for the whole run so no
// test can read or write the real /etc/gre-panel (error journal, audit log,
// peers, sessions, ...).
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "gre-panel-test-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot create test configDir:", err)
		os.Exit(1)
	}
	configDir = dir
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
