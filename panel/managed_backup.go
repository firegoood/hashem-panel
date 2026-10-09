package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const managedBackupHeader = "HASHEM-BACKUP-2\n"

type managedBackupData struct {
	Version int            `json:"version"`
	Peers   []peerRecord   `json:"peers"`
	Keys    map[int][]byte `json:"server_keys"`
}

func managedBackupCipher(create bool) (cipher.AEAD, error) {
	path := filepath.Join(configDir, "managed-backup.key")
	key, err := os.ReadFile(path)
	if os.IsNotExist(err) && create {
		key = make([]byte, 32)
		if _, err = rand.Read(key); err == nil {
			err = atomicPrivateFile(path, key, 0600)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("backup key unavailable: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("invalid managed backup key")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// This archive is authenticated encryption, contains no arbitrary filesystem
// paths and excludes its encryption key. Keep that key separately to recover.
func backupManagedPeers(path string) error {
	unlock, err := lockPeers()
	if err != nil {
		return err
	}
	defer unlock()
	if err = recoverManagedTransactions(); err != nil {
		return err
	}
	peers, err := readPeerRegistry()
	if err != nil {
		return err
	}
	archive := managedBackupData{Version: 2, Peers: peers, Keys: map[int][]byte{}}
	for _, p := range peers {
		if !p.Managed {
			return fmt.Errorf("legacy peers require validated migration before this backup")
		}
		if err = validateManagedPeer(p, peers); err != nil {
			return err
		}
		if p.Role == "iran" {
			key, err := os.ReadFile(filepath.Join(managedDir(p.ID), "server.key"))
			if err != nil {
				return err
			}
			archive.Keys[p.ID] = key
		}
	}
	plain, err := json.Marshal(archive)
	if err != nil {
		return err
	}
	aead, err := managedBackupCipher(true)
	if err != nil {
		return err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return err
	}
	sealed := aead.Seal(nonce, nonce, plain, []byte(managedBackupHeader))
	return atomicPrivateFile(path, append([]byte(managedBackupHeader), sealed...), 0600)
}

func restoreManagedPeers(path string, dryRun bool) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(f, 8*1024*1024+1))
	f.Close()
	if err != nil {
		return err
	}
	if len(data) > 8*1024*1024 || len(data) < len(managedBackupHeader) || string(data[:len(managedBackupHeader)]) != managedBackupHeader {
		return fmt.Errorf("unsupported or oversized backup")
	}
	unlock, err := lockPeers()
	if err != nil {
		return err
	}
	defer unlock()
	aead, err := managedBackupCipher(false)
	if err != nil {
		return err
	}
	sealed := data[len(managedBackupHeader):]
	if len(sealed) < aead.NonceSize()+aead.Overhead() {
		return fmt.Errorf("invalid backup")
	}
	plain, err := aead.Open(nil, sealed[:aead.NonceSize()], sealed[aead.NonceSize():], []byte(managedBackupHeader))
	if err != nil {
		return fmt.Errorf("backup authentication failed; no changes made")
	}
	var archive managedBackupData
	if err = json.Unmarshal(plain, &archive); err != nil {
		return err
	}
	if archive.Version != 2 || len(archive.Peers) > 5 {
		return fmt.Errorf("unsupported backup schema")
	}
	if err = recoverManagedTransactions(); err != nil {
		return err
	}
	current, err := readPeerRegistry()
	if err != nil {
		return err
	}
	seen := map[int]bool{}
	for _, p := range archive.Peers {
		if !p.Managed || seen[p.ID] {
			return fmt.Errorf("invalid/duplicate archived peer")
		}
		seen[p.ID] = true
		if err = validateManagedPeer(p, archive.Peers); err != nil {
			return err
		}
		for _, old := range current {
			if old.ID == p.ID && (!old.Managed || old.Role != p.Role || old.LocalPub != p.LocalPub || old.RemotePub != p.RemotePub || old.ManagementSecret != p.ManagementSecret) {
				return fmt.Errorf("backup does not own existing peer %d", p.ID)
			}
		}
		if p.Role == "iran" {
			pair, e := tls.X509KeyPair([]byte(p.ServerCA), archive.Keys[p.ID])
			if e != nil {
				return fmt.Errorf("invalid archived peer certificate")
			}
			cert, e := x509.ParseCertificate(pair.Certificate[0])
			if e != nil || cert.VerifyHostname(p.LocalGre) != nil {
				return fmt.Errorf("archived GRE certificate mismatch")
			}
			pin, e := ensureManagementCertificate()
			if e != nil || pin != p.MasterPin {
				return fmt.Errorf("central TLS identity changed; restore its certificate securely before restoring peers")
			}
		}
	}
	if dryRun {
		return nil
	}
	for _, p := range archive.Peers {
		var previous *peerRecord
		for _, old := range current {
			if old.ID == p.ID {
				copy := old
				previous = &copy
			}
		}
		if p.Role == "iran" && previous != nil {
			p.Revision = previous.Revision + 1
		}
		files := map[string][]byte{}
		if p.Role == "iran" {
			files[filepath.Join(managedDir(p.ID), "server.key")] = archive.Keys[p.ID]
			files[filepath.Join(managedDir(p.ID), "server.crt")] = []byte(p.ServerCA)
		}
		if err = managedApplyFiles(p, previous, current, files); err != nil {
			return fmt.Errorf("peer %d restore failed (previous peers committed individually): %w", p.ID, err)
		}
		current, err = readPeerRegistry()
		if err != nil {
			return err
		}
		if p.Disabled {
			if _, err = managedControl("disable", *findPeer(p.ID), current); err != nil {
				return err
			}
		}
	}
	return nil
}
