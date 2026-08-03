package shell

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/getconf"
	"go-ispconfig/internal/system"
)

// minUID is the floor for a UID this plugin may delete: at or below it the
// account is a system user (shelluser_base_plugin $min_uid). The insert and
// update paths get the same floor from is_allowed_user, which the delete
// path cannot use because the account name is already gone from the
// database.
const minUID = 499

// ownedDotfiles and ownedDotdirs are the leftovers the PHP plugin removes
// from a home nobody uses any more — but only the ones the departing user
// actually owns, so a home shared with the site user keeps its files.
var (
	ownedDotfiles = []string{".bash_logout", ".bash_history", ".bashrc", ".profile"}
	ownedDotdirs  = []string{".ssh", ".cache"}
)

// shellUserDelete removes the system account of a deleted shell_user row and
// cleans up the dotfiles it owned. A jailkit account is only cleaned up
// here: the jailkit plugin owns its userdel, because the account also has to
// disappear from the jail's own passwd file.
func (p *Plugin) shellUserDelete(ctx context.Context, _ string, data engine.Data) error {
	old := system.Row(data.Old)
	username, dir := old.Str("username"), old.Str("dir")

	allowed, err := p.AllowShellUser()
	if err != nil {
		return err
	}
	if !allowed {
		p.log.Warn("shell: plugin disabled by security settings", "username", username)
		return nil
	}

	if info, err := os.Lstat(dir); err == nil && !info.IsDir() {
		p.log.Warn("shell: user dir must not be an existing file or symlink",
			"username", username, "dir", dir)
		return nil
	}
	if !system.IsAllowedPath(dir) {
		p.log.Warn("shell: user dir is not an allowed path", "username", username, "dir", dir)
		return nil
	}

	uid, ok := p.LookupUID(username)
	if !ok {
		p.log.Warn("shell: user does not exist in /etc/passwd, skipping delete",
			"username", username)
		return nil
	}
	if uid <= minUID {
		return fmt.Errorf("shell: uid %d of %s is a system account, refusing to delete",
			uid, username)
	}

	web, err := p.LoadWeb(old.Num("parent_domain_id"))
	if err != nil {
		return err
	}
	if err := p.cleanHome(ctx, old, web, uid); err != nil {
		return err
	}
	if old.Str("chroot") == "jailkit" {
		p.log.Debug("shell: leaving userdel of a jailed account to the jailkit plugin",
			"username", username)
		return nil
	}
	return p.removeAccount(ctx, username, web)
}

// cleanHome removes the dotfiles and dot-directories the departing user owns
// and drops the home when nothing is left. It is skipped entirely while
// another shell user still logs into the same directory.
func (p *Plugin) cleanHome(ctx context.Context, old, web system.Row, uid int) error {
	dir := old.Str("dir")
	shared, err := p.DirInUse(dir)
	if err != nil {
		return err
	}
	if shared {
		p.log.Debug("shell: login directory is still in use, keeping it", "dir", dir)
		return nil
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return nil
	}

	// PHP reads $data['new']['chroot'] here, which is always empty on a
	// delete; the jailed layout is what the row actually had.
	homedir := homeOf(old)
	var cfg *getconf.WebConfig
	docroot := web.Str("document_root")
	if web != nil {
		if cfg, err = p.LoadWebConfig(uint32(web.Num("server_id"))); err != nil {
			return err
		}
	}
	if err := p.protect(ctx, cfg, docroot, false); err != nil {
		return err
	}
	cleanErr := removeOwned(homedir, uid)
	if err := p.protect(ctx, cfg, docroot, true); err != nil && cleanErr == nil {
		cleanErr = err
	}
	return cleanErr
}

// removeOwned unlinks the known dotfiles and dot-directories of homedir that
// belong to uid, then removes the home itself if that emptied it.
func removeOwned(homedir string, uid int) error {
	for _, name := range ownedDotfiles {
		path := filepath.Join(homedir, name)
		if !ownedBy(path, uid) {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("shell: removing %s: %w", path, err)
		}
	}
	for _, name := range ownedDotdirs {
		path := filepath.Join(homedir, name)
		if !ownedBy(path, uid) {
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("shell: removing %s: %w", path, err)
		}
	}
	entries, err := os.ReadDir(homedir)
	if err != nil || len(entries) > 0 {
		return nil
	}
	if err := os.Remove(homedir); err != nil {
		return fmt.Errorf("shell: removing %s: %w", homedir, err)
	}
	return nil
}

// ownedBy reports whether path exists and belongs to uid. Anything the
// departing user does not own is left alone: the home can be shared with the
// site system user, whose files must survive.
func ownedBy(path string, uid int) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(stat.Uid) == uid
}

// removeAccount kills whatever the user is still running and deletes it from
// /etc/passwd. A php-fpm pool of the parent site runs under the same UID, so
// it is stopped for the duration: killall would take its workers down and
// userdel would then refuse to run.
func (p *Plugin) removeAccount(ctx context.Context, username string, web system.Row) error {
	unit, err := p.fpmUnit(web)
	if err != nil {
		return err
	}
	if unit != "" {
		if out, err := p.runner.Run(ctx, "systemctl", "stop", unit); err != nil {
			p.log.Warn("shell: could not stop php-fpm before userdel",
				"unit", unit, "err", err, "output", string(out))
		}
	}

	// killall exits non-zero when the user has no processes at all, which is
	// the common case and not an error.
	if out, err := p.runner.Run(ctx, "killall", "-u", username); err != nil {
		p.log.Debug("shell: killall found no processes", "username", username, "output", string(out))
	}
	delErr := error(nil)
	if out, err := p.runner.Run(ctx, "userdel", "-f", username); err != nil {
		delErr = fmt.Errorf("shell: userdel %s: %w: %s", username, err, out)
	} else {
		p.log.Info("shell: deleted shelluser", "username", username)
	}

	if unit != "" {
		if out, err := p.runner.Run(ctx, "systemctl", "start", unit); err != nil {
			p.log.Warn("shell: could not start php-fpm again after userdel",
				"unit", unit, "err", err, "output", string(out))
		}
	}
	return delErr
}

// fpmUnit returns the php-fpm unit serving the parent website, or "" when
// the site does not run php-fpm at all: the pinned server_php version when
// the site has one, the server default otherwise.
func (p *Plugin) fpmUnit(web system.Row) (string, error) {
	if web == nil || web.Str("php") != "php-fpm" {
		return "", nil
	}
	if id := web.Num("server_php_id"); id != 0 {
		unit, err := p.LoadServerPHPUnit(id)
		if err != nil {
			return "", err
		}
		if unit != "" {
			return unit, nil
		}
	}
	cfg, err := p.LoadWebConfig(uint32(web.Num("server_id")))
	if err != nil {
		return "", err
	}
	if cfg == nil {
		return "", nil
	}
	return cfg.PHPFPMInitScript, nil
}

// dirInUse reports whether another shell_user still logs into dir. The
// deleted row is gone from the table by the time the daemon sees the event,
// so any hit is a different account.
func (p *Plugin) dirInUse(dir string) (bool, error) {
	if p.db == nil || dir == "" {
		return false, nil
	}
	var count int64
	if err := p.db.Table("shell_user").Where("dir = ?", dir).Count(&count).Error; err != nil {
		return false, fmt.Errorf("shell: counting shell users in %s: %w", dir, err)
	}
	return count > 0, nil
}

// loadServerPHPUnit returns the php_fpm_init_script of a pinned PHP version.
func (p *Plugin) loadServerPHPUnit(serverPHPID int64) (string, error) {
	if p.db == nil {
		return "", nil
	}
	var unit string
	err := p.db.Table("server_php").Where("server_php_id = ?", serverPHPID).
		Pluck("php_fpm_init_script", &unit).Error
	if err != nil {
		return "", fmt.Errorf("shell: loading server_php %d: %w", serverPHPID, err)
	}
	return unit, nil
}
