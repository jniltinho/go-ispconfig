package nginx

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAllowedSystemNameRejectsFlags: a name usable as a command-line flag is
// refused so it cannot reach root's useradd/chown as an option.
func TestAllowedSystemNameRejectsFlags(t *testing.T) {
	for _, bad := range []string{"-u0", "-rf", "", "root", "ispconfig", "a b", "toolongnametoolongnametoolongname"} {
		assert.Falsef(t, allowedSystemName(bad), "%q must be rejected", bad)
	}
	for _, ok := range []string{"web1", "client12", "web_1", "a.b-c"} {
		assert.Truef(t, allowedSystemName(ok), "%q must be allowed", ok)
	}
}

// TestSafeDomain rejects anything usable as a path escape.
func TestSafeDomain(t *testing.T) {
	for _, bad := range []string{"", "../../etc/x", "a/b", "..", "a b.com", "foo/../bar"} {
		assert.Errorf(t, safeDomain(bad), "%q must be rejected", bad)
	}
	for _, ok := range []string{"example.com", "sub.example.co.uk", "*.example.com", "web1"} {
		assert.NoErrorf(t, safeDomain(ok), "%q must be allowed", ok)
	}
}

// TestEnsureSiteRefusesEscapingWebFolder: a web_folder that resolves outside
// the docroot is refused before any chown/chmod as root.
func TestEnsureSiteRefusesEscapingWebFolder(t *testing.T) {
	r := newFakeRunner()
	p, base := testPlugin(t, r)
	d := vhostRow(base)
	d["type"] = "vhostsubdomain"
	d["domain"] = "blog.example.com"
	d["web_folder"] = "../../../../tmp/pwn"
	err := p.ensureSite(context.Background(), site{
		cfg: webCfg(base), action: "insert", old: row{}, new: d, parentDomain: "example.com",
	})
	require.Error(t, err)
	assert.Empty(t, r.calls, "no OS command may run for an escaping web folder")
}

// TestEnsureSiteRefusesUnsafeDomain: a domain usable as a path segment is
// refused.
func TestEnsureSiteRefusesUnsafeDomain(t *testing.T) {
	r := newFakeRunner()
	p, base := testPlugin(t, r)
	d := vhostRow(base)
	d["domain"] = "../../etc/nginx/evil"
	err := p.ensureSite(context.Background(), site{cfg: webCfg(base), action: "insert", old: row{}, new: d})
	require.ErrorContains(t, err, "unsafe domain")
}

// TestEnsureSiteAddsNginxUserToGroup: security level 20 adds the nginx worker
// to the client group so it can read the site files.
func TestEnsureSiteAddsNginxUserToGroup(t *testing.T) {
	r := newFakeRunner()
	p, base := testPlugin(t, r)
	cfg := webCfg(base)
	cfg.NginxUser = "www-data"
	require.NoError(t, p.ensureSite(context.Background(), site{
		cfg: cfg, action: "insert", old: row{}, new: vhostRow(base),
	}))
	assert.Contains(t, r.commands(), "usermod -a -G client1 www-data")
}

// TestValidHtpasswdField blocks line/colon injection.
func TestValidHtpasswdField(t *testing.T) {
	assert.Error(t, validHtpasswdField("alice\nroot", "x"))
	assert.Error(t, validHtpasswdField("a:b", "x"))
	assert.Error(t, validHtpasswdField("alice", "hash\nadmin:evil"))
	assert.NoError(t, validHtpasswdField("alice", "$1$abc$hash"))
}

// TestBlacklistAppliesAfterTemplateRender: a forbidden directive hidden
// inside template logic is stripped after the render pass, not passed
// through.
func TestBlacklistAppliesAfterTemplateRender(t *testing.T) {
	d := goldenDomain()
	// use_socket is truthy for this fixture, so the tmpl_if body renders to a
	// load_module line the raw-line filter would have missed.
	d["nginx_directives"] = "client_max_body_size 50M;\n<tmpl_if name='use_socket'>load_module /tmp/evil.so;</tmpl_if>"
	content, warnings, err := renderVhost(vhostInput{
		cfg: goldenCfg(), d: d, nginxVersion: "1.26.1", dummyFile: "/d.htm",
	}, "")
	require.NoError(t, err)
	require.NotEmpty(t, warnings, "the rendered load_module must be reported")
	assert.NotContains(t, content, "load_module")
	assert.Contains(t, content, "client_max_body_size 50M;")
}

// TestDirectiveRowsEmptyRenderDropsBlock: directives that render to empty
// must not leak raw template syntax into the vhost.
func TestDirectiveRowsEmptyRenderDropsBlock(t *testing.T) {
	d := goldenDomain()
	d["nginx_directives"] = "<tmpl_if name='use_tcp'>fastcgi_read_timeout 300;</tmpl_if>"
	rows, warnings := directiveRows(d, map[string]any{"use_tcp": 0}, fpmInfo{})
	assert.Empty(t, warnings)
	assert.Empty(t, rows, "an empty render must drop the block, not emit tmpl syntax")
}

// TestSSLKeyRollbackKeepsMode0600: a restored/quarantined SSL key is not
// world-readable.
func TestSSLKeyRollbackKeepsMode0600(t *testing.T) {
	p, runner, _, cfg, _ := activateFixture(t)
	docroot := filepath.Join(cfg.WebsiteBasedir, "clients/client1/web1")
	require.NoError(t, os.MkdirAll(filepath.Join(docroot, "ssl"), 0o755))
	key := filepath.Join(docroot, "ssl", "example.com.key")
	require.NoError(t, os.WriteFile(key, []byte("NEW"), 0o600))
	require.NoError(t, os.WriteFile(key+"~", []byte("OLD"), 0o600))

	runner.failCmd = "nginx"
	d := row{"domain": "example.com", "subdomain": "www", "active": "y", "document_root": docroot}
	s := activateSite(cfg, d, d)
	s.sslChanged = true
	require.Error(t, p.activateVhost(context.Background(), s, "server { bogus }\n"))

	ki, err := os.Stat(key)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), ki.Mode().Perm(), "restored key stays 0600")
	ei, err := os.Stat(key + ".err")
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), ei.Mode().Perm(), "quarantined key stays 0600")
}
