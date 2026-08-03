package site

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/getconf"
)

// Request carries everything one provisioning run needs: the server web
// config, the old/new web_domain rows and the parent domain resolved for
// vhostsubdomain/vhostalias records, plus the few knobs that differ between
// the two web servers (Tag, WorkerUser, LogBaseDir).
type Request struct {
	// Tag prefixes errors and log lines ("nginx" or "apache2").
	Tag string
	// WorkerUser is the web server's worker account. At security level 20 it
	// joins the client group so it can read the site behind the 0710/0750
	// perms; empty falls back to www-data.
	WorkerUser string
	// LogBaseDir is the per-domain log directory root of this web server.
	LogBaseDir string

	Cfg    *getconf.WebConfig
	Action string // "insert" or "update"

	Old, New Row
	// ParentDomain is the parent web_domain.domain for sub/alias types.
	ParentDomain string
	// ClientID owns the site; OldClientID is set when it changed owners.
	ClientID, OldClientID int64

	Runner engine.CommandRunner
	Log    *slog.Logger
}

func (r Request) errf(format string, a ...any) error {
	return fmt.Errorf(r.Tag+": "+format, a...)
}

// Ensure provisions the site filesystem and system user/group for a
// vhost-type web_domain (design D4, idempotent): directory tree, ownership,
// user/group creation and docroot move on rename. All shell interaction goes
// through the caller's CommandRunner.
func Ensure(ctx context.Context, r Request) error {
	if r.Log == nil {
		r.Log = slog.Default()
	}
	d := r.New
	docroot := d.Str("document_root")
	basedir := r.Cfg.WebsiteBasedir

	if err := SafePath(docroot, basedir); err != nil {
		return r.errf("%w", err)
	}
	username, groupname := d.Str("system_user"), d.Str("system_group")
	if !AllowedSystemName(username) || !AllowedSystemName(groupname) {
		return r.errf("website user/group not allowed: user=%q group=%q", username, groupname)
	}
	webFolder := WebFolder(d)
	if d.Str("type") != "vhost" {
		parts := strings.Split(strings.ToLower(webFolder), "/")
		if webFolder == "" || slices.Contains(blacklistedWebFolders, parts[0]) {
			return r.errf("blacklisted or empty web folder %q", webFolder)
		}
	}
	// The web folder is user-controllable (web_folder column): revalidate the
	// resolved path so a "../.." folder can never escape the docroot the
	// daemon chowns/chmods as root.
	if err := SafePath(filepath.Join(docroot, webFolder), basedir); err != nil {
		return r.errf("web folder escapes docroot: %w", err)
	}

	// Create group and user when missing (port of the groupadd/useradd
	// block; connect_userid_to_webid fixed uids and chroot are not ported).
	if err := ensureGroup(ctx, r, groupname); err != nil {
		return err
	}
	if err := ensureUser(ctx, r, username, groupname, docroot); err != nil {
		return err
	}

	// Security level 20 (default): the web server worker joins the client
	// group so it can read the site's files behind the 0710/0750 perms (port
	// of add_user_to_group). Without it the server 403s on static files and
	// 500s on the folder-protection password file.
	if d.Str("type") == "vhost" && r.Cfg.SecurityLevel == "20" {
		worker := r.WorkerUser
		if worker == "" {
			worker = "www-data"
		}
		if out, err := r.Runner.Run(ctx, "usermod", "-a", "-G", groupname, worker); err != nil {
			return r.errf("adding %s to group %s: %w: %s", worker, groupname, err, out)
		}
	}

	// Docroot move on rename/client change (vhost only: sub/alias sites live
	// inside their parent's docroot).
	if r.Action == "update" && d.Str("type") == "vhost" &&
		r.Old.Str("document_root") != "" && r.Old.Str("document_root") != docroot {
		if err := moveDocroot(ctx, r, docroot); err != nil {
			return err
		}
	}

	logFolder := LogFolder(d, r.ParentDomain)

	// Directory tree (idempotent). Perms follow the PHP security-level-20
	// defaults; tmp is 1777 per the vhost spec.
	type dir struct {
		rel        string
		mode       os.FileMode
		user, grp  string
		defaultTop bool // only created for plain vhosts
	}
	dirs := []dir{
		{rel: "", mode: 0o755, user: "root", grp: "root"},
		{rel: webFolder, mode: 0o751, user: username, grp: groupname},
		{rel: logFolder, mode: 0o750, user: "root", grp: groupname},
		{rel: "ssl", mode: 0o755, user: "root", grp: "root", defaultTop: true},
		{rel: "tmp", mode: os.ModeSticky | 0o777, user: username, grp: groupname, defaultTop: true},
		{rel: "private", mode: 0o710, user: username, grp: groupname, defaultTop: true},
		{rel: "cgi-bin", mode: 0o755, user: username, grp: groupname, defaultTop: true},
	}
	if d.Num("errordocs") != 0 {
		dirs = append(dirs, dir{rel: webFolder + "/error", mode: 0o755, user: username, grp: groupname})
	}
	if d.Str("stats_type") != "" {
		dirs = append(dirs, dir{rel: webFolder + "/stats", mode: 0o755, user: username, grp: groupname})
	}
	for _, e := range dirs {
		if e.defaultTop && d.Str("type") != "vhost" {
			continue // parent vhost already owns ssl/tmp/private/cgi-bin
		}
		path := filepath.Join(docroot, e.rel)
		if err := os.MkdirAll(path, 0o755); err != nil {
			return r.errf("creating %s: %w", path, err)
		}
		if err := os.Chmod(path, e.mode); err != nil {
			return r.errf("chmod %s: %w", path, err)
		}
		if err := Chown(ctx, r.Runner, path, e.user, e.grp, false); err != nil {
			return r.errf("%w", err)
		}
	}

	// Per-domain log directory (referenced by the vhost template).
	// ponytail: plain directory instead of the PHP bind-mount + fstab dance;
	// logs live only under LogBaseDir, docroot/log stays an empty tree.
	domainLogDir := filepath.Join(r.LogBaseDir, d.Str("domain"))
	if err := os.MkdirAll(domainLogDir, 0o755); err != nil {
		return r.errf("creating %s: %w", domainLogDir, err)
	}
	if err := os.Chmod(domainLogDir, 0o750); err != nil {
		return r.errf("chmod %s: %w", domainLogDir, err)
	}
	if err := Chown(ctx, r.Runner, domainLogDir, "root", groupname, false); err != nil {
		return r.errf("%w", err)
	}

	// Domain rename: drop the old domain's log directory.
	if r.Action == "update" && r.Old.Str("domain") != "" && r.Old.Str("domain") != d.Str("domain") {
		oldLogDir := filepath.Join(r.LogBaseDir, r.Old.Str("domain"))
		if !strings.Contains(r.Old.Str("domain"), "..") && !strings.Contains(r.Old.Str("domain"), "/") {
			if err := os.RemoveAll(oldLogDir); err != nil {
				return r.errf("removing old log dir %s: %w", oldLogDir, err)
			}
		}
	}

	// Default index page (port of the PHP skeleton copy: the apache2/nginx
	// plugin writes standard_index.html when the web folder has no index
	// yet), so a fresh site serves HTTP 200 instead of an error page.
	webDir := filepath.Join(docroot, webFolder)
	hasIndex := false
	for _, name := range []string{"index.html", "index.php", "standard_index.html"} {
		if _, err := os.Stat(filepath.Join(webDir, name)); err == nil {
			hasIndex = true
			break
		}
	}
	if !hasIndex {
		idx := filepath.Join(webDir, "standard_index.html")
		if err := os.WriteFile(idx, []byte(standardIndexHTML), 0o644); err != nil {
			return r.errf("writing %s: %w", idx, err)
		}
		if err := Chown(ctx, r.Runner, idx, username, groupname, false); err != nil {
			return r.errf("%w", err)
		}
	}

	// Website symlinks (website_symlinks config): the rendered vhost's root
	// is website_basedir/<domain>/<web_folder>, which resolves through these
	// links, so they are load-bearing, not cosmetic.
	return ensureSymlinks(r, docroot)
}

// standardIndexHTML is the default page of a freshly created website
// (stand-in for ISPConfig's conf/index/standard_index.html_en skeleton).
const standardIndexHTML = `<!DOCTYPE html>
<html><head><title>Welcome!</title></head>
<body><h1>Welcome to your website!</h1>
<p>This is the default index page of your website.</p>
<p>This file may be deleted or overwritten without any difficulty. This is
produced by the file <b>standard_index.html</b> in the web directory.</p>
</body></html>
`

// ensureSymlinks creates the configured website symlinks pointing at the
// docroot and removes stale ones after a rename, docroot move or client
// change (port of the website_symlinks blocks of update()).
func ensureSymlinks(r Request, docroot string) error {
	d := r.New
	// Remove links of the previous domain/client when anything moved.
	if r.Action == "update" && r.Old.Str("domain") != "" &&
		(r.Old.Str("domain") != d.Str("domain") || r.Old.Str("document_root") != docroot || r.OldClientID != r.ClientID) {
		oldClient := r.OldClientID
		if oldClient == 0 {
			oldClient = r.ClientID
		}
		for _, link := range SymlinkTargets(r.Cfg, r.Old.Str("domain"), oldClient) {
			if info, err := os.Lstat(link); err == nil && info.Mode()&os.ModeSymlink != 0 {
				if err := os.Remove(link); err != nil {
					return r.errf("removing old symlink %s: %w", link, err)
				}
			}
		}
	}
	for _, link := range SymlinkTargets(r.Cfg, d.Str("domain"), r.ClientID) {
		if err := SafePath(link, r.Cfg.WebsiteBasedir); err != nil {
			r.Log.Warn(r.Tag+": skipping symlink outside website_basedir", "link", link)
			continue
		}
		if info, err := os.Lstat(link); err == nil {
			if info.Mode()&os.ModeSymlink == 0 {
				continue // a real file/dir is never replaced
			}
			if target, err := os.Readlink(link); err == nil && target == docroot+"/" {
				continue // already in place
			}
			// Symlink points at an old docroot: replace it.
			if err := os.Remove(link); err != nil {
				return r.errf("replacing symlink %s: %w", link, err)
			}
		}
		if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
			return r.errf("creating %s: %w", filepath.Dir(link), err)
		}
		if err := os.Symlink(docroot+"/", link); err != nil {
			return r.errf("creating symlink %s: %w", link, err)
		}
		r.Log.Info(r.Tag+": created symlink", "link", link, "target", docroot)
	}
	return nil
}

// moveDocroot moves the site data when document_root changed (rename or
// client change), port of the PHP move block without symlink/fstab/chroot
// handling.
func moveDocroot(ctx context.Context, r Request, newDocroot string) error {
	oldDocroot := r.Old.Str("document_root")
	if err := SafePath(oldDocroot, r.Cfg.WebsiteBasedir); err != nil {
		return r.errf("%w", err)
	}
	if _, err := os.Stat(oldDocroot); os.IsNotExist(err) {
		return nil // nothing to move; the tree is created fresh below
	}
	// A clean target is required: rename anything already there.
	if _, err := os.Stat(newDocroot); err == nil {
		backup := newDocroot + "_bak_" + time.Now().Format("2006_01_02_15_04_05")
		if err := os.Rename(newDocroot, backup); err != nil {
			return r.errf("renaming existing %s: %w", newDocroot, err)
		}
		r.Log.Info(r.Tag+": renamed existing directory in new docroot location", "from", newDocroot, "to", backup)
	}
	if err := os.MkdirAll(filepath.Dir(newDocroot), 0o755); err != nil {
		return r.errf("creating %s: %w", filepath.Dir(newDocroot), err)
	}
	// ponytail: os.Rename instead of `mv` — website_basedir is one filesystem
	// in the supported layouts; swap for a runner `mv` if cross-device moves
	// ever matter.
	if err := os.Rename(oldDocroot, newDocroot); err != nil {
		return r.errf("moving %s to %s: %w", oldDocroot, newDocroot, err)
	}
	r.Log.Info(r.Tag+": moved site to new document root", "from", oldDocroot, "to", newDocroot)

	if err := Chown(ctx, r.Runner, newDocroot, r.New.Str("system_user"), r.New.Str("system_group"), true); err != nil {
		return r.errf("%w", err)
	}
	// Update the system user's home directory and group.
	if out, err := r.Runner.Run(ctx, "usermod",
		"--home", newDocroot, "--gid", r.New.Str("system_group"), r.New.Str("system_user")); err != nil {
		return r.errf("usermod %s: %w: %s", r.New.Str("system_user"), err, out)
	}
	return nil
}

// ensureGroup creates the system group when it does not exist yet.
func ensureGroup(ctx context.Context, r Request, group string) error {
	if _, err := r.Runner.Run(ctx, "getent", "group", group); err == nil {
		return nil
	}
	if out, err := r.Runner.Run(ctx, "groupadd", group); err != nil {
		return r.errf("groupadd %s: %w: %s", group, err, out)
	}
	r.Log.Info(r.Tag+": added group", "group", group)
	return nil
}

// ensureUser creates the system user when it does not exist yet.
func ensureUser(ctx context.Context, r Request, user, group, home string) error {
	if _, err := r.Runner.Run(ctx, "getent", "passwd", user); err == nil {
		return nil
	}
	args := []string{"-d", home, "-g", group}
	if r.Cfg.AddWebUsersToSshusersGroup == "y" {
		args = append(args, "-G", "sshusers")
	}
	args = append(args, "-s", "/bin/false", user)
	if out, err := r.Runner.Run(ctx, "useradd", args...); err != nil {
		return r.errf("useradd %s: %w: %s", user, err, out)
	}
	r.Log.Info(r.Tag+": added user", "user", user)
	return nil
}
