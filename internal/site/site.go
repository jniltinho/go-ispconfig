// Package site provisions the on-disk layout of a website: directory tree,
// system user and group, docroot moves on rename, the default index page and
// the configured website symlinks.
//
// Both web-server plugins call Ensure — internal/nginx and internal/apache2
// each own their entry point and pass their own worker user, log base dir and
// error tag, while the filesystem layout (which is dictated by ISPConfig, not
// by the web server) lives here once.
package site

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/getconf"
)

// Row is one decoded datalog record ({old} or {new} payload) or a database
// row scanned into a map. Values may be strings (PHP-era payloads, text
// columns), json float64 numbers, ints from GORM scans or nil.
type Row map[string]any

// Str returns the value of k as a string ("" for missing/nil).
func (r Row) Str(k string) string {
	switch v := r[k].(type) {
	case nil:
		return ""
	case string:
		return v
	case []byte:
		return string(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return fmt.Sprint(v)
	}
}

// Num returns the value of k as an int64 (0 when missing or non-numeric).
func (r Row) Num(k string) int64 {
	switch v := r[k].(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	case uint32:
		return int64(v)
	case uint64:
		return int64(v)
	case int32:
		return int64(v)
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return n
	case []byte:
		n, _ := strconv.ParseInt(strings.TrimSpace(string(v)), 10, 64)
		return n
	default:
		return 0
	}
}

// TrimSlashes removes one leading and one trailing slash (PHP web_folder
// normalization).
func TrimSlashes(s string) string {
	s = strings.TrimPrefix(s, "/")
	return strings.TrimSuffix(s, "/")
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

// AllowedSystemName reports whether name may own a website. A leading "-" is
// rejected so the name can never be parsed as an option flag by the
// privileged useradd/groupadd/chown commands the daemon runs as root
// (e.g. system_user "-u0" would create a UID-0 account).
func AllowedSystemName(name string) bool {
	return name != "" && !strings.HasPrefix(name, "-") &&
		!slices.Contains(forbiddenSystemNames, name) && systemNameRe.MatchString(name)
}

// WebFolder returns the folder below document_root serving the site ("web",
// "web/sub" for a vhost with web_folder, or the raw web_folder for
// vhostsubdomain/vhostalias).
func WebFolder(d Row) string {
	if d.Str("type") == "vhost" {
		if wf := TrimSlashes(d.Str("web_folder")); wf != "" {
			return "web/" + wf
		}
		return "web"
	}
	return TrimSlashes(d.Str("web_folder"))
}

// LogFolder returns the log folder below document_root ("log", or
// "log/<subdomain-host>" for vhostsubdomain/vhostalias).
func LogFolder(d Row, parentDomain string) string {
	if d.Str("type") == "vhost" {
		return "log"
	}
	host := d.Str("domain")
	if parentDomain != "" && strings.HasSuffix(host, "."+parentDomain) {
		host = strings.TrimSuffix(host, "."+parentDomain)
	}
	if host == "" || host == d.Str("domain") {
		host = fmt.Sprintf("web%d", d.Num("domain_id"))
	}
	return "log/" + host
}

// SafePath validates that path is an absolute, traversal-free path strictly
// inside basedir (never basedir itself, never /). Every creating or
// destructive filesystem operation goes through this check (design D4: never
// touch a path outside website_basedir).
func SafePath(path, basedir string) error {
	if basedir == "" || !filepath.IsAbs(basedir) {
		return fmt.Errorf("website_basedir %q is not an absolute path", basedir)
	}
	if path == "" || !filepath.IsAbs(path) {
		return fmt.Errorf("path %q is not absolute", path)
	}
	if strings.Contains(path, "..") {
		return fmt.Errorf("path %q contains a parent-directory traversal", path)
	}
	clean := filepath.Clean(path)
	base := filepath.Clean(basedir)
	if clean == "/" || clean == base {
		return fmt.Errorf("refusing to operate on %q", clean)
	}
	if !strings.HasPrefix(clean, base+string(filepath.Separator)) {
		return fmt.Errorf("path %q is outside website_basedir %q", clean, base)
	}
	return nil
}

// SymlinkTargets expands the website_symlinks templates for one domain and
// client id, trailing slash removed.
func SymlinkTargets(cfg *getconf.WebConfig, domain string, clientID int64) []string {
	var out []string
	for _, tmpl := range strings.Split(cfg.WebsiteSymlinks, ":") {
		tmpl = strings.TrimSpace(tmpl)
		if tmpl == "" {
			continue
		}
		link := strings.ReplaceAll(tmpl, "[client_id]", fmt.Sprint(clientID))
		link = strings.ReplaceAll(link, "[website_domain]", domain)
		out = append(out, strings.TrimSuffix(link, "/"))
	}
	return out
}

// Chown changes ownership through the command runner (the daemon runs as root
// in production; tests fake the runner).
func Chown(ctx context.Context, runner engine.CommandRunner, path, user, group string, recursive bool) error {
	args := []string{user + ":" + group, path}
	if recursive {
		args = append([]string{"-R"}, args...)
	}
	if out, err := runner.Run(ctx, "chown", args...); err != nil {
		return fmt.Errorf("chown %s: %w: %s", path, err, out)
	}
	return nil
}
