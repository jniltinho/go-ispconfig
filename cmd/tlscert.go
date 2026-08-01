package cmd

// TLS certificate resolution for the standalone HTTPS panel (design D13):
// serve terminates TLS itself. An explicitly configured cert/key pair is
// used as-is (and never touched on disk); without one a 10-year self-signed
// certificate is generated into <config dir>/ssl/ and reused across starts,
// regenerated only when missing, unreadable or expired.

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/viper"

	"go-ispconfig/internal/config"
)

// Auto-generated certificate file names under <config dir>/ssl/.
const (
	panelCertFile = "panel.crt"
	panelKeyFile  = "panel.key"
)

// configDir returns the directory holding the loaded config.toml, or "."
// when serve runs on defaults without any config file. The auto-generated
// ssl/ directory lives next to the config file (design D13).
func configDir() string {
	if f := viper.ConfigFileUsed(); f != "" {
		return filepath.Dir(f)
	}
	return "."
}

// resolveTLS decides how serve terminates TLS. It returns the cert/key file
// pair to serve HTTPS with, or empty strings for plain HTTP (server.https =
// false). A configured pair is validated but never modified: an invalid or
// expired explicit certificate is a startup error. Without a configured
// pair, a 10-year self-signed certificate under dir/ssl/ is reused when
// still valid and (re)generated otherwise.
func resolveTLS(srv config.ServerConfig, dir string) (certFile, keyFile string, err error) {
	if !srv.HTTPS {
		return "", "", nil
	}
	if (srv.TLSCert == "") != (srv.TLSKey == "") {
		return "", "", fmt.Errorf("config: server.tls_cert and server.tls_key must both be set (got cert=%q key=%q)",
			srv.TLSCert, srv.TLSKey)
	}
	if srv.TLSCert != "" {
		if err := validateCertKey(srv.TLSCert, srv.TLSKey); err != nil {
			return "", "", fmt.Errorf("configured server.tls_cert/tls_key are invalid: %w; "+
				"fix or remove them from the config — go-ispconfig never overwrites an explicitly configured certificate", err)
		}
		return srv.TLSCert, srv.TLSKey, nil
	}

	certFile = filepath.Join(dir, "ssl", panelCertFile)
	keyFile = filepath.Join(dir, "ssl", panelKeyFile)
	if err := validateCertKey(certFile, keyFile); err == nil {
		// Re-assert the private key mode on reuse: a key loosened by an
		// operator (or created by an older build) must not stay readable.
		if err := os.Chmod(keyFile, 0o600); err != nil {
			return "", "", fmt.Errorf("restricting permissions of %s: %w", keyFile, err)
		}
		slog.Info("reusing self-signed certificate", "cert", certFile)
		return certFile, keyFile, nil
	} else if !os.IsNotExist(err) {
		slog.Warn("self-signed certificate invalid, regenerating", "cert", certFile, "error", err)
	}
	if err := writeSelfSigned(certFile, keyFile, time.Now().Add(-time.Hour), time.Now().AddDate(10, 0, 0)); err != nil {
		return "", "", err
	}
	slog.Info("generated 10-year self-signed certificate", "cert", certFile)
	return certFile, keyFile, nil
}

// validateCertKey reports whether the PEM cert/key pair on disk loads and
// the leaf certificate is within its validity period. A missing file is
// returned as an os.IsNotExist error.
func validateCertKey(certFile, keyFile string) error {
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

// writeSelfSigned generates an ECDSA P-256 self-signed server certificate
// valid between notBefore and notAfter, with CN and SANs covering the
// hostname and every detected host IP, writing both PEM files with 0600
// permissions (parent directory created 0700).
func writeSelfSigned(certFile, keyFile string, notBefore, notAfter time.Time) error {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "localhost"
	}
	ips := hostIPs()

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

// hostIPs returns every global and loopback unicast IP of the host, always
// including 127.0.0.1 and ::1 so https://127.0.0.1 verifies too.
func hostIPs() []net.IP {
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
