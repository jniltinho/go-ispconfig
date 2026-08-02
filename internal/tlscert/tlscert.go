// Package tlscert generates and validates the panel's self-signed TLS
// certificate pair under <config dir>/ssl/ (foundation design D13). Both
// the serve command and the installer's tls-cert step use it, so the
// installer pre-seeds exactly the files serve would generate.
package tlscert

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// Auto-generated certificate file names under <config dir>/ssl/.
const (
	CertFile = "panel.crt"
	KeyFile  = "panel.key"
)

// ValidateCertKey reports whether the PEM cert/key pair on disk loads and
// the leaf certificate is within its validity period. A missing file is
// returned as an os.IsNotExist error.
func ValidateCertKey(certFile, keyFile string) error {
	for _, f := range []string{certFile, keyFile} {
		if _, err := os.Stat(f); err != nil {
			return err
		}
	}
	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return err
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return err
	}
	now := time.Now()
	if now.After(leaf.NotAfter) {
		return fmt.Errorf("certificate expired %s", leaf.NotAfter.Format(time.RFC3339))
	}
	if now.Before(leaf.NotBefore) {
		return fmt.Errorf("certificate not valid before %s", leaf.NotBefore.Format(time.RFC3339))
	}
	return nil
}

// WriteSelfSigned generates an ECDSA P-256 self-signed server certificate
// valid between notBefore and notAfter, with CN = hostname and SANs
// covering hostname, localhost and every detected host IP, writing both
// PEM files with 0600 permissions (parent directory created 0700). An
// empty hostname falls back to os.Hostname, then "localhost".
func WriteSelfSigned(certFile, keyFile, hostname string, notBefore, notAfter time.Time) error {
	if hostname == "" {
		h, err := os.Hostname()
		if err != nil || h == "" {
			h = "localhost"
		}
		hostname = h
	}
	ips := HostIPs()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generating TLS key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return fmt.Errorf("generating certificate serial: %w", err)
	}
	tpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: hostname, Organization: []string{"go-ispconfig"}},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{hostname, "localhost"},
		IPAddresses:           ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, &tpl, &tpl, &key.PublicKey, key)
	if err != nil {
		return fmt.Errorf("creating self-signed certificate: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return fmt.Errorf("encoding TLS key: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(certFile), 0o700); err != nil {
		return fmt.Errorf("creating ssl directory: %w", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(certFile, certPEM, 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", certFile, err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", keyFile, err)
	}
	return nil
}

// HostIPs returns every global and loopback unicast IP of the host, always
// including 127.0.0.1 and ::1 so https://127.0.0.1 verifies too.
func HostIPs() []net.IP {
	ips := []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ips
	}
	for _, a := range addrs {
		ipNet, ok := a.(*net.IPNet)
		if !ok || ipNet.IP.IsLoopback() || ipNet.IP.IsLinkLocalUnicast() {
			continue
		}
		ips = append(ips, ipNet.IP)
	}
	return ips
}
