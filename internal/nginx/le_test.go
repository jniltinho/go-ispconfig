package nginx

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type leRunner struct {
	calls [][]string
}

func (r *leRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, append([]string{name}, args...))
	return nil, nil
}

func lePlugin(t *testing.T, r *leRunner) *Plugin {
	t.Helper()
	p := NewPlugin(nil, nil, r, "", nil)
	p.leWebroot = filepath.Join(t.TempDir(), "acme")
	return p
}

// TestAssembleDomainsReachability: only domains that echo the challenge token
// survive, and www is added for a www site.
func TestAssembleDomainsReachability(t *testing.T) {
	p := lePlugin(t, &leRunner{})
	var probed []string
	p.leHTTPGet = func(url string) (string, error) {
		probed = append(probed, url)
		hash, _ := os.ReadFile(challengeFileFromURL(t, p.acmeWebroot(), url))
		if hostFromURL(url) == "example.com" {
			return string(hash), nil
		}
		return "unreachable", nil
	}

	d := row{"domain": "example.com", "subdomain": "www", "domain_id": float64(1)}
	got, err := p.assembleDomains(context.Background(), d, "example.com", true)
	require.NoError(t, err)
	assert.Equal(t, []string{"example.com"}, got, "unreachable www dropped")
	assert.NotEmpty(t, probed)
}

// TestRequestCertInstallsFiles: issuance links -le files into the ssl dir.
func TestRequestCertInstallsFiles(t *testing.T) {
	dir := t.TempDir()
	p := lePlugin(t, &leRunner{})
	docroot := filepath.Join(dir, "web1")
	d := row{
		"domain": "example.com", "subdomain": "none", "domain_id": float64(1),
		"document_root": docroot, "ssl": "y", "ssl_letsencrypt": "y",
	}
	p.leIssue = func(_ string, keyFile, crtFile string) (bool, error) {
		require.NoError(t, os.MkdirAll(filepath.Dir(keyFile), 0o755))
		require.NoError(t, os.WriteFile(keyFile, []byte("KEY"), 0o644))
		require.NoError(t, os.WriteFile(crtFile, []byte("CRT"), 0o644))
		return true, nil
	}

	ok, err := p.requestCert(context.Background(), webLEConfig{skipCheck: true, signatureType: "ECDSA"}, d)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.FileExists(t, filepath.Join(docroot, "ssl", "example.com-le.key"))
	assert.FileExists(t, filepath.Join(docroot, "ssl", "example.com-le.crt"))
}

// TestLeSSLDomainWildcardStripped: a wildcard site requests the apex.
func TestLeSSLDomainWildcardStripped(t *testing.T) {
	assert.Equal(t, "example.com",
		leSSLDomain(row{"domain": "*.example.com", "ssl": "y", "ssl_letsencrypt": "y"}))
	assert.Equal(t, "example.com",
		leSSLDomain(row{"domain": "example.com", "ssl_domain": "example.com", "ssl": "y", "ssl_letsencrypt": "y"}))
}

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
