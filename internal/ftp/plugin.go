// Package ftp implements the FTP-user plugin of the daemon (openspec change
// add-ftp-shell-module): the Go port of ISPConfig3's
// server/plugins-available/ftpuser_base_plugin.inc.php.
//
// FTP accounts are virtual (design D2): PureFTPd authenticates against the
// ftp_user table over MySQL, so this plugin never touches /etc/passwd. Its
// only job is the filesystem side of an account — make sure the login
// directory exists inside the website document root, and clean up the
// PureFTPd .ftpquota file when the directory changes or the account goes
// away.
package ftp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"gorm.io/gorm"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/getconf"
	"go-ispconfig/internal/system"
)

// quotaFile is the per-directory quota state file PureFTPd maintains.
const quotaFile = ".ftpquota"

// Plugin is the FTP user plugin. It subscribes to the ftp_user events
// announced by the web module.
type Plugin struct {
	db     *gorm.DB
	runner engine.CommandRunner
	log    *slog.Logger

	// LoadWeb returns the parent website row (nil when the site is gone) and
	// LoadWebConfig the [web] section of its server. Both default to the
	// database lookups below and are swapped out in tests.
	LoadWeb       func(domainID int64) (system.Row, error)
	LoadWebConfig func(serverID uint32) (*getconf.WebConfig, error)
}

// NewPlugin creates the FTP plugin; log nil means slog.Default.
func NewPlugin(db *gorm.DB, runner engine.CommandRunner, log *slog.Logger) *Plugin {
	if log == nil {
		log = slog.Default()
	}
	p := &Plugin{db: db, runner: runner, log: log}
	p.LoadWeb, p.LoadWebConfig = p.loadWebDomain, p.webConfig
	return p
}

// Name identifies the plugin in logs.
func (*Plugin) Name() string { return "ftp" }

// OnLoad subscribes to the three ftp_user events (port of
// ftpuser_base_plugin onLoad).
func (p *Plugin) OnLoad(r *engine.Registry) error {
	subs := []struct {
		event string
		fn    engine.EventFunc
	}{
		{"ftp_user_insert", p.ftpUserInsert},
		{"ftp_user_update", p.ftpUserUpdate},
		{"ftp_user_delete", p.ftpUserDelete},
	}
	for _, s := range subs {
		if err := r.RegisterEvent(s.event, s.fn); err != nil {
			return err
		}
	}
	return nil
}

// ftpUserInsert ensures the login directory of a new account exists.
func (p *Plugin) ftpUserInsert(ctx context.Context, _ string, data engine.Data) error {
	return p.ensureDir(ctx, system.Row(data.New))
}

// ftpUserUpdate ensures the login directory exists and drops the .ftpquota
// file left behind at the previous location when the directory moved.
func (p *Plugin) ftpUserUpdate(ctx context.Context, _ string, data engine.Data) error {
	if err := p.ensureDir(ctx, system.Row(data.New)); err != nil {
		return err
	}
	old, new := system.Row(data.Old), system.Row(data.New)
	if oldDir := old.Str("dir"); oldDir != "" && oldDir != new.Str("dir") {
		return p.removeQuotaFile(old)
	}
	return nil
}

// ftpUserDelete removes only the .ftpquota file: the account's files and the
// site content are never touched, and no system account exists to delete.
func (p *Plugin) ftpUserDelete(ctx context.Context, _ string, data engine.Data) error {
	_ = ctx
	old := system.Row(data.Old)
	if err := p.removeQuotaFile(old); err != nil {
		return err
	}
	p.log.Debug("ftp: user deleted", "username", old.Str("username"))
	return nil
}

// ensureDir creates the account's login directory when it does not exist
// yet, owned by the website's system user/group (port of the shared
// insert/update body of the PHP plugin). A directory outside the parent
// site's document root is refused: the daemon re-checks what the API
// already validated, because it acts as root.
func (p *Plugin) ensureDir(ctx context.Context, user system.Row) error {
	dir := user.Str("dir")
	if dir == "" {
		return nil
	}
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return nil
	}
	web, err := p.LoadWeb(user.Num("parent_domain_id"))
	if err != nil || web == nil {
		return err
	}
	docroot := web.Str("document_root")
	if !system.UnderDocroot(dir, docroot) {
		p.log.Warn("ftp: user dir is outside of docroot", "dir", dir, "document_root", docroot)
		return nil
	}
	cfg, err := p.LoadWebConfig(uint32(web.Num("server_id")))
	if err != nil {
		return err
	}

	p.log.Debug("ftp: user directory does not exist, creating it", "dir", dir)
	if err := p.unlockDocroot(ctx, cfg, docroot, false); err != nil {
		return err
	}
	mkErr := system.MkdirPath(ctx, p.runner, dir, 0o755, web.Str("system_user"), web.Str("system_group"))
	// Re-protect the docroot even when the mkdir failed: leaving it mutable
	// would silently weaken the site for every later run.
	if err := p.unlockDocroot(ctx, cfg, docroot, true); err != nil && mkErr == nil {
		mkErr = err
	}
	if mkErr != nil {
		return mkErr
	}
	p.log.Info("ftp: added ftpuser dir", "dir", dir)
	return nil
}

// removeQuotaFile deletes <dir>/.ftpquota when it exists. The directory is
// re-checked against the parent site's document root first, so a stale or
// tampered datalog payload can never make the root daemon unlink a file
// outside the website.
func (p *Plugin) removeQuotaFile(user system.Row) error {
	dir := user.Str("dir")
	if dir == "" {
		return nil
	}
	web, err := p.LoadWeb(user.Num("parent_domain_id"))
	if err != nil {
		return err
	}
	if web == nil {
		p.log.Warn("ftp: parent website is gone, keeping .ftpquota", "dir", dir)
		return nil
	}
	if !system.UnderDocroot(dir, web.Str("document_root")) {
		p.log.Warn("ftp: user dir is outside of docroot", "dir", dir,
			"document_root", web.Str("document_root"))
		return nil
	}
	path := filepath.Join(dir, quotaFile)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("ftp: removing %s: %w", path, err)
	}
	return nil
}

// unlockDocroot drops (protect=false) or restores (protect=true) the
// immutable flag of the document root, honouring the server's
// web_folder_protection setting.
func (p *Plugin) unlockDocroot(ctx context.Context, cfg *getconf.WebConfig, docroot string, protect bool) error {
	return system.WebFolderProtection(ctx, p.runner, p.log, docroot, protect,
		cfg != nil && cfg.WebFolderProtection == "y")
}

// loadWebDomain fetches the parent website row (nil when it no longer
// exists).
func (p *Plugin) loadWebDomain(domainID int64) (system.Row, error) {
	if domainID == 0 || p.db == nil {
		return nil, nil
	}
	var rec map[string]any
	err := p.db.Table("web_domain").Where("domain_id = ?", domainID).Take(&rec).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("ftp: loading web_domain %d: %w", domainID, err)
	}
	return rec, nil
}

// webConfig loads the [web] section of this server's config.
func (p *Plugin) webConfig(serverID uint32) (*getconf.WebConfig, error) {
	if p.db == nil {
		return &getconf.WebConfig{}, nil
	}
	cfg, err := getconf.GetServerConfig(p.db, serverID)
	if err != nil {
		return nil, fmt.Errorf("ftp: loading server %d web config: %w", serverID, err)
	}
	return &cfg.Web, nil
}
