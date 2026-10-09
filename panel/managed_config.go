package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var managedUnitDir = "/etc/systemd/system"
var managedBinaryDir = "/usr/local/lib/hashem/frp-0.71.0"

func managedDir(id int) string              { return filepath.Join(configDir, "managed", strconv.Itoa(id)) }
func managedConfigPath(p peerRecord) string { return filepath.Join(managedDir(p.ID), "frp.toml") }
func managedFRPBinary(p peerRecord) string {
	if p.Role == "foreign" {
		return filepath.Join(managedBinaryDir, "frpc")
	}
	return filepath.Join(managedBinaryDir, "frps")
}
func managedNames(p *peerRecord) {
	if p.Legacy && p.Role == "iran" {
		if validateLegacyNames(*p) == nil {
			return
		}
		p.GreIf = fmt.Sprintf("gre-t%d", p.ID)
		p.FrpsSvc = fmt.Sprintf("frps-%d", p.ID)
		if p.ID == 1 {
			p.GreIf = "gre-tunnel"
			p.FrpsSvc = "frps"
		}
		return
	}
	p.GreIf = fmt.Sprintf("hsh-gre-%d", p.ID)
	p.FrpsSvc = fmt.Sprintf("hsh-frps-%d", p.ID)
	if p.Role == "foreign" {
		p.FrpsSvc = fmt.Sprintf("hsh-frpc-%d", p.ID)
	}
}

func makePeerCertificate(ip string) ([]byte, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, err
	}
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "Hashem peer"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(5, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true, IsCA: true, IPAddresses: []net.IP{net.ParseIP(ip)}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), nil
}

func ensureManagementCertificate() (string, error) {
	cert, certErr := os.ReadFile(tlsCertFile())
	key, keyErr := os.ReadFile(tlsKeyFile())
	if os.IsNotExist(certErr) && os.IsNotExist(keyErr) {
		var err error
		cert, key, err = makePeerCertificate("127.0.0.1")
		if err != nil {
			return "", err
		}
		if err = atomicPrivateFile(tlsKeyFile(), key, 0600); err != nil {
			return "", err
		}
		if err = atomicPrivateFile(tlsCertFile(), cert, 0600); err != nil {
			return "", err
		}
	} else if certErr != nil || keyErr != nil {
		return "", fmt.Errorf("management certificate is incomplete")
	}
	pair, err := tls.X509KeyPair(cert, key)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(pair.Certificate[0])
	return hex.EncodeToString(sum[:]), nil
}

func managedFRPConfig(p peerRecord) ([]byte, error) {
	ms, err := peerMappings(p)
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	q := strconv.Quote
	dir := managedDir(p.ID)
	if p.Role == "foreign" {
		b.WriteString("transport.tcpMux = true\ntransport.tcpMuxKeepaliveInterval = 10\ntransport.heartbeatInterval = 10\ntransport.heartbeatTimeout = 45\n")
		fmt.Fprintf(&b, "webServer.addr = \"127.0.0.1\"\nwebServer.port = %d\nwebServer.user = \"hashem\"\nwebServer.password = %s\n", 27100+p.ID, q(p.ManagementSecret))
		fmt.Fprintf(&b, "serverAddr = %s\nserverPort = %d\nloginFailExit = false\ntransport.protocol = %s\ntransport.connectServerLocalIP = %s\ntransport.tls.enable = true\ntransport.tls.trustedCaFile = %s\ntransport.tls.serverName = %s\n", q(p.PeerGre), p.FrpPort, q(p.FRPTransport), q(p.LocalGre), q(filepath.Join(dir, "ca.crt")), q(p.PeerGre))
		fmt.Fprintf(&b, "auth.method = \"token\"\nauth.token = %s\nauth.additionalScopes = [\"HeartBeats\", \"NewWorkConns\"]\n", q(p.Token))
		for _, m := range ms {
			fmt.Fprintf(&b, "\n[[proxies]]\nname = \"peer%d_%s_%d\"\ntype = %s\nlocalIP = \"127.0.0.1\"\nlocalPort = %d\nremotePort = %d\ntransport.useEncryption = %t\ntransport.useCompression = %t\n", p.ID, m.Protocol, m.Public, q(m.Protocol), m.Local, m.Public, p.UseEncryption, p.UseCompression)
			if p.ProxyProtocol != "" && p.ProxyProtocol != "off" && m.Protocol == "tcp" {
				fmt.Fprintf(&b, "transport.proxyProtocolVersion = %s\n", q(p.ProxyProtocol))
			}
		}
	} else {
		b.WriteString("transport.tcpMuxKeepaliveInterval = 10\ntransport.heartbeatTimeout = 45\n")
		// Keep TCP and KCP available on GRE during a revision transition. The
		// worker chooses the actual transport; neither control listener is public.
		fmt.Fprintf(&b, "bindAddr = %s\nproxyBindAddr = \"0.0.0.0\"\nbindPort = %d\nkcpBindPort = %d\ntransport.tls.force = true\ntransport.tls.certFile = %s\ntransport.tls.keyFile = %s\n", q(p.LocalGre), p.FrpPort, p.FrpPort, q(filepath.Join(dir, "server.crt")), q(filepath.Join(dir, "server.key")))
		fmt.Fprintf(&b, "auth.method = \"token\"\nauth.token = %s\nauth.additionalScopes = [\"HeartBeats\", \"NewWorkConns\"]\n", q(p.Token))
		var allowed []string
		seen := map[int]bool{}
		for _, m := range ms {
			if !seen[m.Public] {
				allowed = append(allowed, fmt.Sprintf("{ single = %d }", m.Public))
				seen[m.Public] = true
			}
		}
		fmt.Fprintf(&b, "allowPorts = [%s]\n", strings.Join(allowed, ", "))
	}
	return []byte(b.String()), nil
}

func managedUnits(p peerRecord) map[string][]byte {
	gre := fmt.Sprintf(`[Unit]
Description=Hashem owned GRE peer %d
After=network-online.target
Wants=network-online.target
StartLimitIntervalSec=120
StartLimitBurst=5
[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/sbin/ip tunnel add %s mode gre local %s remote %s ttl 64
ExecStart=/usr/sbin/ip address add %s/30 dev %s
ExecStart=/usr/sbin/ip link set %s mtu 1380 up
ExecStartPost=/usr/local/bin/gre-panel peer-firewall %s
ExecStop=/usr/sbin/ip link del %s
[Install]
WantedBy=multi-user.target
`, p.ID, p.GreIf, p.LocalPub, p.RemotePub, p.LocalGre, p.GreIf, p.GreIf, filepath.Join(managedDir(p.ID), "peer.json"), p.GreIf)
	frp := fmt.Sprintf(`[Unit]
Description=Hashem owned FRP peer %d
After=network-online.target %s.service
Requires=%s.service
StartLimitIntervalSec=120
StartLimitBurst=5
[Service]
Type=simple
ExecStart=%s -c %s
Restart=on-failure
RestartSec=5
LimitNOFILE=65536
UMask=0077
NoNewPrivileges=yes
[Install]
WantedBy=multi-user.target
`, p.ID, p.GreIf, p.GreIf, managedFRPBinary(p), managedConfigPath(p))
	return map[string][]byte{filepath.Join(managedUnitDir, p.GreIf+".service"): []byte(gre), filepath.Join(managedUnitDir, p.FrpsSvc+".service"): []byte(frp)}
}
