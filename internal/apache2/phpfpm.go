package apache2

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/getconf"
	"go-ispconfig/internal/mastertpl"
)

// poolTemplate is the per-site PHP-FPM pool template, shared verbatim with
// the nginx plugin — the pool is a PHP-FPM concern, not a web-server one.
const poolTemplate = "php_fpm_pool.conf.master"

// buildPool assembles the pool template vector for one site.
func buildPool(cfg *getconf.WebConfig, d row, fpm fpmInfo) (map[string]any, []map[string]any) {
	documentRoot, _, _ := docRoots(d)
	// The socket is opened by the pool (running as the site user) and dialed by
	// Apache, so it must be group-readable by the web server group.
	listenGroup := cfg.Group
	if listenGroup == "" {
		listenGroup = "www-data"
	}
	vars := map[string]any{
		"fpm_pool":                fpm.poolName,
		"fpm_port":                fpm.port,
		"fpm_socket":              fpm.socketPath,
		"fpm_listen_user":         d.str("system_user"),
		"fpm_listen_group":        listenGroup,
		"fpm_listen_mode":         "0660",
		"fpm_user":                d.str("system_user"),
		"fpm_group":               d.str("system_group"),
		"pm":                      orDefault(d.str("pm"), "dynamic"),
		"pm_max_children":         orDefault(d.str("pm_max_children"), "10"),
		"pm_start_servers":        orDefault(d.str("pm_start_servers"), "2"),
		"pm_min_spare_servers":    orDefault(d.str("pm_min_spare_servers"), "1"),
		"pm_max_spare_servers":    orDefault(d.str("pm_max_spare_servers"), "5"),
		"pm_process_idle_timeout": orDefault(d.str("pm_process_idle_timeout"), "10"),
		"pm_max_requests":         orDefault(d.str("pm_max_requests"), "0"),
		"document_root":           documentRoot,
		"security_level":          cfg.SecurityLevel,
		"php_open_basedir":        d.str("php_open_basedir"),
		"use_socket":              fpm.useSocket,
		"use_tcp":                 !fpm.useSocket,
	}
	return vars, customPHPIniRows(d.str("custom_php_ini"))
}

// orDefault returns v trimmed, or def when it is empty.
func orDefault(v, def string) string {
	if s := strings.TrimSpace(v); s != "" {
		return s
	}
	return def
}

// customPHPIniRows turns the site's custom php.ini text into pool
// php_admin_value rows. Lines without an "=" are dropped rather than emitted
// half-parsed — PHP-FPM refuses to start on a malformed pool.
func customPHPIniRows(raw string) []map[string]any {
	raw = strings.ReplaceAll(strings.ReplaceAll(raw, "\r\n", "\n"), "\r", "\n")
	var rows []map[string]any
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		rows = append(rows, map[string]any{
			"ini_setting": strings.TrimSpace(k),
			"ini_value":   strings.TrimSpace(v),
		})
	}
	return rows
}

// renderPool renders the pool file for one site.
func (p *Plugin) renderPool(cfg *getconf.WebConfig, d row, fpm fpmInfo) (string, error) {
	vars, settings := buildPool(cfg, d, fpm)
	src, _, err := mastertpl.Load(poolTemplate, p.customTplDir)
	if err != nil {
		return "", err
	}
	tpl := mastertpl.New(src)
	for k, v := range vars {
		tpl.SetVar(k, v)
	}
	tpl.SetLoop("custom_php_ini_settings", settings)
	out, err := tpl.Render()
	if err != nil {
		return "", fmt.Errorf("apache2: rendering %s: %w", poolTemplate, err)
	}
	return out, nil
}

// managePool writes or removes the site's PHP-FPM pool and schedules the
// delayed reload of the FPM unit. Apache reaches the pool through
// mod_proxy_fcgi, so the pool must exist before the vhost referencing it is
// activated.
func (p *Plugin) managePool(cfg *getconf.WebConfig, d row, fpm fpmInfo) error {
	if fpm.poolDir == "" {
		return nil
	}
	poolFile := filepath.Join(fpm.poolDir, fpm.poolName+".conf")
	if phpMode(d.str("php")) != "php-fpm" {
		if err := os.Remove(poolFile); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("apache2: removing pool %s: %w", poolFile, err)
		}
		p.scheduleFPM(cfg, fpm.initScript)
		return nil
	}
	if fpm.useSocket {
		// PHP-FPM refuses to start when the listen socket's parent dir is
		// missing, taking every other pool on the host down with it.
		if err := os.MkdirAll(fpm.socketDir, 0o755); err != nil {
			return fmt.Errorf("apache2: creating socket dir %s: %w", fpm.socketDir, err)
		}
	}
	content, err := p.renderPool(cfg, d, fpm)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(fpm.poolDir, 0o755); err != nil {
		return err
	}
	if err := writeIfChanged(poolFile, []byte(content), 0o644); err != nil {
		return fmt.Errorf("apache2: writing pool %s: %w", poolFile, err)
	}
	p.scheduleFPM(cfg, fpm.initScript)
	return nil
}

// deletePool removes the pool of a deleted site.
func (p *Plugin) deletePool(cfg *getconf.WebConfig, fpm fpmInfo) error {
	if fpm.poolDir == "" {
		return nil
	}
	poolFile := filepath.Join(fpm.poolDir, fpm.poolName+".conf")
	if err := os.Remove(poolFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("apache2: removing pool %s: %w", poolFile, err)
	}
	p.scheduleFPM(cfg, fpm.initScript)
	return nil
}

// scheduleFPM registers and requests a delayed reload/restart of an FPM unit.
func (p *Plugin) scheduleFPM(cfg *getconf.WebConfig, unit string) {
	if p.services == nil || unit == "" {
		return
	}
	action := engine.ActionRestart
	if cfg.PHPFPMReloadMode == "reload" {
		action = engine.ActionReload
	}
	p.services.Register(unit)
	p.services.RestartServiceDelayed(unit, action)
}
