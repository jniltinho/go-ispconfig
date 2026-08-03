// Package system holds the filesystem and payload helpers shared by the
// website-scoped daemon plugins (ftp, shell, jailkit): the Go port of the
// parts of ISPConfig3's server/lib/classes/system.inc.php they have in
// common. These are security checks run by a root process, so they live in
// one place rather than once per plugin — a fix here reaches every caller.
package system

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Runner executes a privileged command. It matches engine.CommandRunner and
// is redeclared here so this package stays independent of the daemon core.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

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

// UnderDocroot reports whether dir is the document root or a directory
// below it. PHP compares raw string prefixes, which also accepts a sibling
// like /var/www/web12 for docroot /var/www/web1; this port requires a path
// boundary and rejects traversal segments outright.
func UnderDocroot(dir, docroot string) bool {
	if docroot == "" || !strings.HasPrefix(dir, "/") {
		return false
	}
	if strings.Contains(dir, "..") || strings.Contains(dir, "./") {
		return false
	}
	dir = strings.TrimSuffix(dir, "/")
	docroot = strings.TrimSuffix(docroot, "/")
	return dir == docroot || strings.HasPrefix(dir, docroot+"/")
}

// safePathRe is the character allow-list of system::checkpath.
var safePathRe = regexp.MustCompile(`^/[-a-zA-Z0-9_/.*]+~?$`)

// CheckPath ports system::checkpath: absolute, no exotic characters and no
// symlink anywhere along the path, so a symlinked component can never
// redirect a root-owned chmod/chown outside the website.
func CheckPath(path string) bool {
	path = strings.TrimSpace(path)
	if !safePathRe.MatchString(path) {
		return false
	}
	var test strings.Builder
	for part := range strings.SplitSeq(strings.TrimPrefix(path, "/"), "/") {
		test.WriteString("/" + part)
		if info, err := os.Lstat(test.String()); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return false
		}
	}
	return true
}

// blacklistedPaths ports the regex list of system::is_allowed_path: the
// system directories a website-scoped account may never be rooted in.
var blacklistedPaths = []*regexp.Regexp{
	regexp.MustCompile(`^/$`),
	regexp.MustCompile(`^/proc(/.*)?$`),
	regexp.MustCompile(`^/sys(/.*)?$`),
	regexp.MustCompile(`^/etc(/.*)?$`),
	regexp.MustCompile(`^/dev(/.*)?$`),
	regexp.MustCompile(`^/tmp(/.*)?$`),
	regexp.MustCompile(`^/run(/.*)?$`),
	regexp.MustCompile(`^/boot(/.*)?$`),
	regexp.MustCompile(`^/root(/.*)?$`),
	regexp.MustCompile(`^/var(/?|/backups?(/.*)?)?$`),
}

// IsAllowedPath ports system::is_allowed_path: the path is normalised (and
// resolved when it exists, so a symlink into /etc is caught) and then
// matched against the system-directory blacklist.
func IsAllowedPath(path string) bool {
	path = normalizePath(path)
	// PHP resolves an existing path with realpath() first; file_exists()
	// follows symlinks, so a link pointing into /etc is caught here.
	if _, err := os.Stat(path); err == nil {
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			path = resolved
		}
	}
	for _, re := range blacklistedPaths {
		if re.MatchString(path) {
			return false
		}
	}
	return true
}

// normalizePath ports functions::normalize_path: collapse repeated slashes
// and drop the trailing one (but never turn "/" into "").
func normalizePath(path string) string {
	for strings.Contains(path, "//") {
		path = strings.ReplaceAll(path, "//", "/")
	}
	if len(path) > 1 {
		path = strings.TrimSuffix(path, "/")
	}
	return path
}

// MkdirPath ports system::mkdirpath: create every missing path component and
// give each new one the requested mode and ownership. Ownership goes through
// runner because the daemon changes it as root.
func MkdirPath(ctx context.Context, runner Runner, path string, mode fs.FileMode, user, group string) error {
	current := ""
	for _, part := range strings.Split(strings.TrimPrefix(strings.TrimSuffix(path, "/"), "/"), "/") {
		current += "/" + part
		if info, err := os.Stat(current); err == nil {
			if !info.IsDir() {
				return fmt.Errorf("system: %s exists and is not a directory", current)
			}
			continue
		}
		if err := os.Mkdir(current, mode); err != nil {
			return fmt.Errorf("system: creating %s: %w", current, err)
		}
		// Mkdir applies the process umask, so set the mode explicitly.
		if err := os.Chmod(current, mode); err != nil {
			return fmt.Errorf("system: chmod %s: %w", current, err)
		}
		if user == "" && group == "" {
			continue
		}
		if err := Chown(ctx, runner, current, user, group, false); err != nil {
			return err
		}
	}
	return nil
}

// Chown changes ownership through the command runner: the daemon runs as
// root and the target may be owned by any site user.
func Chown(ctx context.Context, runner Runner, path, user, group string, recursive bool) error {
	args := []string{user + ":" + group, path}
	if group == "" {
		args[0] = user
	}
	if recursive {
		args = append([]string{"-R"}, args...)
	}
	if out, err := runner.Run(ctx, "chown", args...); err != nil {
		return fmt.Errorf("system: chown %s: %w: %s", path, err, out)
	}
	return nil
}

// WebFolderProtection ports system::web_folder_protection: the immutable
// flag on the document root is dropped before the daemon writes into a site
// and restored afterwards, but only when the server enables
// web_folder_protection (enabled). Removing the flag is unconditional,
// exactly as in PHP, so a server that just turned the option off still
// unlocks its sites.
func WebFolderProtection(ctx context.Context, runner Runner, log *slog.Logger, docroot string, protect, enabled bool) error {
	if log == nil {
		log = slog.Default()
	}
	if !CheckPath(docroot) {
		log.Debug("system: action aborted, target is a symlink or unsafe path", "path", docroot)
		return nil
	}
	// PHP guards: never chattr /, or a suspiciously short path.
	if len(docroot) <= 6 || docroot == "/" {
		return nil
	}
	flag := "-i"
	if protect {
		if !enabled {
			return nil
		}
		flag = "+i"
	}
	if out, err := runner.Run(ctx, "chattr", flag, docroot); err != nil {
		// A filesystem without chattr support (overlayfs, tmpfs in tests) must
		// not fail the whole event, as in PHP where the exec result is ignored.
		log.Debug("system: chattr failed", "flag", flag, "path", docroot,
			"err", err, "output", string(out))
	}
	return nil
}
