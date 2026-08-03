package jailkit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/getconf"
	"go-ispconfig/internal/system"
)

// jailkitDirs are the top-level directories of a jail that
// delete_jailkit_chroot removes when the jail is unused.
var jailkitDirs = []string{
	"bin", "dev", "etc", "lib", "lib32", "lib64", "opt", "sys", "usr", "var", "run",
}

// shellUserDelete removes a jailkit account and optionally the unused jail
// (port of shelluser_jailkit_plugin::delete). Non-jailkit deletes are a no-op
// here — the base shell plugin owns those.
func (p *Plugin) shellUserDelete(ctx context.Context, _ string, data engine.Data) error {
	old := system.Row(data.Old)
	username, dir := old.Str("username"), old.Str("dir")
	if old.Str("chroot") != "jailkit" {
		return nil
	}

	allowed, err := p.AllowShellUser()
	if err != nil {
		return err
	}
	if !allowed {
		p.log.Warn("jailkit: plugin disabled by security settings", "username", username)
		return nil
	}
	if info, err := os.Lstat(dir); err == nil && !info.IsDir() {
		p.log.Warn("jailkit: user dir must not be an existing file or symlink",
			"username", username, "dir", dir)
		return nil
	}
	if !system.IsAllowedPath(dir) {
		p.log.Warn("jailkit: user dir is not an allowed path", "username", username, "dir", dir)
		return nil
	}

	web, err := p.LoadWeb(old.Num("parent_domain_id"))
	if err != nil {
		return err
	}
	if web == nil {
		web = system.Row{}
	}
	serverID := uint32(web.Num("server_id"))
	jkServer, err := p.LoadJailkitCfg(serverID)
	if err != nil {
		return err
	}
	// Site overrides for sections/programs also cover do_not_remove_paths.
	cfg := MergeConfig(jkServer, web)

	docroot := web.Str("document_root")
	if docroot == "" {
		docroot = dir
	}
	webCfg, err := p.LoadWebConfig(serverID)
	if err != nil {
		return err
	}
	if err := system.WebFolderProtection(ctx, p.runner, p.log, docroot, false,
		webCfg != nil && webCfg.WebFolderProtection == "y"); err != nil {
		return err
	}

	uid, _ := p.LookupUID(username)
	// killall is non-fatal when the user has no processes.
	if out, err := p.runner.Run(ctx, "killall", "-u", username); err != nil {
		p.log.Debug("jailkit: killall found no processes", "username", username, "output", string(out))
	}
	if out, err := p.runner.Run(ctx, "userdel", "-f", username); err != nil {
		p.log.Warn("jailkit: userdel failed", "username", username, "err", err, "output", string(out))
	}

	// Drop the jailed passwd/shadow lines.
	_ = removeLine(filepath.Join(dir, "etc", "passwd"), username+":")
	_ = removeLine(filepath.Join(dir, "etc", "shadow"), username+":")

	userHome := filepath.Join(dir, stringsTrim(HomeOf(cfg, username)))
	if info, err := os.Stat(userHome); err == nil && info.IsDir() {
		if err := p.deleteHomedir(ctx, userHome, uid, old.Num("parent_domain_id"), webCfg, docroot); err != nil {
			p.log.Warn("jailkit: home cleanup", "home", userHome, "err", err)
		}
	}

	if web.Str("delete_unused_jailkit") == "y" {
		if err := p.deleteJailkitIfUnused(ctx, web, cfg, serverID, webCfg); err != nil {
			p.log.Warn("jailkit: unused jail teardown", "err", err)
		}
	}

	if err := system.WebFolderProtection(ctx, p.runner, p.log, docroot, true,
		webCfg != nil && webCfg.WebFolderProtection == "y"); err != nil {
		return err
	}
	p.log.Info("jailkit: deleted jailkit user", "username", username)
	return nil
}

// deleteHomedir removes owned dotfiles of a jailed home and rmdirs it when
// empty (port of _delete_homedir).
func (p *Plugin) deleteHomedir(ctx context.Context, homedir string, uid int, parentDomainID int64, webCfg *getconf.WebConfig, docroot string) error {
	// Skip while another shell_user still logs into this exact home path.
	if p.db != nil {
		var count int64
		if err := p.db.Table("shell_user").Where("dir = ?", homedir).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return nil
		}
	}
	if err := system.WebFolderProtection(ctx, p.runner, p.log, docroot, false,
		webCfg != nil && webCfg.WebFolderProtection == "y"); err != nil {
		return err
	}
	for _, name := range []string{".bash_logout", ".bash_history", ".bashrc", ".profile"} {
		path := filepath.Join(homedir, name)
		if ownedBy(path, uid) {
			_ = os.Remove(path)
		}
	}
	for _, name := range []string{".ssh", ".cache"} {
		path := filepath.Join(homedir, name)
		if ownedBy(path, uid) {
			_ = os.RemoveAll(path)
		}
	}
	if entries, err := os.ReadDir(homedir); err == nil && len(entries) == 0 {
		_ = os.Remove(homedir)
	}
	return system.WebFolderProtection(ctx, p.runner, p.log, docroot, true,
		webCfg != nil && webCfg.WebFolderProtection == "y")
}

// deleteJailkitIfUnused tears down the jail tree when no shell_user or
// chrooted cron still needs it (port of _delete_jailkit_if_unused).
func (p *Plugin) deleteJailkitIfUnused(ctx context.Context, web system.Row, cfg Config, serverID uint32, webCfg *getconf.WebConfig) error {
	parentDomainID := web.Num("domain_id")
	docroot := web.Str("document_root")
	if docroot == "" {
		return nil
	}
	if info, err := os.Stat(docroot); err != nil || !info.IsDir() {
		return nil
	}
	if p.PHPFPMChroot(web) {
		p.log.Debug("jailkit: php-fpm chroot still uses the jail", "docroot", docroot)
		return nil
	}
	inuse, err := p.JailkitInUse(parentDomainID)
	if err != nil {
		return err
	}
	if inuse {
		return nil
	}

	skip := map[string]bool{}
	for _, pth := range cfg.DoNotRemovePaths {
		skip[stringsTrim(pth)] = true
	}
	folders, err := p.ListWebFolders(parentDomainID, docroot, serverID)
	if err != nil {
		return err
	}
	for _, f := range folders {
		skip[stringsTrim(f)] = true
	}

	for _, name := range jailkitDirs {
		if skip[name] {
			continue
		}
		path := filepath.Join(docroot, name)
		info, err := os.Lstat(path)
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			_ = os.Remove(path)
			continue
		}
		if info.IsDir() {
			if err := os.RemoveAll(path); err != nil {
				p.log.Warn("jailkit: removing jail dir", "path", path, "err", err)
			}
		}
	}
	// Drop empty /home; otherwise archive under /private (PHP behaviour).
	home := filepath.Join(docroot, "home")
	if err := os.Remove(home); err != nil && !errors.Is(err, os.ErrNotExist) {
		private := filepath.Join(docroot, "private")
		if info, err := os.Stat(private); err == nil && info.IsDir() {
			archive := filepath.Join(private, "home-archive")
			_ = os.Rename(home, archive)
		}
	}
	if err := p.ClearHash(docroot); err != nil {
		return err
	}
	p.log.Info("jailkit: removed unused jail", "docroot", docroot)
	return nil
}

func ownedBy(path string, uid int) bool {
	if uid <= 0 {
		return false
	}
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(stat.Uid) == uid
}

func stringsTrim(p string) string {
	for len(p) > 0 && p[0] == '/' {
		p = p[1:]
	}
	return p
}
