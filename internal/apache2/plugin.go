// Package apache2 renders and applies Apache 2.4 configuration for the
// websites go-ispconfig manages. It is the Apache-side sibling of
// internal/nginx: it subscribes to the same named events raised by the web
// module and turns web_domain rows into vhosts, PHP-FPM pools and folder
// auth files.
//
// Only one of the two plugins is ever registered on a server —
// getconf.WebConfig.ServerType decides — because nginx and Apache cannot
// both own port 80.
package apache2

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gorm.io/gorm"

	"go-ispconfig/internal/acme"
	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/getconf"
)

// DefaultLogBaseDir is where per-domain Apache logs live (ISPConfig layout,
// referenced by the vhost template's ErrorLog lines).
const DefaultLogBaseDir = "/var/log/ispconfig/httpd"

// ServiceName is the services-registry key of the Apache unit.
const ServiceName = "apache2"

// Plugin is the Apache server plugin.
type Plugin struct {
	db       *gorm.DB
	services *engine.Services
	runner   engine.CommandRunner
	log      *slog.Logger

	// customTplDir is the mastertpl override directory (conf-custom parity).
	customTplDir string
	// logBaseDir is DefaultLogBaseDir in production, a temp dir in tests.
	logBaseDir string
	// apacheVersion caches the `apache2ctl -v` probe (preset in tests).
	apacheVersion string
	// serverID is this node's server row (set by the daemon).
	serverID uint32
	// leWebroot overrides the ACME challenge webroot in tests.
	leWebroot string
	acmeMgr   *acme.Manager
	leIssue   func(mainDomain, keyFile, crtFile string) (bool, error)
	leHTTPGet func(url string) (string, error)
}

// NewPlugin creates the Apache plugin. customTplDir may be empty (embedded
// templates only); log nil means slog.Default.
func NewPlugin(db *gorm.DB, services *engine.Services, runner engine.CommandRunner, customTplDir string, log *slog.Logger) *Plugin {
	if log == nil {
		log = slog.Default()
	}
	return &Plugin{
		db:           db,
		services:     services,
		runner:       runner,
		log:          log,
		customTplDir: customTplDir,
		logBaseDir:   DefaultLogBaseDir,
	}
}

// SetServerID records this node's server id for ACME account paths.
func (p *Plugin) SetServerID(id uint32) { p.serverID = id }

// Name identifies the plugin in logs.
func (*Plugin) Name() string { return "apache2" }

// OnLoad subscribes the plugin to the web module events.
func (p *Plugin) OnLoad(r *engine.Registry) error {
	subs := []struct {
		event string
		fn    engine.EventFunc
	}{
		{"web_domain_insert", p.webDomainInsert},
		{"web_domain_update", p.webDomainUpdate},
		{"web_domain_delete", p.webDomainDelete},
		{"web_folder_update", p.webFolder},
		{"web_folder_delete", p.webFolderDelete},
		{"web_folder_user_insert", p.webFolderUser},
		{"web_folder_user_update", p.webFolderUser},
		{"web_folder_user_delete", p.webFolderUser},
	}
	for _, s := range subs {
		if err := r.RegisterEvent(s.event, s.fn); err != nil {
			return err
		}
	}
	return nil
}

// webConfig loads the [web] section of this server's config.
func (p *Plugin) webConfig(serverID uint32) (*getconf.WebConfig, error) {
	cfg, err := getconf.GetServerConfig(p.db, serverID)
	if err != nil {
		return nil, fmt.Errorf("apache2: loading server %d web config: %w", serverID, err)
	}
	return &cfg.Web, nil
}

func (p *Plugin) webDomainInsert(ctx context.Context, _ string, data engine.Data) error {
	return p.apply(ctx, "insert", row(data.Old), row(data.New))
}

func (p *Plugin) webDomainUpdate(ctx context.Context, _ string, data engine.Data) error {
	return p.apply(ctx, "update", row(data.Old), row(data.New))
}

// apply provisions the site directory tree, then renders and activates the
// vhost of one web_domain row.
func (p *Plugin) apply(ctx context.Context, action string, oldRow, newRow row) error {
	if !isVhostType(newRow.str("type")) {
		return nil
	}
	domain := newRow.str("domain")
	if err := safeDomain(domain); err != nil {
		return err
	}
	cfg, err := p.webConfig(uint32(newRow.num("server_id")))
	if err != nil {
		return err
	}
	docroot := strings.TrimSuffix(newRow.str("document_root"), "/")
	if err := safeSitePath(docroot, cfg.WebsiteBasedir); err != nil {
		return err
	}
	// The document root, its system user/group and the website symlinks must
	// exist before the vhost pointing at them is written: Apache refuses to
	// start on a SuexecUserGroup naming an unknown account.
	if err := p.ensureSite(ctx, cfg, action, oldRow, newRow); err != nil {
		return err
	}

	leWarnings := p.maybeRequestLE(ctx, oldRow, newRow, cfg)

	sslOn := newRow.str("ssl") == "y" && sslUsable(newRow)
	crt, _, bundle := sslPaths(newRow)
	in := vhostInput{
		cfg:           cfg,
		d:             newRow,
		apacheVersion: p.probeVersion(ctx),
		ips:           defaultListeners(newRow, sslOn),
		sslOCSP:       sslOn && p.probeOCSP(ctx, crt),
		sslBundle:     fileNonEmpty(bundle),
	}
	if in.aliases, err = p.loadAliases(newRow.num("domain_id")); err != nil {
		return err
	}
	// The row's own ssl flag drives the template; a site whose certificate
	// files are missing must render as plain HTTP or Apache refuses to start.
	if !sslOn {
		in.d = withSSLOff(newRow)
	}

	content, fpm, err := renderVhost(in, p.customTplDir)
	if err != nil {
		return err
	}
	// The pool must exist before the vhost that proxies to it is activated.
	if err := p.managePool(cfg, newRow, fpm); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(p.logBaseDir, domain), 0o755); err != nil {
		return fmt.Errorf("apache2: creating log dir: %w", err)
	}
	if err := p.activate(ctx, cfg, domain, content, newRow.str("active") != "n"); err != nil {
		return errors.Join(append(leWarnings, err)...)
	}
	return errors.Join(leWarnings...)
}

// webDomainDelete removes the vhost, its activation symlink and its pool.
func (p *Plugin) webDomainDelete(ctx context.Context, _ string, data engine.Data) error {
	old := row(data.Old)
	if !isVhostType(old.str("type")) {
		return nil
	}
	domain := old.str("domain")
	if err := safeDomain(domain); err != nil {
		return err
	}
	cfg, err := p.webConfig(uint32(old.num("server_id")))
	if err != nil {
		return err
	}
	for _, path := range []string{
		filepath.Join(cfg.VhostConfDir, vhostFileName(domain)),
		filepath.Join(cfg.VhostConfEnabledDir, vhostFileName(domain)),
	} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("apache2: removing %s: %w", path, err)
		}
	}
	if err := p.deletePool(cfg, resolveFPM(cfg, old, nil)); err != nil {
		return err
	}
	return p.reload()
}

// withSSLOff returns a shallow copy of d with the ssl flag cleared, so a
// site whose certificate is missing renders its plain vhost instead of one
// Apache would reject.
func withSSLOff(d row) row {
	out := make(row, len(d)+1)
	for k, v := range d {
		out[k] = v
	}
	out["ssl"] = "n"
	return out
}

// loadAliases reads the active alias/subdomain children of a domain.
func (p *Plugin) loadAliases(parentID int64) ([]row, error) {
	if p.db == nil || parentID == 0 {
		return nil, nil
	}
	var raw []map[string]any
	err := p.db.Table("web_domain").
		Where("parent_domain_id = ? AND type IN ('alias','subdomain') AND active = 'y'", parentID).
		Find(&raw).Error
	if err != nil {
		return nil, fmt.Errorf("apache2: loading aliases of domain %d: %w", parentID, err)
	}
	out := make([]row, 0, len(raw))
	for _, r := range raw {
		out = append(out, row(r))
	}
	return out, nil
}

// activate writes the vhost, validates the whole Apache config and reloads.
// A rejected config is rolled back to the previous file before returning, so
// a bad row can never take the web server down.
func (p *Plugin) activate(ctx context.Context, cfg *getconf.WebConfig, domain, content string, active bool) error {
	if cfg.VhostConfDir == "" || cfg.VhostConfEnabledDir == "" {
		return fmt.Errorf("apache2: vhost_conf_dir / vhost_conf_enabled_dir are not configured")
	}
	for _, dir := range []string{cfg.VhostConfDir, cfg.VhostConfEnabledDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	vhostFile := filepath.Join(cfg.VhostConfDir, vhostFileName(domain))
	link := filepath.Join(cfg.VhostConfEnabledDir, vhostFileName(domain))

	backup := vhostFile + "." + randomSuffix() + "~"
	hadPrevious := false
	if prev, err := os.ReadFile(vhostFile); err == nil {
		if string(prev) == content && linkOK(link, vhostFile, active) {
			return nil // nothing changed: no write, no reload
		}
		if err := os.WriteFile(backup, prev, 0o644); err != nil {
			return fmt.Errorf("apache2: backing up %s: %w", vhostFile, err)
		}
		hadPrevious = true
	}
	defer func() {
		if hadPrevious {
			_ = os.Remove(backup)
		}
	}()

	if err := os.WriteFile(vhostFile, []byte(content), 0o644); err != nil {
		return fmt.Errorf("apache2: writing %s: %w", vhostFile, err)
	}
	if err := setLink(link, vhostFile, active); err != nil {
		return err
	}

	if err := p.configTest(ctx, cfg); err != nil {
		p.rollback(vhostFile, backup, link, hadPrevious)
		return fmt.Errorf("apache2: config test rejected the vhost of %s (previous config restored): %w", domain, err)
	}
	return p.reload()
}

// linkOK reports whether the activation symlink already matches the wanted
// state.
func linkOK(link, target string, active bool) bool {
	dest, err := os.Readlink(link)
	if !active {
		return err != nil
	}
	return err == nil && dest == target
}

// setLink creates or removes the sites-enabled symlink.
func setLink(link, target string, active bool) error {
	if !active {
		if err := os.Remove(link); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("apache2: removing %s: %w", link, err)
		}
		return nil
	}
	if dest, err := os.Readlink(link); err == nil && dest == target {
		return nil
	}
	if err := os.Remove(link); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("apache2: replacing %s: %w", link, err)
	}
	if err := os.Symlink(target, link); err != nil {
		return fmt.Errorf("apache2: linking %s: %w", link, err)
	}
	return nil
}

// rollback restores the previous vhost after a failed config test. A site
// that had no previous vhost is deactivated instead — leaving a rejected
// file linked would keep Apache from restarting later.
func (p *Plugin) rollback(vhostFile, backup, link string, hadPrevious bool) {
	if !hadPrevious {
		if err := os.Remove(link); err != nil && !os.IsNotExist(err) {
			p.log.Error("apache2: rollback could not remove the activation link", "link", link, "error", err)
		}
		if err := os.Remove(vhostFile); err != nil && !os.IsNotExist(err) {
			p.log.Error("apache2: rollback could not remove the vhost", "file", vhostFile, "error", err)
		}
		return
	}
	prev, err := os.ReadFile(backup)
	if err != nil {
		p.log.Error("apache2: rollback could not read the backup", "file", backup, "error", err)
		return
	}
	if err := os.WriteFile(vhostFile, prev, 0o644); err != nil {
		p.log.Error("apache2: rollback could not restore the vhost", "file", vhostFile, "error", err)
	}
}

// configTest runs the distro's config check (check_apache_config, default
// "apache2ctl -t"). An empty setting disables the check, matching the PHP
// plugin's opt-out.
func (p *Plugin) configTest(ctx context.Context, cfg *getconf.WebConfig) error {
	cmd := strings.TrimSpace(cfg.CheckApacheConfig)
	if cmd == "n" || cmd == "no" {
		return nil
	}
	name, args := "apache2ctl", []string{"-t"}
	if cmd != "" && cmd != "y" && cmd != "yes" {
		fields := strings.Fields(cmd)
		name, args = fields[0], fields[1:]
	}
	out, err := p.runner.Run(ctx, name, args...)
	if err != nil {
		return fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// reload requests a delayed reload of the Apache unit through the services
// registry, so a datalog cycle touching many sites reloads once.
func (p *Plugin) reload() error {
	if p.services == nil {
		return nil
	}
	p.services.Register(ServiceName)
	p.services.RestartServiceDelayed(ServiceName, engine.ActionReload)
	return nil
}

// apacheVersionRe extracts the version from `apache2ctl -v` output
// ("Server version: Apache/2.4.58 (Debian)").
var apacheVersionRe = regexp.MustCompile(`Apache/(\d+(?:\.\d+)*)`)

// probeVersion returns the running Apache version, caching the probe. On a
// failed probe it returns 2.4: the template's <tmpl_else> branches emit the
// Apache 2.2 "Order allow,deny" syntax, which a 2.4 server without
// mod_access_compat rejects outright — guessing modern is the safe default.
func (p *Plugin) probeVersion(ctx context.Context) string {
	if p.apacheVersion != "" {
		return p.apacheVersion
	}
	out, err := p.runner.Run(ctx, "apache2ctl", "-v")
	if m := apacheVersionRe.FindSubmatch(out); err == nil && m != nil {
		p.apacheVersion = string(m[1])
	} else {
		p.log.Warn("apache2: version probe failed, assuming 2.4", "error", err)
		p.apacheVersion = "2.4"
	}
	return p.apacheVersion
}
