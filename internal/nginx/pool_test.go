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

// poolDomain is a web_domain fixture for pool rendering.
func poolDomain() row {
	return row{
		"domain_id": float64(1), "server_id": float64(1),
		"domain": "example.com", "document_root": "/var/www/clients/client1/web1",
		"system_user": "web1", "system_group": "client1", "php": "php-fpm",
		"php_fpm_use_socket": "y", "php_fpm_chroot": "n",
		"pm": "dynamic", "pm_max_children": float64(10),
		"pm_start_servers": float64(2), "pm_min_spare_servers": float64(1),
		"pm_max_spare_servers": float64(5), "pm_process_idle_timeout": float64(10),
		"pm_max_requests": float64(0), "php_open_basedir": "",
		"custom_php_ini": "",
	}
}

func poolCfg() *getconf.WebConfig {
	return &getconf.WebConfig{
		SecurityLevel:    "20",
		Group:            "www-data",
		PHPFPMPoolDir:    "/etc/php/8.3/fpm/pool.d",
		PHPFPMSocketDir:  "/var/lib/php8.3-fpm",
		PHPFPMInitScript: "php8.3-fpm",
		PHPFPMStartPort:  "9010",
	}
}

func renderPoolGolden(t *testing.T, d row) string {
	t.Helper()
	cfg := poolCfg()
	p := NewPlugin(nil, nil, newFakeRunner(), "", nil)
	fpm := resolveFPM(cfg, d, nil)
	content, err := p.renderPool(cfg, d, fpm)
	require.NoError(t, err)
	return content
}

// TestGoldenPoolMatrix pins dynamic/static/ondemand x socket/TCP.
func TestGoldenPoolMatrix(t *testing.T) {
	for _, pm := range []string{"dynamic", "static", "ondemand"} {
		for _, sock := range []struct {
			name  string
			value string
		}{{"socket", "y"}, {"tcp", "n"}} {
			d := poolDomain()
			d["pm"] = pm
			d["php_fpm_use_socket"] = sock.value
			checkGolden(t, "pool_"+pm+"_"+sock.name, renderPoolGolden(t, d))
		}
	}
}

// TestGoldenPoolCustomIni covers the custom_php_ini loop and open_basedir.
func TestGoldenPoolCustomIni(t *testing.T) {
	d := poolDomain()
	d["php_open_basedir"] = "/var/www/clients/client1/web1/web:/tmp"
	d["custom_php_ini"] = "memory_limit = 256M\ndisplay_errors = off\n; a comment\nsession.save_path = {WEBROOT}/sessions"
	checkGolden(t, "pool_custom_ini", renderPoolGolden(t, d))
}

// TestCustomPHPIniMapping pins the flag-vs-value classification.
func TestCustomPHPIniMapping(t *testing.T) {
	rows, session, sendmail := customPHPIni(
		"memory_limit = 256M\ndisplay_errors = off\nallow_url_fopen = 0\nsendmail_path = /usr/sbin/sendmail\n# skip\n;skip2\nnovalue =",
		"/var/www/web1")
	got := make([]string, len(rows))
	for i, r := range rows {
		got[i] = r["ini_setting"].(string)
	}
	assert.Equal(t, []string{
		"php_admin_value[memory_limit] = 256M",
		"php_admin_flag[display_errors] = off",
		"php_admin_flag[allow_url_fopen] = off", // 0 -> off
		"php_admin_value[sendmail_path] = /usr/sbin/sendmail",
	}, got)
	assert.False(t, session)
	assert.True(t, sendmail)
}

// poolFixture: plugin with temp dirs, recording FPM executor, nil db.
func poolFixture(t *testing.T) (*Plugin, *recordingExecutor, *getconf.WebConfig, *engine.Services, string) {
	t.Helper()
	base := t.TempDir()
	exec := &recordingExecutor{}
	services := engine.NewServices(exec, nil)
	p := NewPlugin(nil, services, newFakeRunner(), "", nil)
	cfg := poolCfg()
	cfg.PHPFPMPoolDir = filepath.Join(base, "8.3/pool.d")
	cfg.PHPFPMSocketDir = filepath.Join(base, "8.3/sock")
	return p, exec, cfg, services, base
}

// TestManagePoolWritesAndReloads: a php-fpm site writes its pool and
// schedules exactly one reload of its version.
func TestManagePoolWritesAndReloads(t *testing.T) {
	p, exec, cfg, services, _ := poolFixture(t)
	d := poolDomain()
	fpm := resolveFPM(cfg, d, nil)

	require.NoError(t, p.managePool(context.Background(), cfg, d, row{}, fpm))
	poolFile := filepath.Join(cfg.PHPFPMPoolDir, "web1.conf")
	assert.FileExists(t, poolFile)
	assert.DirExists(t, cfg.PHPFPMSocketDir)

	services.ProcessDelayedActions(context.Background())
	assert.Equal(t, [][2]string{{"php8.3-fpm", "restart"}}, exec.runs)
}

// TestManagePoolPrunesStaleVersion: a pool left in the server-default dir by
// a previous version is removed and that version reloaded.
func TestManagePoolPrunesStaleVersion(t *testing.T) {
	p, exec, cfg, services, base := poolFixture(t)
	// Old pool sat in the default 8.3 dir; the site now pins 8.2.
	require.NoError(t, os.MkdirAll(cfg.PHPFPMPoolDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cfg.PHPFPMPoolDir, "web1.conf"), []byte("old\n"), 0o644))

	d := poolDomain()
	pinned := row{
		"php_fpm_pool_dir":    filepath.Join(base, "8.2/pool.d"),
		"php_fpm_socket_dir":  filepath.Join(base, "8.2/sock"),
		"php_fpm_init_script": "php8.2-fpm",
	}
	fpm := resolveFPM(cfg, d, pinned)
	require.NoError(t, p.managePool(context.Background(), cfg, d, row{}, fpm))

	assert.FileExists(t, filepath.Join(base, "8.2/pool.d", "web1.conf"))
	assert.NoFileExists(t, filepath.Join(cfg.PHPFPMPoolDir, "web1.conf"), "stale default-dir pool pruned")

	services.ProcessDelayedActions(context.Background())
	// Both the pruned default version and the new pinned version reload once.
	assert.ElementsMatch(t, [][2]string{{"php8.3-fpm", "restart"}, {"php8.2-fpm", "restart"}}, exec.runs)
}

// TestManagePoolNonFPMRemoves: switching a site to php=no removes its pool.
func TestManagePoolNonFPMRemoves(t *testing.T) {
	p, exec, cfg, services, _ := poolFixture(t)
	require.NoError(t, os.MkdirAll(cfg.PHPFPMPoolDir, 0o755))
	poolFile := filepath.Join(cfg.PHPFPMPoolDir, "web1.conf")
	require.NoError(t, os.WriteFile(poolFile, []byte("old\n"), 0o644))

	d := poolDomain()
	d["php"] = "no"
	old := poolDomain() // was php-fpm
	fpm := resolveFPM(cfg, d, nil)
	require.NoError(t, p.managePool(context.Background(), cfg, d, old, fpm))

	assert.NoFileExists(t, poolFile)
	services.ProcessDelayedActions(context.Background())
	assert.Equal(t, [][2]string{{"php8.3-fpm", "restart"}}, exec.runs)
}

// TestReloadActionRespectsConfig: php_fpm_reload_mode=reload yields reload.
func TestReloadActionRespectsConfig(t *testing.T) {
	assert.Equal(t, engine.ActionRestart, reloadAction(&getconf.WebConfig{}))
	assert.Equal(t, engine.ActionReload, reloadAction(&getconf.WebConfig{PHPFPMReloadMode: "reload"}))
}
