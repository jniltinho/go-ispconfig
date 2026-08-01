package nginx

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/getconf"
)

// recordingExecutor captures delayed service flushes.
type recordingExecutor struct{ runs [][2]string }

func (f *recordingExecutor) Run(_ context.Context, service, action string) error {
	f.runs = append(f.runs, [2]string{service, action})
	return nil
}

// activateFixture builds a plugin wired to temp vhost dirs, a fake runner
// and a real Services registry with a recording executor.
func activateFixture(t *testing.T) (*Plugin, *fakeRunner, *recordingExecutor, *getconf.WebConfig, *engine.Services) {
	t.Helper()
	base := t.TempDir()
	runner := newFakeRunner()
	exec := &recordingExecutor{}
	services := engine.NewServices(exec, nil)
	services.Register("httpd")
	p := NewPlugin(nil, services, runner, "", nil)
	p.logBaseDir = filepath.Join(base, "logs")
	cfg := &getconf.WebConfig{
		WebsiteBasedir:           base,
		NginxVhostConfDir:        filepath.Join(base, "sites-available"),
		NginxVhostConfEnabledDir: filepath.Join(base, "sites-enabled"),
	}
	require.NoError(t, os.MkdirAll(cfg.NginxVhostConfDir, 0o755))
	require.NoError(t, os.MkdirAll(cfg.NginxVhostConfEnabledDir, 0o755))
	return p, runner, exec, cfg, services
}

func activateSite(cfg *getconf.WebConfig, d row, old row) site {
	action := "update"
	if len(old) == 0 {
		action = "insert"
	}
	return site{cfg: cfg, action: action, old: old, new: d}
}

// TestActivateVhostSuccess covers "Valid vhost goes live": file written,
// symlink in the enabled dir, exactly one delayed httpd reload.
func TestActivateVhostSuccess(t *testing.T) {
	p, runner, exec, cfg, services := activateFixture(t)
	d := row{"domain": "example.com", "subdomain": "www", "active": "y"}

	require.NoError(t, p.activateVhost(context.Background(), activateSite(cfg, d, row{}), "server { }\n"))

	content, err := os.ReadFile(filepath.Join(cfg.NginxVhostConfDir, "example.com.vhost"))
	require.NoError(t, err)
	assert.Equal(t, "server { }\n", string(content))

	link := filepath.Join(cfg.NginxVhostConfEnabledDir, "100-example.com.vhost")
	target, err := os.Readlink(link)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(cfg.NginxVhostConfDir, "example.com.vhost"), target)

	assert.Contains(t, runner.commands(), "nginx -t")
	services.ProcessDelayedActions(context.Background())
	assert.Equal(t, [][2]string{{"httpd", "reload"}}, exec.runs)

	// No leftover backup.
	assert.NoFileExists(t, filepath.Join(cfg.NginxVhostConfDir, "example.com.vhost~"))
}

// TestActivateVhostRollback covers "Broken vhost is quarantined and rolled
// back": .err saved, previous vhost restored, no reload scheduled, error
// carries the nginx -t output.
func TestActivateVhostRollback(t *testing.T) {
	p, runner, exec, cfg, services := activateFixture(t)
	d := row{"domain": "example.com", "subdomain": "www", "active": "y"}
	vhostFile := filepath.Join(cfg.NginxVhostConfDir, "example.com.vhost")
	require.NoError(t, os.WriteFile(vhostFile, []byte("server { old }\n"), 0o644))

	runner.failCmd = "nginx"
	runner.failOut = `nginx: [emerg] unknown directive "bogus"`

	err := p.activateVhost(context.Background(), activateSite(cfg, d, d), "server { bogus }\n")
	require.Error(t, err)
	assert.ErrorContains(t, err, "unknown directive")

	restored, readErr := os.ReadFile(vhostFile)
	require.NoError(t, readErr)
	assert.Equal(t, "server { old }\n", string(restored), "previous vhost restored")

	quarantined, readErr := os.ReadFile(vhostFile + ".err")
	require.NoError(t, readErr)
	assert.Equal(t, "server { bogus }\n", string(quarantined), "broken config quarantined")

	services.ProcessDelayedActions(context.Background())
	assert.Empty(t, exec.runs, "no reload with a broken config")
}

// TestActivateVhostRollbackWithoutPrevious: a brand-new site that fails
// validation leaves a placeholder comment file.
func TestActivateVhostRollbackWithoutPrevious(t *testing.T) {
	p, runner, _, cfg, _ := activateFixture(t)
	runner.failCmd = "nginx"
	d := row{"domain": "new.example.com", "subdomain": "none", "active": "y"}

	err := p.activateVhost(context.Background(), activateSite(cfg, d, row{}), "server { bogus }\n")
	require.Error(t, err)

	vhostFile := filepath.Join(cfg.NginxVhostConfDir, "new.example.com.vhost")
	content, readErr := os.ReadFile(vhostFile)
	require.NoError(t, readErr)
	assert.Contains(t, string(content), "# nginx did not start")
	assert.FileExists(t, vhostFile+".err")
}

// TestActivateVhostSSLRollback: when the certificate changed in this run, a
// failed nginx -t restores the ~ SSL backups and quarantines the new files.
func TestActivateVhostSSLRollback(t *testing.T) {
	p, runner, _, cfg, _ := activateFixture(t)
	docroot := filepath.Join(cfg.WebsiteBasedir, "clients/client1/web1")
	require.NoError(t, os.MkdirAll(filepath.Join(docroot, "ssl"), 0o755))
	key := filepath.Join(docroot, "ssl", "example.com.key")
	require.NoError(t, os.WriteFile(key, []byte("NEW KEY"), 0o600))
	require.NoError(t, os.WriteFile(key+"~", []byte("OLD KEY"), 0o600))

	runner.failCmd = "nginx"
	d := row{
		"domain": "example.com", "subdomain": "www", "active": "y",
		"document_root": docroot,
	}
	s := activateSite(cfg, d, d)
	s.sslChanged = true

	require.Error(t, p.activateVhost(context.Background(), s, "server { bogus }\n"))

	restored, err := os.ReadFile(key)
	require.NoError(t, err)
	assert.Equal(t, "OLD KEY", string(restored))
	quarantined, err := os.ReadFile(key + ".err")
	require.NoError(t, err)
	assert.Equal(t, "NEW KEY", string(quarantined))
}

// TestActivateVhostDeactivate covers "Deactivated domain removes the
// enabled symlink": active=n removes the link and still schedules a reload.
func TestActivateVhostDeactivate(t *testing.T) {
	p, _, exec, cfg, services := activateFixture(t)
	active := row{"domain": "example.com", "subdomain": "www", "active": "y"}
	require.NoError(t, p.activateVhost(context.Background(), activateSite(cfg, active, row{}), "server { }\n"))
	link := filepath.Join(cfg.NginxVhostConfEnabledDir, "100-example.com.vhost")
	require.FileExists(t, filepath.Join(cfg.NginxVhostConfDir, "example.com.vhost"))
	_, err := os.Lstat(link)
	require.NoError(t, err)

	inactive := row{"domain": "example.com", "subdomain": "www", "active": "n"}
	require.NoError(t, p.activateVhost(context.Background(), activateSite(cfg, inactive, active), "server { }\n"))
	_, err = os.Lstat(link)
	assert.True(t, os.IsNotExist(err), "enabled symlink removed on deactivate")

	services.ProcessDelayedActions(context.Background())
	assert.Equal(t, [][2]string{{"httpd", "reload"}}, exec.runs)
}

// TestActivateVhostRename: a domain rename drops the old vhost file and
// symlinks and creates the new ones.
func TestActivateVhostRename(t *testing.T) {
	p, _, _, cfg, _ := activateFixture(t)
	old := row{"domain": "old.example.com", "subdomain": "none", "active": "y"}
	require.NoError(t, p.activateVhost(context.Background(), activateSite(cfg, old, row{}), "server { }\n"))

	renamed := row{"domain": "new.example.com", "subdomain": "none", "active": "y"}
	require.NoError(t, p.activateVhost(context.Background(), activateSite(cfg, renamed, old), "server { }\n"))

	assert.NoFileExists(t, filepath.Join(cfg.NginxVhostConfDir, "old.example.com.vhost"))
	_, err := os.Lstat(filepath.Join(cfg.NginxVhostConfEnabledDir, "100-old.example.com.vhost"))
	assert.True(t, os.IsNotExist(err))
	assert.FileExists(t, filepath.Join(cfg.NginxVhostConfDir, "new.example.com.vhost"))
	_, err = os.Lstat(filepath.Join(cfg.NginxVhostConfEnabledDir, "100-new.example.com.vhost"))
	assert.NoError(t, err)
}

// TestActivateVhostWildcardUses900Prefix: subdomain '*' sites order last.
func TestActivateVhostWildcardUses900Prefix(t *testing.T) {
	p, _, _, cfg, _ := activateFixture(t)
	d := row{"domain": "example.com", "subdomain": "*", "active": "y"}
	require.NoError(t, p.activateVhost(context.Background(), activateSite(cfg, d, row{}), "server { }\n"))
	_, err := os.Lstat(filepath.Join(cfg.NginxVhostConfEnabledDir, "900-example.com.vhost"))
	assert.NoError(t, err)
}
