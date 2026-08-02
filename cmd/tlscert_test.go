package cmd

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/config"
	"go-ispconfig/internal/tlscert"
)

// httpsCfg returns a ServerConfig with HTTPS on and no explicit cert pair.
func httpsCfg() config.ServerConfig {
	return config.ServerConfig{HTTPS: true}
}

// readLeaf parses the first certificate of a PEM file.
func readLeaf(t *testing.T, certFile string) *x509.Certificate {
	t.Helper()
	data, err := os.ReadFile(certFile)
	require.NoError(t, err)
	block, _ := pem.Decode(data)
	require.NotNil(t, block)
	leaf, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)
	return leaf
}

func TestResolveTLSGeneratesAndReuses(t *testing.T) {
	dir := t.TempDir()

	certFile, keyFile, err := resolveTLS(httpsCfg(), dir)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(dir, "ssl", "panel.crt"), certFile)
	require.Equal(t, filepath.Join(dir, "ssl", "panel.key"), keyFile)

	for _, f := range []string{certFile, keyFile} {
		info, err := os.Stat(f)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}

	leaf := readLeaf(t, certFile)
	hostname, _ := os.Hostname()
	require.Contains(t, leaf.DNSNames, hostname)
	require.Contains(t, leaf.DNSNames, "localhost")
	require.WithinDuration(t, time.Now().AddDate(10, 0, 0), leaf.NotAfter, time.Hour)
	_, err = tls.LoadX509KeyPair(certFile, keyFile)
	require.NoError(t, err)

	// Second start reuses the same files untouched and re-asserts the
	// private key mode even when an operator loosened it.
	before, err := os.ReadFile(certFile)
	require.NoError(t, err)
	require.NoError(t, os.Chmod(keyFile, 0o644))
	certFile2, _, err := resolveTLS(httpsCfg(), dir)
	require.NoError(t, err)
	require.Equal(t, certFile, certFile2)
	after, err := os.ReadFile(certFile)
	require.NoError(t, err)
	require.Equal(t, before, after)
	info, err := os.Stat(keyFile)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "reused key must be chmodded back to 0600")
}

func TestResolveTLSRegeneratesExpired(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "ssl", "panel.crt")
	keyFile := filepath.Join(dir, "ssl", "panel.key")
	require.NoError(t, tlscert.WriteSelfSigned(certFile, keyFile, "",
		time.Now().AddDate(-2, 0, 0), time.Now().AddDate(-1, 0, 0)))

	_, _, err := resolveTLS(httpsCfg(), dir)
	require.NoError(t, err)
	leaf := readLeaf(t, certFile)
	require.True(t, leaf.NotAfter.After(time.Now()), "expired cert must be regenerated")
}

func TestResolveTLSRegeneratesUnreadable(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "ssl", "panel.crt")
	require.NoError(t, os.MkdirAll(filepath.Dir(certFile), 0o700))
	require.NoError(t, os.WriteFile(certFile, []byte("not a certificate"), 0o600))

	_, _, err := resolveTLS(httpsCfg(), dir)
	require.NoError(t, err)
	leaf := readLeaf(t, certFile)
	require.True(t, leaf.NotAfter.After(time.Now()))
}

func TestResolveTLSHTTPOptOut(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile, err := resolveTLS(config.ServerConfig{HTTPS: false}, dir)
	require.NoError(t, err)
	require.Empty(t, certFile)
	require.Empty(t, keyFile)
	_, err = os.Stat(filepath.Join(dir, "ssl"))
	require.True(t, os.IsNotExist(err), "opt-out must not generate certificates")
}

func TestResolveTLSExplicitInvalidCertErrors(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "given.crt")
	keyFile := filepath.Join(dir, "given.key")
	require.NoError(t, os.WriteFile(certFile, []byte("garbage"), 0o600))
	require.NoError(t, os.WriteFile(keyFile, []byte("garbage"), 0o600))

	cfg := config.ServerConfig{HTTPS: true, TLSCert: certFile, TLSKey: keyFile}
	_, _, err := resolveTLS(cfg, dir)
	require.ErrorContains(t, err, "never overwrites")

	// The invalid files must remain untouched.
	data, err := os.ReadFile(certFile)
	require.NoError(t, err)
	require.Equal(t, []byte("garbage"), data)
}

func TestResolveTLSExplicitExpiredCertErrors(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "given.crt")
	keyFile := filepath.Join(dir, "given.key")
	require.NoError(t, tlscert.WriteSelfSigned(certFile, keyFile, "",
		time.Now().AddDate(-2, 0, 0), time.Now().AddDate(-1, 0, 0)))

	cfg := config.ServerConfig{HTTPS: true, TLSCert: certFile, TLSKey: keyFile}
	_, _, err := resolveTLS(cfg, dir)
	require.ErrorContains(t, err, "expired")
}

func TestResolveTLSHalfConfiguredPairErrors(t *testing.T) {
	cfg := config.ServerConfig{HTTPS: true, TLSCert: "/some/cert.pem"}
	_, _, err := resolveTLS(cfg, t.TempDir())
	require.ErrorContains(t, err, "must both be set")
}
