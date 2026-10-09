package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

func runPasswordCLI(op string) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("root is required")
	}
	data, err := io.ReadAll(io.LimitReader(os.Stdin, 4098))
	if err != nil {
		return err
	}
	password := strings.TrimSuffix(string(data), "\n")
	if strings.ContainsAny(password, "\r\n\x00") || len(password) > 4096 {
		return fmt.Errorf("invalid password input")
	}
	if err = ValidatePasswordStrength(password); err != nil {
		return err
	}
	if op == "init-panel" {
		if _, err = os.Stat(cfgPath()); err == nil {
			return nil
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	if op == "hash-password" {
		fmt.Println(hash)
		return nil
	}
	data, err = os.ReadFile(cfgPath())
	if err == nil {
		if err = json.Unmarshal(data, &cfg); err != nil {
			return err
		}
	} else if os.IsNotExist(err) {
		cfg = panelConfig{Username: "admin", Port: 7777, BasePath: randomBase(12)}
	} else {
		return err
	}
	cfg.PassHash = hash
	if err = atomicPrivateFile(cfgPath(), mustJSON(cfg), 0600); err != nil {
		return err
	}
	return nil
}
