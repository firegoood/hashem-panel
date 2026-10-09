package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
)

func publicManagedPeer(p peerRecord) peerRecord {
	p.Token = ""
	p.ManagementSecret = ""
	p.ServerCA = ""
	p.MasterPin = ""
	return p
}

func managedSetup(b setupRequest) (peerRecord, string, error) {
	p := peerRecord{Name: b.Name, LocalPub: b.LocalPub, RemotePub: b.RemotePub, LocalGre: b.LocalGre, PeerGre: b.PeerGre, FrpPort: b.FrpPort, Token: b.Token, FRPTransport: b.FRPTransport, Role: "iran", RawPorts: []string{b.Ports}, ProxyProtocol: b.ProxyProtocol, UseEncryption: b.UseEncryption, UseCompression: b.UseCompression}
	if b.Role == "foreign" {
		s := b.OrigBundle
		if s == "" {
			s = b.Token
		}
		master, err := parseManagedBundle(s)
		if err != nil {
			return p, "", fmt.Errorf("foreign setup requires a secure hsh2 pairing bundle: %w", err)
		}
		p = workerView(master)
		if b.LocalPub != "" && b.LocalPub != p.LocalPub {
			return p, "", fmt.Errorf("local public endpoint does not match the pairing bundle")
		}
		// Fetch the authenticated current revision before creating any resources.
		bootstrap := p
		u, err := url.Parse(p.MasterURL)
		if err != nil {
			return p, "", err
		}
		u.Host = net.JoinHostPort(p.RemotePub, u.Port())
		bootstrap.MasterURL = u.String()
		data, err := managedRequest(bootstrap, "GET", nil)
		if err != nil {
			return p, "", err
		}
		var desired peerRecord
		if err = json.Unmarshal(data, &desired); err != nil {
			return p, "", err
		}
		if desired.ManagementSecret != p.ManagementSecret || desired.MasterPin != p.MasterPin || desired.ID != p.ID || desired.Revision < p.Revision {
			return p, "", fmt.Errorf("pairing bundle no longer matches the central peer")
		}
		p = workerView(desired)
	}
	operation := "add"
	if p.Role == "foreign" && b.Force && findPeer(p.ID) != nil {
		operation = "update"
	}
	if p.Role == "iran" {
		for _, existing := range loadPeers() {
			if existing.Managed && existing.Role == "iran" && existing.RemotePub == p.RemotePub && existing.LocalGre == p.LocalGre {
				p.ID = existing.ID
			}
		}
	}
	result, err := managedPeerMutation(operation, p)
	if err != nil {
		return result, "", err
	}
	if result.Role == "iran" {
		bundle, err := makeManagedBundle(result)
		return result, bundle, err
	}
	_ = reconcileManagedPeer(result)
	return result, "", nil
}

func handleManagedSetup(w http.ResponseWriter, r *http.Request, b setupRequest) {
	w.Header().Set("Cache-Control", "no-store")
	if b.Role != "iran" && b.Role != "foreign" && b.Role != "add-peer" {
		writeAPIError(w, r, "E-SETUP-02", "")
		return
	}
	p, bundle, err := managedSetup(b)
	if err != nil {
		writeAPIError(w, r, "E-INSTALL-02", err.Error())
		return
	}
	// The panel starts its TLS listener before serving requests. Restarting that
	// listener here would close the very HTTPS connection creating this peer.
	writeJSON(w, map[string]any{"status": "pending", "state": p.State, "peer": publicManagedPeer(p), "bundle": bundle, "steps": []string{"configuration validated and local resources activated", "remote registration and application forwarding require verification"}})
}

func applyManagedPatch(p peerRecord, b peerPatchRequest) (peerRecord, error) {
	if b.Engine != "" && b.Engine != "frp" {
		return p, fmt.Errorf("engine changes require explicit migration")
	}
	if b.Carrier != "" && b.Carrier != "direct" {
		return p, fmt.Errorf("this peer uses the direct GRE carrier; alternate carrier migration is not supported")
	}
	if b.Transport != "" && b.FRPTransport == "" {
		b.FRPTransport = b.Transport
	}
	if b.Name != "" {
		p.Name = b.Name
	}
	if b.RemotePub != "" {
		p.RemotePub = b.RemotePub
	}
	if b.FRPTransport != "" {
		p.FRPTransport = b.FRPTransport
	}
	if b.ProxyProtocol != "" {
		p.ProxyProtocol = b.ProxyProtocol
	}
	if b.UseEncryption != nil {
		p.UseEncryption = *b.UseEncryption
	}
	if b.UseCompression != nil {
		p.UseCompression = *b.UseCompression
	}
	if b.RawPorts != nil {
		p.RawPorts = *b.RawPorts
	} else if b.Ports != nil {
		p.RawPorts = nil
		p.Ports = *b.Ports
	}
	if p.Role == "foreign" {
		return p, fmt.Errorf("edit desired configuration from the central panel")
	}
	return managedPeerMutation("update", p)
}

// Native CLI and WebUI call these same functions. JSON via stdin keeps secrets
// out of subprocess arguments. Interactive CLI flags remain accepted for
// compatibility, but secure automation should use --request-file -.
func runManagedCLI(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: gre-panel peer-manage COMMAND [--request-file - | flags]")
	}
	command := args[0]
	var b setupRequest
	var patch peerPatchRequest
	id := 0
	input := ""
	bundle := ""
	backupPath := ""
	dryRun := false
	for i := 1; i < len(args); i++ {
		flag := args[i]
		if flag == "--dry-run" {
			dryRun = true
			continue
		}
		if flag == "--force" {
			b.Force = true
			continue
		}
		if flag == "--encrypt" {
			v := true
			patch.UseEncryption = &v
			b.UseEncryption = true
			continue
		}
		if flag == "--compress" {
			v := true
			patch.UseCompression = &v
			b.UseCompression = true
			continue
		}
		if flag == "--help" || flag == "-h" {
			fmt.Println("peer-manage add-peer|setup-iran|setup-foreign|edit-peer|remove-peer|disable-peer|enable-peer|restart-peer|peer-list|peer-token|reconcile|recover; secure input: --request-file -")
			return nil
		}
		if i+1 >= len(args) {
			return fmt.Errorf("missing value for %s", flag)
		}
		i++
		value := args[i]
		switch flag {
		case "--file":
			backupPath = value
		case "--id":
			n, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("invalid peer id")
			}
			id = n
		case "--name":
			b.Name = value
			patch.Name = value
		case "--local-pub":
			b.LocalPub = value
		case "--remote-pub":
			b.RemotePub = value
			patch.RemotePub = value
		case "--local-gre":
			b.LocalGre = value
		case "--peer-gre":
			b.PeerGre = value
		case "--frp-port":
			n, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("invalid control port")
			}
			b.FrpPort = n
		case "--token":
			b.Token = value
		case "--bundle":
			bundle = value
			b.Token = value
		case "--ports":
			b.Ports = value
			raw := []string{value}
			patch.RawPorts = &raw
		case "--frp-transport":
			b.FRPTransport = value
			patch.FRPTransport = value
		case "--carrier":
			if (command == "add-peer" || command == "setup-iran" || command == "setup-foreign") && value != "direct" {
				return fmt.Errorf("managed setup supports direct GRE; alternate carrier migration is not supported")
			}
			patch.Carrier = value
		case "--proxy-protocol":
			b.ProxyProtocol = value
			patch.ProxyProtocol = value
		case "--frp-encryption", "--frp-compression":
			v := value == "on" || value == "true"
			if value != "on" && value != "off" && value != "true" && value != "false" {
				return fmt.Errorf("invalid boolean flag")
			}
			if flag == "--frp-encryption" {
				patch.UseEncryption = &v
				b.UseEncryption = v
			} else {
				patch.UseCompression = &v
				b.UseCompression = v
			}
		case "--request-file":
			input = value
		case "--chaff":
			if value != "off" {
				return fmt.Errorf("chaff is not supported on managed peers")
			}
		default:
			return fmt.Errorf("unknown peer flag %s", flag)
		}
	}
	if input != "" {
		var reader io.Reader = os.Stdin
		if input != "-" {
			f, err := os.Open(input)
			if err != nil {
				return err
			}
			defer f.Close()
			reader = f
		}
		data, err := io.ReadAll(io.LimitReader(reader, 65537))
		if err != nil {
			return err
		}
		if len(data) > 65536 {
			return fmt.Errorf("request too large")
		}
		if command == "edit-peer" || command == "edit-peer-ports" {
			if err = json.Unmarshal(data, &patch); err != nil {
				return err
			}
			id = patch.ID
		} else {
			if err = json.Unmarshal(data, &b); err != nil {
				return err
			}
			var envelope struct {
				ID     int    `json:"id"`
				Bundle string `json:"bundle"`
			}
			if err = json.Unmarshal(data, &envelope); err != nil {
				return err
			}
			id = envelope.ID
			bundle = envelope.Bundle
			if bundle != "" {
				b.Token = bundle
			}
		}
	}
	switch command {
	case "backup", "restore-backup":
		if backupPath == "" {
			return fmt.Errorf("--file is required")
		}
		if command == "backup" {
			return backupManagedPeers(backupPath)
		}
		return restoreManagedPeers(backupPath, dryRun)
	case "remove-all-owned":
		peers, err := readPeerRegistry()
		if err != nil {
			return err
		}
		for _, p := range peers {
			if p.Engine != "" && p.Engine != "frp" {
				return fmt.Errorf("non-FRP registry resources require their engine-specific removal; no global teardown performed")
			}
		}
		for _, p := range peers {
			if _, err = managedPeerMutation("remove", peerRecord{ID: p.ID}); err != nil {
				return err
			}
		}
		return nil
	case "health-check":
		states, err := managedHealthCheck()
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(states)
	case "peer-list":
		peers, err := readPeerRegistry()
		if err != nil {
			return err
		}
		for i := range peers {
			peers[i] = publicManagedPeer(peers[i])
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"peers": peers})
	case "recover":
		unlock, err := lockPeers()
		if err != nil {
			return err
		}
		defer unlock()
		return recoverManagedTransactions()
	case "peer-token":
		p := findPeer(id)
		if p == nil {
			return fmt.Errorf("peer not found")
		}
		s, err := makeManagedBundle(*p)
		if err != nil {
			return err
		}
		fmt.Println(s)
		return nil
	case "edit-peer", "edit-peer-ports":
		p := findPeer(id)
		if p == nil {
			return fmt.Errorf("peer not found")
		}
		result, err := applyManagedPatch(*p, patch)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(publicManagedPeer(result))
	case "remove-peer", "disable-peer", "enable-peer", "restart-peer":
		op := strings.TrimSuffix(command, "-peer")
		_, err := managedPeerMutation(op, peerRecord{ID: id})
		return err
	case "reconcile":
		p := findPeer(id)
		if p == nil || p.Role != "foreign" {
			return fmt.Errorf("foreign managed peer required")
		}
		return reconcileManagedPeer(*p)
	case "add-peer", "setup-iran", "setup-foreign":
		b.Role = "iran"
		if command == "setup-foreign" {
			b.Role = "foreign"
		}
		if bundle != "" {
			b.OrigBundle = bundle
		}
		if id > 0 && b.Role == "iran" {
			p := peerRecord{ID: id, Name: b.Name, LocalPub: b.LocalPub, RemotePub: b.RemotePub, LocalGre: b.LocalGre, PeerGre: b.PeerGre, FrpPort: b.FrpPort, Token: b.Token, FRPTransport: b.FRPTransport, RawPorts: []string{b.Ports}, Role: "iran", ProxyProtocol: b.ProxyProtocol, UseEncryption: b.UseEncryption, UseCompression: b.UseCompression}
			result, err := managedPeerMutation("add", p)
			if err != nil {
				return err
			}
			s, err := makeManagedBundle(result)
			if err != nil {
				return err
			}
			return json.NewEncoder(os.Stdout).Encode(map[string]any{"peer": publicManagedPeer(result), "bundle": s, "status": "pending"})
		}
		result, s, err := managedSetup(b)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"peer": publicManagedPeer(result), "bundle": s, "status": "pending"})
	}
	return fmt.Errorf("unknown peer command")
}
