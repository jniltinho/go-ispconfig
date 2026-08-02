package nginx

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/getconf"
	"go-ispconfig/internal/mastertpl"
)

// poolTemplate is the master template name for PHP-FPM pools.
const poolTemplate = "php_fpm_pool.conf.master"

// buildPool assembles the template variables and the custom-php.ini loop for
// php_fpm_pool.conf.master (port of php_fpm_pool_update's tpl vector).
func buildPool(cfg *getconf.WebConfig, d row, fpm fpmInfo) (map[string]any, []map[string]any) {
	listenGroup := cfg.Group
	if listenGroup == "" {
		listenGroup = "www-data"
	}
	docroot := d.str("document_root")
	openBasedir := d.str("php_open_basedir")
	if openBasedir == "" {
		openBasedir = docroot
	}

	vars := map[string]any{
		"fpm_pool":                fpm.poolName,
		"fpm_port":                fpm.port,
		"fpm_socket":              fpm.socketPath,
		"fpm_listen_mode":         "0660",
		"fpm_listen_user":         d.str("system_user"),
		"fpm_listen_group":        listenGroup,
		"fpm_user":                d.str("system_user"),
		"fpm_group":               d.str("system_group"),
		"fpm_domain":              d.str("domain"),
		"domain":                  d.str("domain"),
		"pm":                      d.str("pm"),
		"pm_max_children":         d.num("pm_max_children"),
		"pm_start_servers":        d.num("pm_start_servers"),
		"pm_min_spare_servers":    d.num("pm_min_spare_servers"),
		"pm_max_spare_servers":    d.num("pm_max_spare_servers"),
		"pm_process_idle_timeout": d.num("pm_process_idle_timeout"),
		"pm_max_requests":         d.num("pm_max_requests"),
		"document_root":           docroot,
		"security_level":          cfg.SecurityLevel,
		"php_open_basedir":        openBasedir,
	}
	if fpm.useSocket {
		vars["use_tcp"], vars["use_socket"] = 0, 1
	} else {
		vars["use_tcp"], vars["use_socket"] = 1, 0
	}
	if openBasedir != "" {
		vars["enable_php_open_basedir"] = ""
	} else {
		vars["enable_php_open_basedir"] = ";"
	}

	// Chrooted PHP-FPM: chroot dir is the docroot, open_basedir/document_root
	// become chroot-relative (port of the chroot block; jailkit setup itself
	// is out of scope, this only shapes the pool file).
	if d.str("php_fpm_chroot") == "y" {
		vars["php_fpm_chroot"] = "y"
		vars["php_fpm_chroot_dir"] = docroot
		vars["php_fpm_chroot_web_folder"] = "/" + strings.Trim(webFolderOf(d), "/")
		vars["php_open_basedir"] = strings.ReplaceAll(openBasedir, docroot, "")
		vars["document_root"] = ""
	}

	settings, customSession, customSendmail := customPHPIni(d.str("custom_php_ini"), docroot)
	vars["custom_session_save_path"] = yn(customSession)
	vars["custom_sendmail_path"] = yn(customSendmail)
	return vars, settings
}

// booleanIniValues are the php.ini value spellings PHP-FPM wants expressed as
// php_admin_flag rather than php_admin_value.
var booleanIniValues = map[string]bool{
	"1": true, "on": true, "off": true, "true": true, "false": true, "yes": true, "no": true,
}

// customPHPIni ports the custom_php_ini parsing: it turns the site's raw
// php.ini text into php_admin_flag/php_admin_value loop rows, substitutes
// {WEBROOT}, and reports whether session.save_path and sendmail_path were
// overridden (so the template drops its own defaults).
func customPHPIni(raw, docroot string) (rows []map[string]any, customSession, customSendmail bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, false, false
	}
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\r", "\n")
	raw = strings.ReplaceAll(raw, "{WEBROOT}", docroot+"/web")
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if key == "session.save_path" {
			customSession = true
		}
		if key == "sendmail_path" {
			customSendmail = true
		}
		v := value
		if strings.EqualFold(v, "0") {
			// PHP-FPM rejects 0 as a boolean; map to off (PHP parity).
			v = "off"
		}
		if booleanIniValues[strings.ToLower(v)] {
			rows = append(rows, map[string]any{"ini_setting": "php_admin_flag[" + key + "] = " + v})
		} else {
			rows = append(rows, map[string]any{"ini_setting": "php_admin_value[" + key + "] = " + value})
		}
	}
	return rows, customSession, customSendmail
}

// poolServerPHP picks the server_php override that locates a domain's pool:
// the new pinned version for a PHP-FPM site, the old one for a site
// switching away from PHP-FPM (so its stale pool is removed from the right
// dir), matching php_fpm_pool_update.
func poolServerPHP(newPHP row, newRow, oldRow, oldPHP row) row {
	if newRow.str("php") == "php-fpm" || newRow.str("php") == "fast-cgi" {
		return newPHP
	}
	if oldRow.str("php") != "no" && oldRow.str("php") != "" {
		return oldPHP
	}
	return nil
}

// renderPool renders the pool file for one domain.
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
		return "", fmt.Errorf("nginx: rendering %s: %w", poolTemplate, err)
	}
	return out, nil
}

// reloadAction maps the php_fpm_reload_mode config to a service action
// (default restart, PHP parity).
func reloadAction(cfg *getconf.WebConfig) string {
	if cfg.PHPFPMReloadMode == "reload" {
		return engine.ActionReload
	}
	return engine.ActionRestart
}

// scheduleFPM registers and requests a delayed reload/restart of one FPM unit.
func (p *Plugin) scheduleFPM(unit, action string) {
	if p.services == nil || unit == "" {
		return
	}
	p.services.Register(unit)
	p.services.RestartServiceDelayed(unit, action)
}

// managePool is the pool lifecycle for one web_domain apply (port of
// php_fpm_pool_update): writes the pool for a php-fpm site (or removes it for
// a non-fpm site), prunes the same pool name from every OTHER PHP version's
// pool dir, and schedules a delayed reload of each affected FPM version.
func (p *Plugin) managePool(ctx context.Context, cfg *getconf.WebConfig, d, oldRow row, fpm fpmInfo) error {
	action := reloadAction(cfg)
	poolFile := filepath.Join(fpm.poolDir, fpm.poolName+".conf")

	php := d.str("php")
	if php != "php-fpm" && php != "fast-cgi" {
		// Site switched away from PHP-FPM: drop its pool and reload the old
		// version that owned it.
		if err := removeFileIfExists(poolFile); err != nil {
			return err
		}
		if oldRow.str("php") != "no" && oldRow.str("php") != "" {
			p.scheduleFPM(fpm.initScript, action)
		}
		return p.prunePool(ctx, cfg, d.num("server_id"), fpm, action)
	}

	if fpm.useSocket {
		if err := os.MkdirAll(fpm.socketDir, 0o755); err != nil {
			return fmt.Errorf("nginx: creating socket dir %s: %w", fpm.socketDir, err)
		}
	}
	content, err := p.renderPool(cfg, d, fpm)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(fpm.poolDir, 0o755); err != nil {
		return fmt.Errorf("nginx: creating pool dir %s: %w", fpm.poolDir, err)
	}
	if err := os.WriteFile(poolFile, []byte(content), 0o644); err != nil {
		return fmt.Errorf("nginx: writing pool file %s: %w", poolFile, err)
	}
	p.log.Info("nginx: wrote php-fpm pool", "file", poolFile)

	if err := p.prunePool(ctx, cfg, d.num("server_id"), fpm, action); err != nil {
		return err
	}
	// Reload the current version last (PHP reloads it after the others).
	p.scheduleFPM(fpm.initScript, action)
	return nil
}

// prunePool removes the pool name from every server PHP version whose pool
// dir differs from the active one and schedules a reload of each version that
// actually had a stale file (port of the "delete pool in all other PHP
// versions" loop, including the server default).
func (p *Plugin) prunePool(ctx context.Context, cfg *getconf.WebConfig, serverID int64, fpm fpmInfo, action string) error {
	seen := map[string]bool{fpm.poolDir: true}

	prune := func(dir, unit string) error {
		dir = strings.TrimSuffix(strings.TrimSpace(dir), "/")
		if dir == "" || seen[dir] {
			return nil
		}
		seen[dir] = true
		stale := filepath.Join(dir, fpm.poolName+".conf")
		if _, err := os.Stat(stale); err == nil {
			if err := os.Remove(stale); err != nil {
				return fmt.Errorf("nginx: removing stale pool %s: %w", stale, err)
			}
			p.log.Info("nginx: removed stale php-fpm pool", "file", stale)
			p.scheduleFPM(unit, action)
		}
		return nil
	}

	if err := prune(cfg.PHPFPMPoolDir, cfg.PHPFPMInitScript); err != nil {
		return err
	}
	versions, err := p.serverPHPVersions(serverID)
	if err != nil {
		return err
	}
	for _, v := range versions {
		if err := prune(v.str("php_fpm_pool_dir"), v.str("php_fpm_init_script")); err != nil {
			return err
		}
	}
	return nil
}

// serverPHPVersions returns the usable server_php rows of a server (those
// with pool/ini/init-script set), used for pruning stale pools across
// versions.
func (p *Plugin) serverPHPVersions(serverID int64) ([]row, error) {
	if p.db == nil {
		return nil, nil
	}
	var recs []map[string]any
	err := p.db.Table("server_php").
		Where("php_fpm_init_script != '' AND php_fpm_ini_dir != '' AND php_fpm_pool_dir != '' AND server_id = ?", serverID).
		Find(&recs).Error
	if err != nil {
		return nil, fmt.Errorf("nginx: loading server_php versions: %w", err)
	}
	rows := make([]row, len(recs))
	for i, r := range recs {
		rows[i] = r
	}
	return rows, nil
}

// poolDelete removes a deleted site's pool from its PHP version's dir and
// prunes stale copies from the other versions, scheduling a reload of each
// affected version (port of php_fpm_pool_delete).
func (p *Plugin) poolDelete(ctx context.Context, cfg *getconf.WebConfig, old row) error {
	var serverPHP row
	if old.num("server_php_id") != 0 && old.str("php") != "no" {
		php, err := p.loadServerPHP(old.num("server_php_id"))
		if err != nil {
			return err
		}
		serverPHP = php
	}
	fpm := resolveFPM(cfg, old, serverPHP)
	action := reloadAction(cfg)
	if err := removeFileIfExists(filepath.Join(fpm.poolDir, fpm.poolName+".conf")); err != nil {
		return err
	}
	if err := p.prunePool(ctx, cfg, old.num("server_id"), fpm, action); err != nil {
		return err
	}
	p.scheduleFPM(fpm.initScript, action)
	return nil
}

// removeFileIfExists removes a file, ignoring a missing one.
func removeFileIfExists(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("nginx: removing %s: %w", path, err)
	}
	return nil
}
