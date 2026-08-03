// Shell-user plugin of the daemon: the Go port of ISPConfig3's
// server/plugins-available/shelluser_base_plugin.inc.php.
//
// Unlike an FTP account a shell user is a real entry in /etc/passwd, sharing
// the UID of the website's system user (useradd -o) so every file it writes
// still belongs to the site. This file carries the insert path; the update
// and delete paths follow in their own files.

package shell

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

	"gorm.io/gorm"

	"go-ispconfig/internal/auth"
	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/getconf"
	"go-ispconfig/internal/system"
)

// systemMinUID is the floor for a UID/GID the plugin may act on: below it
// the name belongs to a system account (system.inc.php $min_uid /
// $min_gid, checked by is_allowed_user / is_allowed_group).
const systemMinUID = 500

// profileContent is the .profile every shell user gets, verbatim from the
// PHP plugin.
const profileContent = `if [ -f ~/.bashrc ]
then
	. ~/.bashrc
fi

`

// Plugin is the shell user plugin. It subscribes to the shell_user events
// announced by the web module.
type Plugin struct {
	db     *gorm.DB
	runner engine.CommandRunner
	log    *slog.Logger

	// Seams the tests replace: everything that reads the database or the
	// real /etc/passwd of the host.
	LoadWeb        func(domainID int64) (system.Row, error)
	LoadWebConfig  func(serverID uint32) (*getconf.WebConfig, error)
	AllowShellUser func() (bool, error)
	LookupUID      func(username string) (int, bool)
	LookupGID      func(groupname string) (int, bool)
}

// NewPlugin creates the shell plugin; log nil means slog.Default.
func NewPlugin(db *gorm.DB, runner engine.CommandRunner, log *slog.Logger) *Plugin {
	if log == nil {
		log = slog.Default()
	}
	p := &Plugin{db: db, runner: runner, log: log}
	p.LoadWeb, p.LoadWebConfig = p.loadWebDomain, p.webConfig
	p.AllowShellUser = p.allowShellUser
	p.LookupUID, p.LookupGID = lookupUID, lookupGID
	return p
}

// Name identifies the plugin in logs.
func (*Plugin) Name() string { return "shell" }

// OnLoad subscribes to the shell_user events (port of
// shelluser_base_plugin onLoad).
func (p *Plugin) OnLoad(r *engine.Registry) error {
	return r.RegisterEvent("shell_user_insert", p.shellUserInsert)
}

// shellUserInsert creates the system account and the home layout of a new
// shell user.
func (p *Plugin) shellUserInsert(ctx context.Context, _ string, data engine.Data) error {
	u := system.Row(data.New)

	allowed, err := p.AllowShellUser()
	if err != nil {
		return err
	}
	if !allowed {
		p.log.Warn("shell: plugin disabled by security settings",
			"username", u.Str("username"))
		return nil
	}

	web, err := p.LoadWeb(u.Num("parent_domain_id"))
	if err != nil || web == nil {
		return err
	}
	if !p.checkUser(u, web) {
		return nil
	}

	// The parent user owns the UID the shell user shares. Without it there
	// is nothing to attach the account to.
	puser := u.Str("puser")
	uid, ok := p.LookupUID(puser)
	if !ok {
		p.log.Warn("shell: skipping insert, parent user does not exist",
			"username", u.Str("username"), "puser", puser)
		return nil
	}
	// PHP re-checks uid > $min_uid (499) here; checkUser already refused
	// anything below systemMinUID through is_allowed_user, which is the
	// same floor, so nothing is left to test at this point.

	cfg, err := p.LoadWebConfig(uint32(web.Num("server_id")))
	if err != nil {
		return err
	}
	docroot := web.Str("document_root")
	if err := p.protect(ctx, cfg, docroot, false); err != nil {
		return err
	}
	insErr := p.createUser(ctx, u, uid)
	// Re-protect the docroot even when the creation failed: leaving it
	// mutable would silently weaken the site for every later run.
	if err := p.protect(ctx, cfg, docroot, true); err != nil && insErr == nil {
		insErr = err
	}
	return insErr
}

// createUser is the body of the insert between the two web_folder_protection
// calls: home layout, useradd, password, convenience symlinks.
func (p *Plugin) createUser(ctx context.Context, u system.Row, uid int) error {
	username, puser, pgroup := u.Str("username"), u.Str("puser"), u.Str("pgroup")
	homeBase := filepath.Join(u.Str("dir"), "home")
	homedir := filepath.Join(homeBase, username)

	// The home base belongs to root: a site user must not be able to swap
	// another shell user's home out from under it. Mode and ownership are
	// re-asserted even when the base already exists, as in PHP.
	if err := system.MkdirPath(ctx, p.runner, homeBase, 0o755, "", ""); err != nil {
		return err
	}
	if err := os.Chmod(homeBase, 0o755); err != nil {
		return fmt.Errorf("shell: chmod %s: %w", homeBase, err)
	}
	if err := system.Chown(ctx, p.runner, homeBase, "root", "root", false); err != nil {
		return err
	}
	if _, err := os.Stat(homedir); errors.Is(err, os.ErrNotExist) {
		if err := system.MkdirPath(ctx, p.runner, homedir, 0o750, puser, pgroup); err != nil {
			return err
		}
	}

	if out, err := p.runner.Run(ctx, "useradd", "-d", homedir, "-g", pgroup, "-o",
		"-s", shellOf(u), "-u", strconv.Itoa(uid), username); err != nil {
		return fmt.Errorf("shell: useradd %s: %w: %s", username, err, out)
	}
	p.log.Info("shell: added shelluser", "username", username, "homedir", homedir)

	if err := p.setPassword(ctx, username, u.Str("password")); err != nil {
		return err
	}
	// The login directory itself stays root-owned: it is the site tree, not
	// the user's home.
	if err := system.Chown(ctx, p.runner, u.Str("dir"), "root", "root", false); err != nil {
		return err
	}
	if err := p.writeHomeLayout(ctx, homedir, username, pgroup); err != nil {
		return err
	}

	// Convenience symlinks so an SFTP session does not land in an empty
	// directory. Relative, because they must also resolve inside a jail.
	for _, name := range []string{"web", "log", "private"} {
		link := filepath.Join(homedir, name)
		if err := os.Symlink("../../"+name, link); err != nil && !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("shell: symlink %s: %w", link, err)
		}
	}

	// A jailkit user is parked until the jailkit plugin has built its chroot
	// and can hand it a real shell.
	if u.Str("chroot") == "jailkit" {
		if out, err := p.runner.Run(ctx, "usermod", "-s", "/bin/false", "-L", username); err != nil {
			p.log.Warn("shell: could not disable shelluser temporarily",
				"username", username, "err", err, "output", string(out))
		}
	}
	return nil
}

// writeHomeLayout creates the dotfiles and directories every shell user gets
// (.bash_history, .profile, .bashrc.d, .local/bin).
func (p *Plugin) writeHomeLayout(ctx context.Context, homedir, username, pgroup string) error {
	files := []struct {
		name    string
		mode    os.FileMode
		content string
	}{
		{".bash_history", 0o750, ""},
		{".profile", 0o644, profileContent},
	}
	for _, f := range files {
		path := filepath.Join(homedir, f.name)
		if err := os.WriteFile(path, []byte(f.content), f.mode); err != nil {
			return fmt.Errorf("shell: writing %s: %w", path, err)
		}
		// WriteFile applies the process umask on creation, so set the mode.
		if err := os.Chmod(path, f.mode); err != nil {
			return fmt.Errorf("shell: chmod %s: %w", path, err)
		}
		if err := system.Chown(ctx, p.runner, path, username, pgroup, false); err != nil {
			return err
		}
	}

	// .local/bin follows FHS 3.0: it is where the jail's PHP binary and any
	// user-provided tools land, and systemd/XDG puts it on PATH.
	for _, dir := range []string{".bashrc.d", ".local/bin"} {
		path := filepath.Join(homedir, dir)
		if _, err := os.Stat(path); err == nil {
			continue
		}
		if err := system.MkdirPath(ctx, p.runner, path, 0o750, username, pgroup); err != nil {
			return err
		}
	}
	return nil
}

// setPassword pipes the already-hashed password into chpasswd -e. A failure
// is logged and not returned: the account exists and is worth keeping even
// when the hash was rejected, exactly as in PHP.
func (p *Plugin) setPassword(ctx context.Context, username, password string) error {
	if password == "" {
		return nil
	}
	stdin, ok := p.runner.(engine.StdinRunner)
	if !ok {
		return fmt.Errorf("shell: runner cannot pipe the password hash to chpasswd")
	}
	out, err := stdin.RunWithStdin(ctx, []byte(username+":"+password+"\n"), "chpasswd", "-e")
	if err != nil {
		p.log.Warn("shell: chpasswd failed", "username", username,
			"err", err, "output", string(out))
	}
	return nil
}

// shellOf returns the login shell to give the account: an inactive user and
// a not-yet-built jailkit user both get /bin/false.
func shellOf(u system.Row) string {
	if u.Str("active") != "y" || u.Str("chroot") == "jailkit" {
		return "/bin/false"
	}
	return u.Str("shell")
}

// nameRe is the allow-list for user and group names of
// system::is_allowed_user / is_allowed_group.
var nameRe = regexp.MustCompile(`^[a-zA-Z0-9.\-_]{1,32}$`)

// webNameRe and clientNameRe are the restrict_names patterns: a parent user
// is always a site user and a parent group always a client group.
var (
	webNameRe    = regexp.MustCompile(`^web\d+$`)
	clientNameRe = regexp.MustCompile(`^client\d+$`)
)

// nameBlacklist is the hard-coded list of system::is_allowed_user; it is
// separate from (and much shorter than) the operator-facing blacklist used
// by the API validator.
var nameBlacklist = map[string]bool{"root": true, "ispconfig": true, "vmail": true, "getmail": true}

// checkUser ports the guard block shared by the PHP insert and update paths:
// the login directory has to sit inside the site and outside the system
// directories, and neither the account nor its parent user/group may be a
// privileged name. Every one of these is re-checked here because the daemon
// acts as root on a payload the API produced earlier.
func (p *Plugin) checkUser(u system.Row, web system.Row) bool {
	dir, username := u.Str("dir"), u.Str("username")
	if !system.UnderDocroot(dir, web.Str("document_root")) {
		p.log.Warn("shell: directory of the shell user is outside of the website docroot",
			"username", username, "dir", dir, "document_root", web.Str("document_root"))
		return false
	}
	if !p.isAllowedUser(username, false, nil) ||
		!p.isAllowedUser(u.Str("puser"), true, webNameRe) ||
		!p.isAllowedGroup(u.Str("pgroup")) {
		p.log.Warn("shell: user must not be root or in group root",
			"username", username, "puser", u.Str("puser"), "pgroup", u.Str("pgroup"))
		return false
	}
	if info, err := os.Lstat(dir); err == nil && !info.IsDir() {
		p.log.Warn("shell: user dir must not be an existing file or symlink",
			"username", username, "dir", dir)
		return false
	}
	if !system.IsAllowedPath(dir) {
		p.log.Warn("shell: user dir is not an allowed path", "username", username, "dir", dir)
		return false
	}
	return true
}

// isAllowedUser ports system::is_allowed_user. checkID also requires the
// account to exist with a non-system UID; restrict, when set, is the extra
// name pattern the caller demands.
func (p *Plugin) isAllowedUser(name string, checkID bool, restrict *regexp.Regexp) bool {
	if nameBlacklist[name] || !nameRe.MatchString(name) {
		return false
	}
	if checkID {
		uid, ok := p.LookupUID(name)
		if !ok || uid < systemMinUID {
			return false
		}
	}
	return restrict == nil || restrict.MatchString(name)
}

// isAllowedGroup ports system::is_allowed_group for the parent group, which
// is always checked with both the id and the client-name restriction.
func (p *Plugin) isAllowedGroup(name string) bool {
	if nameBlacklist[name] || !nameRe.MatchString(name) {
		return false
	}
	gid, ok := p.LookupGID(name)
	return ok && gid >= systemMinUID && clientNameRe.MatchString(name)
}

// protect drops (protect=false) or restores (protect=true) the immutable
// flag of the document root, honouring the server's web_folder_protection
// setting.
func (p *Plugin) protect(ctx context.Context, cfg *getconf.WebConfig, docroot string, protect bool) error {
	return system.WebFolderProtection(ctx, p.runner, p.log, docroot, protect,
		cfg != nil && cfg.WebFolderProtection == "y")
}

// lookupUID resolves a username in /etc/passwd.
func lookupUID(username string) (int, bool) {
	u, err := user.Lookup(username)
	if err != nil {
		return 0, false
	}
	uid, err := strconv.Atoi(u.Uid)
	return uid, err == nil
}

// lookupGID resolves a group name in /etc/group.
func lookupGID(groupname string) (int, bool) {
	g, err := user.LookupGroup(groupname)
	if err != nil {
		return 0, false
	}
	gid, err := strconv.Atoi(g.Gid)
	return gid, err == nil
}

// allowShellUser reads the allow_shell_user security policy flag.
func (p *Plugin) allowShellUser() (bool, error) {
	if p.db == nil {
		return true, nil
	}
	value, err := auth.GetPolicy(p.db, "allow_shell_user")
	if err != nil {
		return false, fmt.Errorf("shell: loading allow_shell_user: %w", err)
	}
	return value == "yes", nil
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
		return nil, fmt.Errorf("shell: loading web_domain %d: %w", domainID, err)
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
		return nil, fmt.Errorf("shell: loading server %d web config: %w", serverID, err)
	}
	return &cfg.Web, nil
}
