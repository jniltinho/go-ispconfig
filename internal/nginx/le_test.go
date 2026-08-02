package nginx

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// leRunner is a fake CommandRunner scripting `which`, `--version` and the
// acme.sh/certbot issue/install calls; it records every argv.
type leRunner struct {
	which   map[string]string // candidate list key -> path output
	version string
	fail    map[string]bool // script/arg0 that should fail
	calls   [][]string
}

func (r *leRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, append([]string{name}, args...))
	if name == "which" {
		return []byte(r.which[args[0]]), nil
	}
	if len(args) > 0 && args[0] == "--version" {
		return []byte("v" + r.version), nil
	}
	if r.fail[name] {
		return []byte("boom"), assertErr("issuance failed")
	}
	return nil, nil
}

type assertErr string

func (e assertErr) Error() string { return string(e) }

// makeExecutable creates an executable stub at path so os.Stat sees the 0111
// bit whichExecutable checks.
func makeExecutable(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755))
}

func lePlugin(t *testing.T, r *leRunner) *Plugin {
	t.Helper()
	p := NewPlugin(nil, nil, r, "", nil)
	p.leWebroot = filepath.Join(t.TempDir(), "acme")
	return p
}

// TestLEDetectsAcmePreferred: acme.sh wins over certbot.
func TestLEDetectsAcmePreferred(t *testing.T) {
	dir := t.TempDir()
	acme := filepath.Join(dir, "acme.sh")
	makeExecutable(t, acme)
	r := &leRunner{which: map[string]string{"acme.sh": acme}, version: "3.0.0"}
	p := lePlugin(t, r)

	c := p.newLEClient(context.Background())
	assert.Equal(t, leAcme, c.kind)
	assert.Equal(t, acme, c.script)
	assert.Equal(t, "3.0.0", c.version)
}

// TestLEFallsBackToCertbot: no acme.sh, certbot present.
func TestLEFallsBackToCertbot(t *testing.T) {
	dir := t.TempDir()
	certbot := filepath.Join(dir, "certbot")
	makeExecutable(t, certbot)
	r := &leRunner{which: map[string]string{"certbot": certbot}, version: "2.9.0"}
	p := lePlugin(t, r)

	c := p.newLEClient(context.Background())
	assert.Equal(t, leCertbot, c.kind)
}

// TestLENoClient: neither found.
func TestLENoClient(t *testing.T) {
	r := &leRunner{which: map[string]string{}}
	c := lePlugin(t, r).newLEClient(context.Background())
	assert.Equal(t, leNone, c.kind)
}

// TestCertTypeVersionGates pins ec-256 vs RSA selection.
func TestCertTypeVersionGates(t *testing.T) {
	assert.Equal(t, "ECDSA", (&leClient{kind: leAcme, version: "3.0.0"}).certType("ECDSA"))
	assert.Equal(t, "RSA", (&leClient{kind: leAcme, version: "2.6.0"}).certType("ECDSA"))
	assert.Equal(t, "ECDSA", (&leClient{kind: leCertbot, version: "2.1"}).certType("ECDSA"))
	assert.Equal(t, "RSA", (&leClient{kind: leCertbot, version: "1.9"}).certType("ECDSA"))
	assert.Equal(t, "RSA", (&leClient{kind: leAcme, version: "3.0.0"}).certType("RSA"))
}

// TestAcmeIssueArgs pins the acme.sh --issue argv.
func TestAcmeIssueArgs(t *testing.T) {
	c := &leClient{webroot: "/w"}
	assert.Equal(t,
		[]string{"--issue", "-d", "example.com", "-d", "www.example.com", "-w", "/w",
			"--always-force-new-domain-key", "--ecc", "--keylength", "ec-256"},
		c.acmeIssueArgs([]string{"example.com", "www.example.com"}, "ECDSA"))
	assert.Contains(t, c.acmeIssueArgs([]string{"a"}, "RSA"), "4096")
}

// TestCertbotArgs pins the modern certbot argv.
func TestCertbotArgs(t *testing.T) {
	c := &leClient{webroot: "/w"}
	got := c.certbotArgs([]string{"example.com", "www.example.com"}, "ECDSA")
	assert.Contains(t, got, "--cert-name")
	assert.Contains(t, got, "example.com_ecc")
	assert.Contains(t, got, "secp256r1")
	assert.Contains(t, got, "webmaster@example.com")
	assert.Subset(t, got, []string{"-d", "example.com", "-d", "www.example.com"})

	rsa := c.certbotArgs([]string{"example.com"}, "RSA")
	assert.Contains(t, rsa, "4096")
	assert.Contains(t, rsa, "example.com") // cert-name without _ecc
}

// TestAssembleDomainsReachability: only domains that echo the challenge token
// survive, and www is added for a www site.
func TestAssembleDomainsReachability(t *testing.T) {
	r := &leRunner{}
	p := lePlugin(t, r)
	c := p.newLEClient(context.Background())
	var probed []string
	c.httpGet = func(url string) (string, error) {
		probed = append(probed, url)
		// www is unreachable in this scenario.
		if filepath.Base(url) == "" {
			return "", assertErr("x")
		}
		// The token file was written; echo its hash back only for the apex.
		hash, _ := os.ReadFile(challengeFileFromURL(t, c.webroot, url))
		if len(url) > 0 && !contains([]string{"www.example.com"}, hostFromURL(url)) {
			return string(hash), nil
		}
		return "unreachable", nil
	}

	d := row{"domain": "example.com", "subdomain": "www", "domain_id": float64(1)}
	got, err := c.assembleDomains(context.Background(), d, "example.com", true)
	require.NoError(t, err)
	assert.Equal(t, []string{"example.com"}, got, "unreachable www dropped")
	assert.NotEmpty(t, probed)
}

// TestRequestCertAcmeInstallsFiles: acme.sh path issues then install-certs
// into the -le files.
func TestRequestCertAcmeInstallsFiles(t *testing.T) {
	dir := t.TempDir()
	acme := filepath.Join(dir, "acme.sh")
	makeExecutable(t, acme)
	r := &leRunner{which: map[string]string{"acme.sh": acme}, version: "3.0.0"}
	p := lePlugin(t, r)
	docroot := filepath.Join(dir, "web1")
	d := row{
		"domain": "example.com", "subdomain": "none", "domain_id": float64(1),
		"document_root": docroot, "ssl": "y", "ssl_letsencrypt": "y",
	}

	ok, err := p.requestCert(context.Background(), webLEConfig{skipCheck: true, signatureType: "ECDSA"}, d)
	require.NoError(t, err)
	assert.True(t, ok)

	// The install-cert call must target the -le files.
	var install []string
	for _, c := range r.calls {
		if len(c) > 1 && c[1] == "--install-cert" {
			install = c
		}
	}
	require.NotNil(t, install)
	assert.Contains(t, install, filepath.Join(docroot, "ssl", "example.com-le.key"))
	assert.Contains(t, install, filepath.Join(docroot, "ssl", "example.com-le.crt"))
	assert.Contains(t, install, "--ecc")
}

// TestRequestCertNoClient returns a clear error.
func TestRequestCertNoClient(t *testing.T) {
	r := &leRunner{which: map[string]string{}}
	p := lePlugin(t, r)
	d := row{"domain": "example.com", "document_root": "/x", "ssl": "y", "ssl_letsencrypt": "y"}
	ok, err := p.requestCert(context.Background(), webLEConfig{skipCheck: true}, d)
	assert.False(t, ok)
	require.ErrorContains(t, err, "no Let's Encrypt client")
}

// TestLeSSLDomainWildcardStripped: a wildcard site requests the apex.
func TestLeSSLDomainWildcardStripped(t *testing.T) {
	assert.Equal(t, "example.com",
		leSSLDomain(row{"domain": "*.example.com", "ssl": "y", "ssl_letsencrypt": "y"}))
	assert.Equal(t, "example.com",
		leSSLDomain(row{"domain": "example.com", "ssl_domain": "example.com", "ssl": "y", "ssl_letsencrypt": "y"}))
}

// helpers for the reachability test.
func hostFromURL(url string) string {
	rest := url[len("http://"):]
	if i := indexByte(rest, '/'); i >= 0 {
		return rest[:i]
	}
	return rest
}

func challengeFileFromURL(t *testing.T, webroot, url string) string {
	t.Helper()
	base := filepath.Base(url)
	return filepath.Join(webroot, ".well-known", "acme-challenge", base)
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}
