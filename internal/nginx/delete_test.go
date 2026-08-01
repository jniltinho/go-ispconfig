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

// provisionedSite creates a full site on disk (vhost file, symlink, pool,
// docroot, log dir, website symlink) and returns its row.
func provisionedSite(t *testing.T, p *Plugin, cfg *webDeleteCfg) row {
	t.Helper()
	d := row{
		"domain_id": float64(1), "server_id": float64(1),
		"domain": "example.com", "type": "vhost", "subdomain": "www",
		"document_root": filepath.Join(cfg.base, "clients/client1/web1"),
		"system_user":   "web1", "system_group": "client1",
		"php": "php-fpm", "php_fpm_use_socket": "y", "active": "y",
	}
	require.NoError(t, os.MkdirAll(filepath.Join(d.str("document_root"), "web"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(p.logBaseDir, "example.com"), 0o755))
	vhostFile := filepath.Join(cfg.cfg.NginxVhostConfDir, "example.com.vhost")
	require.NoError(t, os.WriteFile(vhostFile, []byte("server { }\n"), 0o644))
	require.NoError(t, os.Symlink(vhostFile, filepath.Join(cfg.cfg.NginxVhostConfEnabledDir, "100-example.com.vhost")))
	require.NoError(t, os.MkdirAll(cfg.poolDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cfg.poolDir, "web1.conf"), []byte("[web1]\n"), 0o644))
	require.NoError(t, os.Symlink(d.str("document_root")+"/", filepath.Join(cfg.base, "example.com")))
	return d
}

type webDeleteCfg struct {
	base    string
	poolDir string
	cfg     *getconf.WebConfig
}

// deleteFixture builds a plugin plus config with temp dirs and a fake
// runner that knows user web1 and group client1.
func deleteFixture(t *testing.T) (*Plugin, *fakeRunner, *webDeleteCfg) {
	t.Helper()
	base := t.TempDir()
	r := newFakeRunner()
	r.users["web1"] = true
	r.groups["client1"] = true
	p := NewPlugin(nil, nil, r, "", nil)
	p.logBaseDir = filepath.Join(base, "logs")
	cfg := &getconf.WebConfig{
		WebsiteBasedir:           base,
		NginxVhostConfDir:        filepath.Join(base, "sites-available"),
		NginxVhostConfEnabledDir: filepath.Join(base, "sites-enabled"),
		PHPFPMPoolDir:            filepath.Join(base, "pool.d"),
		PHPFPMInitScript:         "php8.3-fpm",
		PHPFPMSocketDir:          filepath.Join(base, "sock"),
		WebsiteSymlinks:          base + "/[website_domain]/",
	}
	require.NoError(t, os.MkdirAll(cfg.NginxVhostConfDir, 0o755))
	require.NoError(t, os.MkdirAll(cfg.NginxVhostConfEnabledDir, 0o755))
	return p, r, &webDeleteCfg{base: base, poolDir: cfg.PHPFPMPoolDir, cfg: cfg}
}

// TestDeleteSiteRemovesEverything covers "Delete removes vhost and
// directories": vhost file, symlink, pool file, docroot, log dir and the
// system user are gone.
func TestDeleteSiteRemovesEverything(t *testing.T) {
	p, r, fix := deleteFixture(t)
	d := provisionedSite(t, p, fix)

	require.NoError(t, p.deleteSite(context.Background(), fix.cfg, d, nil, 1))

	assert.NoFileExists(t, filepath.Join(fix.cfg.NginxVhostConfDir, "example.com.vhost"))
	_, err := os.Lstat(filepath.Join(fix.cfg.NginxVhostConfEnabledDir, "100-example.com.vhost"))
	assert.True(t, os.IsNotExist(err), "enabled symlink removed")
	assert.NoFileExists(t, filepath.Join(fix.poolDir, "web1.conf"))
	assert.NoDirExists(t, d.str("document_root"))
	assert.NoDirExists(t, filepath.Join(p.logBaseDir, "example.com"))
	_, err = os.Lstat(filepath.Join(fix.base, "example.com"))
	assert.True(t, os.IsNotExist(err), "website symlink removed")
	assert.Contains(t, r.commands(), "userdel web1")
}

// TestDeleteSiteRefusesUnsafePaths covers "Unsafe path is never deleted".
func TestDeleteSiteRefusesUnsafePaths(t *testing.T) {
	p, r, fix := deleteFixture(t)
	for _, docroot := range []string{"/", fix.base, "/etc", fix.base + "/../x"} {
		d := row{
			"domain": "example.com", "type": "vhost", "server_id": float64(1),
			"document_root": docroot, "system_user": "web1",
		}
		err := p.deleteSite(context.Background(), fix.cfg, d, nil, 1)
		assert.Errorf(t, err, "docroot %q must be refused", docroot)
	}
	assert.NotContains(t, r.commands(), "userdel web1", "nothing deleted for unsafe paths")
}

// TestSubdomainFolderToDelete pins the used-paths safety port.
func TestSubdomainFolderToDelete(t *testing.T) {
	// Paths under web/ or empty are never deleted.
	_, ok := subdomainFolderToDelete("web/sub", nil)
	assert.False(t, ok)
	_, ok = subdomainFolderToDelete("/", nil)
	assert.False(t, ok)

	// Fully unused tree: the shallowest unused ancestor is removed (PHP
	// walks upward while unused, so the whole "blog" tree goes).
	folder, ok := subdomainFolderToDelete("blog/2024", nil)
	assert.True(t, ok)
	assert.Equal(t, "blog", folder)

	// The folder itself still used by a sibling: nothing is removed.
	_, ok = subdomainFolderToDelete("blog", []string{"blog"})
	assert.False(t, ok)

	// A parent is used: only the unused deep part is removed.
	folder, ok = subdomainFolderToDelete("blog/2024/de", []string{"blog/2024"})
	assert.True(t, ok)
	assert.Equal(t, "blog/2024/de", folder)

	// Sibling uses a deeper path: the shared parents stay.
	folder, ok = subdomainFolderToDelete("shared/a", []string{"shared/b"})
	assert.True(t, ok)
	assert.Equal(t, "shared/a", folder)
}

// TestDeleteSubdomainKeepsSharedFolder: deleting a vhostsubdomain only
// removes its own folder, never the parent docroot.
func TestDeleteSubdomainKeepsSharedFolder(t *testing.T) {
	p, _, fix := deleteFixture(t)
	docroot := filepath.Join(fix.base, "clients/client1/web1")
	require.NoError(t, os.MkdirAll(filepath.Join(docroot, "web"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(docroot, "blog"), 0o755))
	d := row{
		"domain": "blog.example.com", "type": "vhostsubdomain", "server_id": float64(1),
		"parent_domain_id": float64(1), "domain_id": float64(2),
		"document_root": docroot, "web_folder": "blog", "system_user": "web1",
	}

	require.NoError(t, p.deleteSite(context.Background(), fix.cfg, d, nil, 1))
	assert.NoDirExists(t, filepath.Join(docroot, "blog"))
	assert.DirExists(t, filepath.Join(docroot, "web"), "parent web folder untouched")
	assert.DirExists(t, docroot, "parent docroot untouched")
}
