package nginx

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"go-ispconfig/internal/getconf"
)

// site carries everything one web_domain event handler run needs: the server
// web config, the decoded payload and the parent domain names resolved for
// vhostsubdomain/vhostalias records. It is assembled by the event handlers
// (DB lookups) and consumed by the pure-ish worker functions, keeping those
// testable with fake runners and temp dirs.
type site struct {
	cfg             *getconf.WebConfig
	action          string // "insert" or "update"
	old, new        row
	parentDomain    string // parent web_domain.domain for sub/alias types
	oldParentDomain string
}

// systemNameRe is the allowed system user/group name pattern (PHP
// is_allowed_user).
var systemNameRe = regexp.MustCompile(`^[a-zA-Z0-9.\-_]{1,32}$`)

// forbiddenSystemNames are names a website may never run as.
var forbiddenSystemNames = []string{"root", "ispconfig", "vmail", "getmail"}

// blacklistedWebFolders are first path elements a vhostsubdomain/vhostalias
// web_folder may not use (PHP is_blacklisted_web_path).
var blacklistedWebFolders = []string{
	"bin", "cgi-bin", "dev", "etc", "home", "lib", "lib64", "log",
	"ssl", "usr", "var", "proc", "net", "sys", "srv", "sbin", "run",
}

// allowedSystemName reports whether name may own a website.
func allowedSystemName(name string) bool {
	return !slices.Contains(forbiddenSystemNames, name) && systemNameRe.MatchString(name)
}

// webFolderOf returns the folder below document_root serving the site
// ("web", "web/sub" for a vhost with web_folder, or the raw web_folder for
// vhostsubdomain/vhostalias).
func webFolderOf(d row) string {
	if d.str("type") == "vhost" {
		if wf := trimSlashes(d.str("web_folder")); wf != "" {
			return "web/" + wf
		}
		return "web"
	}
	return trimSlashes(d.str("web_folder"))
}

// logFolderOf returns the log folder below document_root ("log", or
// "log/<subdomain-host>" for vhostsubdomain/vhostalias).
func logFolderOf(d row, parentDomain string) string {
	if d.str("type") == "vhost" {
		return "log"
	}
	host := d.str("domain")
	if parentDomain != "" && strings.HasSuffix(host, "."+parentDomain) {
		host = strings.TrimSuffix(host, "."+parentDomain)
	}
	if host == "" || host == d.str("domain") {
		host = fmt.Sprintf("web%d", d.num("domain_id"))
	}
	return "log/" + host
}

// ensureSite provisions the site filesystem and system user/group for a
// vhost-type web_domain (design D4, idempotent): directory tree, ownership,
// user/group creation and docroot move on rename. All shell interaction goes
// through the plugin's CommandRunner.
func (p *Plugin) ensureSite(ctx context.Context, s site) error {
	d := s.new
	docroot := d.str("document_root")
	basedir := s.cfg.WebsiteBasedir

	if strings.TrimSpace(d.str("domain")) == "" {
		return fmt.Errorf("nginx: domain is empty")
	}
	if err := safeSitePath(docroot, basedir); err != nil {
		return err
	}
	username, groupname := d.str("system_user"), d.str("system_group")
	if !allowedSystemName(username) || !allowedSystemName(groupname) {
		return fmt.Errorf("nginx: website user/group not allowed: user=%q group=%q", username, groupname)
	}
	webFolder := webFolderOf(d)
	if d.str("type") != "vhost" {
		parts := strings.Split(strings.ToLower(webFolder), "/")
		if webFolder == "" || slices.Contains(blacklistedWebFolders, parts[0]) {
			return fmt.Errorf("nginx: blacklisted or empty web folder %q", webFolder)
		}
	}

	// Create group and user when missing (port of the groupadd/useradd
	// block; connect_userid_to_webid fixed uids and chroot are not ported).
	if err := p.ensureGroup(ctx, groupname); err != nil {
		return err
	}
	if err := p.ensureUser(ctx, username, groupname, docroot, s.cfg.AddWebUsersToSshusersGroup == "y"); err != nil {
		return err
	}

	// Docroot move on rename/client change (vhost only: sub/alias sites live
	// inside their parent's docroot).
	if s.action == "update" && d.str("type") == "vhost" &&
		s.old.str("document_root") != "" && s.old.str("document_root") != docroot {
		if err := p.moveDocroot(ctx, s, docroot); err != nil {
			return err
		}
	}

	logFolder := logFolderOf(d, s.parentDomain)

	// Directory tree (idempotent). Perms follow the PHP security-level-20
	// defaults; tmp is 1777 per the nginx-vhost spec.
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
	if d.num("errordocs") != 0 {
		dirs = append(dirs, dir{rel: webFolder + "/error", mode: 0o755, user: username, grp: groupname})
	}
	if d.str("stats_type") != "" {
		dirs = append(dirs, dir{rel: webFolder + "/stats", mode: 0o755, user: username, grp: groupname})
	}
	for _, e := range dirs {
		if e.defaultTop && d.str("type") != "vhost" {
			continue // parent vhost already owns ssl/tmp/private/cgi-bin
		}
		path := filepath.Join(docroot, e.rel)
		if err := os.MkdirAll(path, 0o755); err != nil {
			return fmt.Errorf("nginx: creating %s: %w", path, err)
		}
		if err := os.Chmod(path, e.mode); err != nil {
			return fmt.Errorf("nginx: chmod %s: %w", path, err)
		}
		if err := p.chown(ctx, path, e.user, e.grp, false); err != nil {
			return err
		}
	}

	// Per-domain nginx log directory (referenced by the vhost template).
	// ponytail: plain directory instead of the PHP bind-mount + fstab dance;
	// logs live only under logBaseDir, docroot/log stays an empty tree.
	domainLogDir := filepath.Join(p.logBaseDir, d.str("domain"))
	if err := os.MkdirAll(domainLogDir, 0o755); err != nil {
		return fmt.Errorf("nginx: creating %s: %w", domainLogDir, err)
	}
	if err := os.Chmod(domainLogDir, 0o750); err != nil {
		return fmt.Errorf("nginx: chmod %s: %w", domainLogDir, err)
	}
	if err := p.chown(ctx, domainLogDir, "root", groupname, false); err != nil {
		return err
	}

	// Domain rename: drop the old domain's log directory.
	if s.action == "update" && s.old.str("domain") != "" && s.old.str("domain") != d.str("domain") {
		oldLogDir := filepath.Join(p.logBaseDir, s.old.str("domain"))
		if !strings.Contains(s.old.str("domain"), "..") && !strings.Contains(s.old.str("domain"), "/") {
			if err := os.RemoveAll(oldLogDir); err != nil {
				return fmt.Errorf("nginx: removing old log dir %s: %w", oldLogDir, err)
			}
		}
	}
	return nil
}

// moveDocroot moves the site data when document_root changed (rename or
// client change), port of the PHP move block without symlink/fstab/chroot
// handling.
func (p *Plugin) moveDocroot(ctx context.Context, s site, newDocroot string) error {
	oldDocroot := s.old.str("document_root")
	if err := safeSitePath(oldDocroot, s.cfg.WebsiteBasedir); err != nil {
		return err
	}
	if _, err := os.Stat(oldDocroot); os.IsNotExist(err) {
		return nil // nothing to move; the tree is created fresh below
	}
	// A clean target is required: rename anything already there.
	if _, err := os.Stat(newDocroot); err == nil {
		backup := newDocroot + "_bak_" + time.Now().Format("2006_01_02_15_04_05")
		if err := os.Rename(newDocroot, backup); err != nil {
			return fmt.Errorf("nginx: renaming existing %s: %w", newDocroot, err)
		}
		p.log.Info("nginx: renamed existing directory in new docroot location", "from", newDocroot, "to", backup)
	}
	if err := os.MkdirAll(filepath.Dir(newDocroot), 0o755); err != nil {
		return fmt.Errorf("nginx: creating %s: %w", filepath.Dir(newDocroot), err)
	}
	// ponytail: os.Rename instead of `mv` — website_basedir is one
	// filesystem in the supported layouts; swap for a runner `mv` if
	// cross-device moves ever matter.
	if err := os.Rename(oldDocroot, newDocroot); err != nil {
		return fmt.Errorf("nginx: moving %s to %s: %w", oldDocroot, newDocroot, err)
	}
	p.log.Info("nginx: moved site to new document root", "from", oldDocroot, "to", newDocroot)

	if err := p.chown(ctx, newDocroot, s.new.str("system_user"), s.new.str("system_group"), true); err != nil {
		return err
	}
	// Update the system user's home directory and group.
	if out, err := p.runner.Run(ctx, "usermod",
		"--home", newDocroot, "--gid", s.new.str("system_group"), s.new.str("system_user")); err != nil {
		return fmt.Errorf("nginx: usermod %s: %w: %s", s.new.str("system_user"), err, out)
	}
	return nil
}

// ensureGroup creates the system group when it does not exist yet.
func (p *Plugin) ensureGroup(ctx context.Context, group string) error {
	if _, err := p.runner.Run(ctx, "getent", "group", group); err == nil {
		return nil
	}
	if out, err := p.runner.Run(ctx, "groupadd", group); err != nil {
		return fmt.Errorf("nginx: groupadd %s: %w: %s", group, err, out)
	}
	p.log.Info("nginx: added group", "group", group)
	return nil
}

// ensureUser creates the system user when it does not exist yet.
func (p *Plugin) ensureUser(ctx context.Context, user, group, home string, sshusers bool) error {
	if _, err := p.runner.Run(ctx, "getent", "passwd", user); err == nil {
		return nil
	}
	args := []string{"-d", home, "-g", group}
	if sshusers {
		args = append(args, "-G", "sshusers")
	}
	args = append(args, "-s", "/bin/false", user)
	if out, err := p.runner.Run(ctx, "useradd", args...); err != nil {
		return fmt.Errorf("nginx: useradd %s: %w: %s", user, err, out)
	}
	p.log.Info("nginx: added user", "user", user)
	return nil
}

// chown changes ownership through the command runner (the daemon runs as
// root in production; tests fake the runner).
func (p *Plugin) chown(ctx context.Context, path, user, group string, recursive bool) error {
	args := []string{user + ":" + group, path}
	if recursive {
		args = append([]string{"-R"}, args...)
	}
	if out, err := p.runner.Run(ctx, "chown", args...); err != nil {
		return fmt.Errorf("nginx: chown %s: %w: %s", path, err, out)
	}
	return nil
}
