package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Accept the string written by older Shell Backhaul code without losing the
// other records. Invalid records are reported instead of being silently erased.
func (p *peerRecord) UnmarshalJSON(data []byte) error {
	type plain peerRecord
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	raw := fields["raw_ports"]
	delete(fields, "raw_ports")
	rest, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(rest, (*plain)(p)); err != nil {
		return err
	}
	if len(raw) > 0 && string(raw) != "null" {
		if err = json.Unmarshal(raw, &p.RawPorts); err != nil {
			var text string
			if err = json.Unmarshal(raw, &text); err != nil {
				return fmt.Errorf("raw_ports must be a string or string array")
			}
			p.RawPorts = strings.FieldsFunc(text, func(r rune) bool { return r == ',' || r == ' ' })
		}
	}
	return nil
}

func readPeerRegistry() ([]peerRecord, error) {
	data, err := os.ReadFile(peersFile())
	if os.IsNotExist(err) {
		return []peerRecord{}, nil
	}
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Peers         []json.RawMessage `json:"peers"`
		SchemaVersion int               `json:"schema_version"`
	}
	if err = json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("invalid peers.json: %w", err)
	}
	if envelope.SchemaVersion > 2 || envelope.Peers == nil {
		return nil, fmt.Errorf("unsupported or missing peers registry schema")
	}
	var out []peerRecord
	var problems []string
	seen := map[int]bool{}
	for i, raw := range envelope.Peers {
		var p peerRecord
		if err := json.Unmarshal(raw, &p); err != nil {
			problems = append(problems, fmt.Sprintf("record %d: %v", i, err))
			continue
		}
		if p.ID < 1 || seen[p.ID] {
			problems = append(problems, fmt.Sprintf("record %d: invalid/duplicate peer id", i))
			continue
		}
		seen[p.ID] = true
		out = append(out, p)
	}
	if len(problems) > 0 {
		return out, fmt.Errorf("registry requires repair: %s", strings.Join(problems, "; "))
	}
	return out, nil
}

// A unique same-directory temporary file and fsync protect readers from partial
// writes. All peer mutators hold the OS lock for read/validate/apply/commit.
func atomicPrivateFile(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".hashem-stage-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	if dir, err := os.Open(filepath.Dir(path)); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}

func writePeerRegistry(peers []peerRecord) error {
	data, err := json.MarshalIndent(map[string]any{"schema_version": 2, "peers": peers}, "", "  ")
	if err != nil {
		return err
	}
	return atomicPrivateFile(peersFile(), append(data, '\n'), 0600)
}
