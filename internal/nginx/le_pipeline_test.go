package nginx

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/getconf"
)

// TestCertPathsLEAware: a letsencrypt site references the -le files.
func TestCertPathsLEAware(t *testing.T) {
	self := row{"document_root": "/var/www/web1", "domain": "example.com", "ssl": "y", "ssl_letsencrypt": "n"}
	key, crt, _ := certPaths(self)
	assert.Equal(t, "/var/www/web1/ssl/example.com.key", key)
	assert.Equal(t, "/var/www/web1/ssl/example.com.crt", crt)

	le := row{"document_root": "/var/www/web1", "domain": "example.com", "ssl": "y", "ssl_letsencrypt": "y"}
	key, crt, _ = certPaths(le)
	assert.Equal(t, "/var/www/web1/ssl/example.com-le.key", key)
	assert.Equal(t, "/var/www/web1/ssl/example.com-le.crt", crt)
}

// TestVhostUsesLEPaths: the rendered vhost points ssl_certificate at the -le
// file for a letsencrypt site.
func TestVhostUsesLEPaths(t *testing.T) {
	d := goldenDomain()
	d["ssl"] = "y"
	d["ssl_letsencrypt"] = "y"
	out := renderGolden(t, vhostInput{cfg: goldenCfg(), d: d, sslFilesExist: true})
	assert.Contains(t, out, "ssl_certificate /var/www/clients/client1/web1/ssl/example.com-le.crt;")
	assert.Contains(t, out, "ssl_certificate_key /var/www/clients/client1/web1/ssl/example.com-le.key;")
}

// TestMaybeRequestLEIssuesAndFlags: turning LE on issues a cert, installs the
// -le files and flags the cert change; no warnings.
func TestMaybeRequestLEIssuesAndFlags(t *testing.T) {
	p := lePlugin(t, &leRunner{})
	docroot := filepath.Join(t.TempDir(), "web1")
	d := row{
		"domain": "example.com", "subdomain": "none", "domain_id": float64(1),
		"document_root": docroot, "ssl": "y", "ssl_letsencrypt": "y",
	}
	old := row{"ssl": "n", "ssl_letsencrypt": "n"}
	s := &site{cfg: &getconf.WebConfig{SkipLeCheck: "y"}, new: d, old: old}
	p.leIssue = func(_ string, keyFile, crtFile string) (bool, error) {
		require.NoError(t, os.MkdirAll(filepath.Dir(keyFile), 0o755))
		require.NoError(t, os.WriteFile(keyFile, []byte("KEY"), 0o644))
		require.NoError(t, os.WriteFile(crtFile, []byte("CRT"), 0o644))
		return true, nil
	}

	warnings := p.maybeRequestLE(context.Background(), s)
	assert.Empty(t, warnings)
	assert.True(t, s.sslChanged)

	key, crt, _ := certPaths(d)
	assert.FileExists(t, key)
	assert.FileExists(t, crt)
}

// TestMaybeRequestLESkippedWhenUnchanged: an unrelated update on a stable LE
// site does not re-issue.
func TestMaybeRequestLESkippedWhenUnchanged(t *testing.T) {
	r := &leRunner{}
	p := lePlugin(t, r)
	d := row{"domain": "example.com", "subdomain": "none", "ssl": "y", "ssl_letsencrypt": "y"}
	s := &site{cfg: &getconf.WebConfig{}, new: d, old: d}
	assert.Empty(t, p.maybeRequestLE(context.Background(), s))
}

// TestMaybeRequestLEFallsBackOnFailure: issuance failure returns a warning
// and the vhost renders without SSL when -le files are missing.
func TestMaybeRequestLEFallsBackOnFailure(t *testing.T) {
	p := lePlugin(t, &leRunner{})
	docroot := filepath.Join(t.TempDir(), "web1")
	d := row{
		"domain": "example.com", "subdomain": "none", "domain_id": float64(1),
		"document_root": docroot, "ssl": "y", "ssl_letsencrypt": "y",
	}
	old := row{"ssl": "n", "ssl_letsencrypt": "n"}
	s := &site{cfg: &getconf.WebConfig{SkipLeCheck: "y"}, new: d, old: old}
	p.leIssue = func(string, string, string) (bool, error) {
		return false, assertErr("issuance failed")
	}

	warnings := p.maybeRequestLE(context.Background(), s)
	require.Len(t, warnings, 1)
	assert.ErrorContains(t, warnings[0], "issuance failed")

	d["errordocs"] = float64(1)
	out := renderGolden(t, vhostInput{cfg: goldenCfg(), d: d, sslFilesExist: false})
	assert.NotContains(t, out, "ssl_certificate ")
}

type assertErr string

func (e assertErr) Error() string { return string(e) }
