// Package jailkit is the jailkit plugin of the daemon: the Go port of
// ISPConfig3's server/plugins-available/shelluser_jailkit_plugin.inc.php
// (openspec change add-ftp-shell-module, tasks 4.x).
//
// It rides the same shell_user_* events as the base shell plugin and only
// acts when chroot = "jailkit". The base plugin creates the OS account and
// parks it on /bin/false; this plugin builds the chroot, relocates the
// account into it and hands it /usr/sbin/jk_chrootsh.
package jailkit

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"go-ispconfig/internal/auth"
	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/getconf"
	"go-ispconfig/internal/system"
)

// minUID is the floor for a UID this plugin may act on (PHP $min_uid).
const minUID = 499

// jkChrootShell is the login shell of every active jailkit account.
const jkChrootShell = "/usr/sbin/jk_chrootsh"

// Plugin is the jailkit shell-user plugin.
type Plugin struct {
	db     *gorm.DB
	runner engine.CommandRunner
	log    *slog.Logger

	// Seams the tests replace.
	LoadWeb        func(domainID int64) (system.Row, error)
	LoadJailkitCfg func(serverID uint32) (getconf.JailkitConfig, error)
	LoadWebConfig  func(serverID uint32) (*getconf.WebConfig, error)
	AllowShellUser func() (bool, error)
	LookupUID      func(username string) (int, bool)
	LookupGID      func(groupname string) (int, bool)
	// StampHash writes last_jailkit_update/hash for every site sharing a
	// document_root; ClearHash nulls the hash when the jail is torn down.
	StampHash func(docroot, hash string) error
	ClearHash func(docroot string) error
	// ListWebFolders returns web_folder values of subdomains that share a
	// document root (used as skip= options on update/delete).
	ListWebFolders func(parentDomainID int64, docroot string, serverID uint32) ([]string, error)
	// JailkitInUse reports whether any remaining jailkit shell_user or
	// chrooted cron job still needs the jail of a website.
	JailkitInUse func(parentDomainID int64) (bool, error)
	// PHPFPMChroot reports whether the parent site's php-fpm pool is itself
	// chrooted into the same tree (which blocks unused-jail teardown).
	PHPFPMChroot func(web system.Row) bool
	// RootAuthorizedKeys is the admin key seeded into a fresh authorized_keys
	// file; empty in tests so host /root keys never leak into fixtures.
	RootAuthorizedKeys string
}

// NewPlugin creates the jailkit plugin; log nil means slog.Default.
func NewPlugin(db *gorm.DB, runner engine.CommandRunner, log *slog.Logger) *Plugin {
	if log == nil {
		log = slog.Default()
	}
	p := &Plugin{db: db, runner: runner, log: log}
	p.LoadWeb = p.loadWebDomain
	p.LoadJailkitCfg = p.loadJailkitCfg
	p.LoadWebConfig = p.webConfig
	p.AllowShellUser = p.allowShellUser
	p.LookupUID, p.LookupGID = lookupUID, lookupGID
	p.StampHash, p.ClearHash = p.stampHash, p.clearHash
	p.ListWebFolders = p.listWebFolders
	p.JailkitInUse = p.jailkitInUse
	p.PHPFPMChroot = func(web system.Row) bool { return web.Str("php_fpm_chroot") == "y" }
	p.RootAuthorizedKeys = "/root/.ssh/authorized_keys"
	return p
}

// Name identifies the plugin in logs.
func (*Plugin) Name() string { return "jailkit" }

// OnLoad subscribes to the shell_user events (port of
// shelluser_jailkit_plugin onLoad). Registration order after the base shell
// plugin is required so the OS account already exists.
func (p *Plugin) OnLoad(r *engine.Registry) error {
	for _, s := range []struct {
		event string
		fn    engine.EventFunc
	}{
		{"shell_user_insert", p.shellUserInsert},
		{"shell_user_update", p.shellUserUpdate},
		{"shell_user_delete", p.shellUserDelete},
	} {
		if err := r.RegisterEvent(s.event, s.fn); err != nil {
			return err
		}
	}
	return nil
}

// shellUserInsert builds the jail and relocates a new jailkit account.
func (p *Plugin) shellUserInsert(ctx context.Context, _ string, data engine.Data) error {
	u := system.Row(data.New)
	if u.Str("chroot") != "jailkit" {
		return nil
	}
	return p.setup(ctx, u, system.Row(data.Old), true)
}

// shellUserUpdate refreshes the jail / relocates the account when chroot is
// jailkit. A non-jailkit update is a no-op here (the base plugin owns it).
func (p *Plugin) shellUserUpdate(ctx context.Context, _ string, data engine.Data) error {
	u := system.Row(data.New)
	if u.Str("chroot") != "jailkit" {
		return nil
	}
	return p.setup(ctx, u, system.Row(data.Old), false)
}

// setup is the shared insert/update body: guards, config merge, chroot
// create/update, jailed user, ssh keys, php symlink, unlock (insert only).
func (p *Plugin) setup(ctx context.Context, u, old system.Row, unlock bool) error {
	username := u.Str("username")
	allowed, err := p.AllowShellUser()
	if err != nil {
		return err
	}
	if !allowed {
		p.log.Warn("jailkit: plugin disabled by security settings", "username", username)
		return nil
	}
	if !p.checkUser(u) {
		return nil
	}

	// Parent user must exist with a non-system UID; the shell user itself
	// must already be in /etc/passwd (base plugin insert ran first).
	puser := u.Str("puser")
	uid, ok := p.LookupUID(puser)
	if !ok {
		p.log.Warn("jailkit: parent user does not exist", "username", username, "puser", puser)
		return nil
	}
	if uid <= minUID {
		return fmt.Errorf("jailkit: uid %d of parent %s is a system account", uid, puser)
	}
	if _, ok := p.LookupUID(username); !ok {
		p.log.Warn("jailkit: shell user does not exist in /etc/passwd, skipping",
			"username", username)
		return nil
	}

	web, err := p.LoadWeb(u.Num("parent_domain_id"))
	if err != nil || web == nil {
		return err
	}
	serverID := uint32(web.Num("server_id"))
	jkServer, err := p.LoadJailkitCfg(serverID)
	if err != nil {
		return err
	}
	cfg := MergeConfig(jkServer, web)

	docroot := web.Str("document_root")
	webCfg, err := p.LoadWebConfig(serverID)
	if err != nil {
		return err
	}
	if err := p.securityLevel(ctx, webCfg, docroot); err != nil {
		return err
	}
	if err := system.WebFolderProtection(ctx, p.runner, p.log, docroot, false,
		webCfg != nil && webCfg.WebFolderProtection == "y"); err != nil {
		return err
	}

	setupErr := error(nil)
	if err := p.setupChroot(ctx, u, web, cfg, serverID); err != nil {
		setupErr = err
	} else if err := p.addUser(ctx, u, old, cfg); err != nil {
		setupErr = err
	} else if err := p.setupSSHRSA(ctx, u, old, cfg); err != nil {
		setupErr = err
	} else if err := p.setupShellPHP(ctx, u, web, cfg); err != nil {
		setupErr = err
	} else if unlock {
		// Insert path: force jk_chrootsh (base plugin left /bin/false) and unlock.
		if out, err := p.runner.Run(ctx, "usermod", "-s", jkChrootShell, username); err != nil {
			setupErr = fmt.Errorf("jailkit: usermod shell %s: %w: %s", username, err, out)
		} else if out, err := p.runner.Run(ctx, "usermod", "-U", username); err != nil {
			// Unlock is best-effort: a never-locked account fails usermod -U.
			p.log.Debug("jailkit: usermod -U", "username", username, "err", err, "output", string(out))
		}
	}

	if err := p.securityLevel(ctx, webCfg, docroot); err != nil && setupErr == nil {
		setupErr = err
	}
	if err := system.WebFolderProtection(ctx, p.runner, p.log, docroot, true,
		webCfg != nil && webCfg.WebFolderProtection == "y"); err != nil && setupErr == nil {
		setupErr = err
	}
	if setupErr == nil {
		p.log.Info("jailkit: configured jailkit user", "username", username, "dir", u.Str("dir"))
	}
	return setupErr
}

// checkUser ports the guard block shared by the PHP insert/update paths.
func (p *Plugin) checkUser(u system.Row) bool {
	dir, username := u.Str("dir"), u.Str("username")
	if !isAllowedName(username, false) ||
		!isAllowedName(u.Str("puser"), true) ||
		!isAllowedGroup(u.Str("pgroup"), p.LookupGID) {
		p.log.Warn("jailkit: user must not be root or in group root",
			"username", username, "puser", u.Str("puser"), "pgroup", u.Str("pgroup"))
		return false
	}
	// Parent user name must match web\d+ and parent group client\d+.
	if !webNameRe.MatchString(u.Str("puser")) || !clientNameRe.MatchString(u.Str("pgroup")) {
		p.log.Warn("jailkit: parent user/group name rejected",
			"puser", u.Str("puser"), "pgroup", u.Str("pgroup"))
		return false
	}
	if info, err := os.Lstat(dir); err == nil && !info.IsDir() {
		p.log.Warn("jailkit: user dir must not be an existing file or symlink",
			"username", username, "dir", dir)
		return false
	}
	if !system.IsAllowedPath(dir) {
		p.log.Warn("jailkit: user dir is not an allowed path", "username", username, "dir", dir)
		return false
	}
	return true
}

var (
	nameRe        = regexp.MustCompile(`^[a-zA-Z0-9.\-_]{1,32}$`)
	webNameRe     = regexp.MustCompile(`^web\d+$`)
	clientNameRe  = regexp.MustCompile(`^client\d+$`)
	nameBlacklist = map[string]bool{"root": true, "ispconfig": true, "vmail": true, "getmail": true}
)

func isAllowedName(name string, checkPattern bool) bool {
	if nameBlacklist[name] || !nameRe.MatchString(name) {
		return false
	}
	_ = checkPattern
	return true
}

func isAllowedGroup(name string, lookup func(string) (int, bool)) bool {
	if nameBlacklist[name] || !nameRe.MatchString(name) {
		return false
	}
	gid, ok := lookup(name)
	return ok && gid > minUID && clientNameRe.MatchString(name)
}

// securityLevel re-asserts root:root 0755 on the document root when the
// server security_level is high (20) — port of _update_website_security_level.
func (p *Plugin) securityLevel(ctx context.Context, webCfg *getconf.WebConfig, docroot string) error {
	if webCfg == nil || webCfg.SecurityLevel != "20" || docroot == "" {
		return nil
	}
	if err := system.WebFolderProtection(ctx, p.runner, p.log, docroot, false, true); err != nil {
		return err
	}
	if err := os.Chmod(docroot, 0o755); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("jailkit: chmod %s: %w", docroot, err)
	}
	if err := system.Chown(ctx, p.runner, docroot, "root", "root", false); err != nil {
		return err
	}
	return system.WebFolderProtection(ctx, p.runner, p.log, docroot, true, true)
}

// allowShellUser reads the allow_shell_user security policy flag.
func (p *Plugin) allowShellUser() (bool, error) {
	if p.db == nil {
		return true, nil
	}
	value, err := auth.GetPolicy(p.db, "allow_shell_user")
	if err != nil {
		return false, fmt.Errorf("jailkit: loading allow_shell_user: %w", err)
	}
	return value == "yes", nil
}

// loadWebDomain fetches the parent website plus the optional server_php
// columns the jailkit plugin needs (php_jk_section, php_cli_binary).
func (p *Plugin) loadWebDomain(domainID int64) (system.Row, error) {
	if domainID == 0 || p.db == nil {
		return nil, nil
	}
	var rec map[string]any
	err := p.db.Table("web_domain").
		Select("web_domain.*, server_php.php_cli_binary, server_php.php_jk_section").
		Joins("LEFT JOIN server_php ON web_domain.server_php_id = server_php.server_php_id").
		Where("web_domain.domain_id = ?", domainID).
		Take(&rec).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("jailkit: loading web_domain %d: %w", domainID, err)
	}
	return rec, nil
}

func (p *Plugin) loadJailkitCfg(serverID uint32) (getconf.JailkitConfig, error) {
	if p.db == nil {
		return getconf.DefaultJailkitConfig(), nil
	}
	cfg, err := getconf.GetServerConfig(p.db, serverID)
	if err != nil {
		return getconf.JailkitConfig{}, fmt.Errorf("jailkit: loading server %d config: %w", serverID, err)
	}
	return cfg.Jailkit, nil
}

func (p *Plugin) webConfig(serverID uint32) (*getconf.WebConfig, error) {
	if p.db == nil {
		return &getconf.WebConfig{}, nil
	}
	cfg, err := getconf.GetServerConfig(p.db, serverID)
	if err != nil {
		return nil, fmt.Errorf("jailkit: loading server %d web config: %w", serverID, err)
	}
	return &cfg.Web, nil
}

func (p *Plugin) stampHash(docroot, hash string) error {
	if p.db == nil || docroot == "" {
		return nil
	}
	return p.db.Exec(
		"UPDATE web_domain SET last_jailkit_update = NOW(), last_jailkit_hash = ? WHERE document_root = ?",
		hash, docroot,
	).Error
}

func (p *Plugin) clearHash(docroot string) error {
	if p.db == nil || docroot == "" {
		return nil
	}
	return p.db.Exec(
		"UPDATE web_domain SET last_jailkit_update = NOW(), last_jailkit_hash = NULL WHERE document_root = ?",
		docroot,
	).Error
}

func (p *Plugin) listWebFolders(parentDomainID int64, docroot string, serverID uint32) ([]string, error) {
	if p.db == nil {
		return nil, nil
	}
	var folders []string
	err := p.db.Table("web_domain").
		Where("parent_domain_id = ? AND document_root = ? AND server_id = ? AND web_folder != '' AND web_folder IS NOT NULL",
			parentDomainID, docroot, serverID).
		Pluck("web_folder", &folders).Error
	return folders, err
}

func (p *Plugin) jailkitInUse(parentDomainID int64) (bool, error) {
	if p.db == nil {
		return false, nil
	}
	var count int64
	if err := p.db.Table("shell_user").
		Where("parent_domain_id = ? AND chroot = ?", parentDomainID, "jailkit").
		Count(&count).Error; err != nil {
		return false, err
	}
	if count > 0 {
		return true, nil
	}
	// cron.type = 'chrooted' also pins the jail (if the table exists).
	if err := p.db.Table("cron").
		Where("parent_domain_id = ? AND type = ?", parentDomainID, "chrooted").
		Count(&count).Error; err != nil {
		// Missing cron table is fine on a partial schema; treat as unused.
		if strings.Contains(err.Error(), "doesn't exist") || strings.Contains(err.Error(), "Error 1146") {
			return false, nil
		}
		return false, err
	}
	return count > 0, nil
}

func lookupUID(username string) (int, bool) {
	u, err := user.Lookup(username)
	if err != nil {
		return 0, false
	}
	uid, err := strconv.Atoi(u.Uid)
	return uid, err == nil
}

func lookupGID(groupname string) (int, bool) {
	g, err := user.LookupGroup(groupname)
	if err != nil {
		return 0, false
	}
	gid, err := strconv.Atoi(g.Gid)
	return gid, err == nil
}

// removeLine drops every line of path that contains pattern (port of
// system::removeLine used on jail passwd/shadow).
func removeLine(path, pattern string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	var kept []string
	for line := range strings.SplitSeq(string(data), "\n") {
		if line == "" {
			continue
		}
		if strings.Contains(line, pattern) {
			continue
		}
		kept = append(kept, line)
	}
	content := ""
	if len(kept) > 0 {
		content = strings.Join(kept, "\n") + "\n"
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// jailEtcPath is dir/etc/jailkit — presence marks an initialised jail.
func jailEtcPath(dir string) string {
	return filepath.Join(dir, "etc", "jailkit")
}
