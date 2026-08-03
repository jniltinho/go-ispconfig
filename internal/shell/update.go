package shell

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/system"
)

// shellUserUpdate applies a changed shell_user row to the system account:
// the home may have moved (a renamed account or a new login directory), the
// login name, group and shell may have changed, and an account that went
// inactive loses its shell.
//
// When the account is not in /etc/passwd at all the event falls through to
// the insert path, so a row that was created while the plugin was disabled
// still lands on the next change.
func (p *Plugin) shellUserUpdate(ctx context.Context, event string, data engine.Data) error {
	u, old := system.Row(data.New), system.Row(data.Old)

	allowed, err := p.AllowShellUser()
	if err != nil {
		return err
	}
	if !allowed {
		p.log.Warn("shell: plugin disabled by security settings", "username", u.Str("username"))
		return nil
	}

	web, err := p.LoadWeb(u.Num("parent_domain_id"))
	if err != nil || web == nil {
		return err
	}
	if !p.checkUser(u, web) {
		return nil
	}
	if _, ok := p.LookupUID(u.Str("puser")); !ok {
		p.log.Warn("shell: skipping update, parent user does not exist",
			"username", u.Str("username"), "puser", u.Str("puser"))
		return nil
	}
	if _, ok := p.LookupUID(old.Str("username")); !ok {
		p.log.Info("shell: account does not exist yet, creating it",
			"username", old.Str("username"))
		return p.shellUserInsert(ctx, event, data)
	}

	cfg, err := p.LoadWebConfig(uint32(web.Num("server_id")))
	if err != nil {
		return err
	}
	docroot := web.Str("document_root")
	if err := p.protect(ctx, cfg, docroot, false); err != nil {
		return err
	}
	updErr := p.modifyUser(ctx, u, old)
	// Re-protect the docroot even when the update failed: leaving it mutable
	// would silently weaken the site for every later run.
	if err := p.protect(ctx, cfg, docroot, true); err != nil && updErr == nil {
		updErr = err
	}
	return updErr
}

// modifyUser is the body of the update between the two
// web_folder_protection calls.
func (p *Plugin) modifyUser(ctx context.Context, u, old system.Row) error {
	username, pgroup := u.Str("username"), u.Str("pgroup")
	homedir, oldHomedir := homeOf(u), homeOf(old)

	if homedir != oldHomedir {
		if err := p.moveHome(ctx, oldHomedir, homedir, u); err != nil {
			return err
		}
	} else if _, err := os.Stat(homedir); errors.Is(err, os.ErrNotExist) {
		if err := p.ensureHome(ctx, u); err != nil {
			return err
		}
	}

	// PHP rewrites /etc/passwd, /etc/group and /etc/shadow by hand to change
	// the login name and the hash in one pass. usermod does the same job
	// atomically; it refuses while the account has running processes, which
	// is the safer failure of the two for a root daemon.
	args := []string{"-d", homedir, "-g", pgroup, "-s", shellOnUpdate(u)}
	if oldName := old.Str("username"); oldName != username {
		args = append(args, "-l", username)
		args = append(args, oldName)
	} else {
		args = append(args, username)
	}
	if out, err := p.runner.Run(ctx, "usermod", args...); err != nil {
		return fmt.Errorf("shell: usermod %s: %w: %s", old.Str("username"), err, out)
	}
	p.log.Info("shell: updated shelluser", "username", username, "homedir", homedir)

	if err := p.setPassword(ctx, username, u.Str("password")); err != nil {
		return err
	}
	return p.writeHomeLayout(ctx, homedir, username, pgroup)
}

// moveHome carries the existing home over to its new location. A directory
// already sitting at the target is parked as <homedir>_bak rather than
// overwritten: it may hold a previous user's files.
func (p *Plugin) moveHome(ctx context.Context, oldHomedir, homedir string, u system.Row) error {
	if _, err := os.Stat(homedir); err == nil {
		p.log.Debug("shell: new homedir exists, renaming it", "homedir", homedir)
		if err := os.Rename(homedir, homedir+"_bak"); err != nil {
			return fmt.Errorf("shell: renaming %s: %w", homedir, err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(homedir), 0o755); err != nil {
		return fmt.Errorf("shell: creating %s: %w", filepath.Dir(homedir), err)
	}
	if err := os.Rename(oldHomedir, homedir); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("shell: moving %s to %s: %w", oldHomedir, homedir, err)
	}
	return p.ensureHome(ctx, u)
}

// ensureHome creates the root-owned home base and the user's own home when
// they are missing, and re-asserts the ownership of the home either way.
func (p *Plugin) ensureHome(ctx context.Context, u system.Row) error {
	homedir := homeOf(u)
	if u.Str("chroot") != "jailkit" {
		homeBase := filepath.Join(u.Str("dir"), "home")
		if err := system.MkdirPath(ctx, p.runner, homeBase, 0o755, "", ""); err != nil {
			return err
		}
		if err := system.Chown(ctx, p.runner, homeBase, "root", "root", false); err != nil {
			return err
		}
	}
	if err := system.MkdirPath(ctx, p.runner, homedir, 0o750, "", ""); err != nil {
		return err
	}
	return system.Chown(ctx, p.runner, homedir, u.Str("puser"), u.Str("pgroup"), false)
}

// homeOf returns the home directory of a shell user: a jailkit account is
// chrooted to its login directory, everyone else lives in dir/home/<user>.
func homeOf(u system.Row) string {
	if u.Str("chroot") == "jailkit" {
		return u.Str("dir")
	}
	return filepath.Join(u.Str("dir"), "home", u.Str("username"))
}

// shellOnUpdate returns the login shell an update assigns. Unlike the insert
// path this does not force /bin/false on a jailkit user: by update time the
// jailkit plugin owns that decision and has already given the account
// jk_chrootsh.
func shellOnUpdate(u system.Row) string {
	if u.Str("active") != "y" {
		return "/bin/false"
	}
	return u.Str("shell")
}
