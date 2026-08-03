package apache2

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/getconf"
)

// ensureRunner fakes the OS commands the site provisioning issues: getent
// reports every account as missing, so useradd/groupadd always run.
type ensureRunner struct{ calls []string }

func (r *ensureRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, strings.Join(append([]string{name}, args...), " "))
	if name == "getent" {
		return nil, fmt.Errorf("exit status 2")
	}
	return nil, nil
}

// TestEnsureSiteProvisionsApacheTree: the Apache plugin owns its own site
// provisioning — document root tree, system user/group and the Apache worker
// (the [web] user, not nginx_user) joined to the client group.
func TestEnsureSiteProvisionsApacheTree(t *testing.T) {
	base := t.TempDir()
	r := &ensureRunner{}
	p := NewPlugin(nil, nil, r, "", nil)
	p.logBaseDir = filepath.Join(base, "httpd")

	cfg := &getconf.WebConfig{
		WebsiteBasedir: base, SecurityLevel: "20",
		User: "www-data", NginxUser: "nginx-should-not-be-used",
	}
	d := row{
		"domain_id": float64(1), "server_id": float64(1),
		"domain": "example.com", "type": "vhost",
		"document_root": filepath.Join(base, "clients/client1/web1"),
		"system_user":   "web1", "system_group": "client1",
		"errordocs": float64(1), "active": "y",
	}

	require.NoError(t, p.ensureSite(context.Background(), cfg, "insert", row{}, d))

	docroot := d.str("document_root")
	for _, rel := range []string{"web", "web/error", "log", "ssl", "tmp", "private", "cgi-bin"} {
		assert.DirExistsf(t, filepath.Join(docroot, rel), "missing %s", rel)
	}
	assert.DirExists(t, filepath.Join(p.logBaseDir, "example.com"))
	assert.FileExists(t, filepath.Join(docroot, "web", "standard_index.html"))

	assert.Contains(t, r.calls, "groupadd client1")
	assert.Contains(t, r.calls, "useradd -d "+docroot+" -g client1 -s /bin/false web1")
	assert.Contains(t, r.calls, "usermod -a -G client1 www-data")
	assert.NotContains(t, strings.Join(r.calls, "\n"), "nginx-should-not-be-used")

	// Re-running provisions nothing new.
	before := len(r.calls)
	require.NoError(t, p.ensureSite(context.Background(), cfg, "update", d, d))
	assert.NotEmpty(t, r.calls[before:], "idempotent run still re-asserts ownership")
}

// TestEnsureSiteRefusesUnsafeDocroot: a docroot outside website_basedir never
// reaches the filesystem.
func TestEnsureSiteRefusesUnsafeDocroot(t *testing.T) {
	base := t.TempDir()
	r := &ensureRunner{}
	p := NewPlugin(nil, nil, r, "", nil)
	p.logBaseDir = filepath.Join(base, "httpd")

	d := row{
		"domain": "example.com", "type": "vhost",
		"document_root": "/etc/apache2",
		"system_user":   "web1", "system_group": "client1",
	}
	err := p.ensureSite(context.Background(), &getconf.WebConfig{WebsiteBasedir: base}, "insert", row{}, d)
	require.ErrorContains(t, err, "apache2:")
	assert.Empty(t, r.calls)
	assert.NoDirExists(t, filepath.Join(base, "httpd", "example.com"))
	_, statErr := os.Stat("/etc/apache2/web")
	assert.True(t, os.IsNotExist(statErr), "nothing may be created outside website_basedir")
}
