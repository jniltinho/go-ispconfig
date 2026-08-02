package nginx

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// domainNameRe matches a safe DNS domain / wildcard: only labels, dots and
// a leading "*." — enough to reject anything usable as a path segment
// (slashes, "..", spaces) since domain is joined into vhost/log file names.
var domainNameRe = regexp.MustCompile(`^(\*\.)?([a-zA-Z0-9_-]+\.)*[a-zA-Z0-9_-]+$`)

// safeDomain rejects a domain that is unsafe to use as a filesystem path
// segment (vhost file name, log dir, ssl file name). Defense in depth: the
// sites API validates the domain, but migrated rows or a compromised row
// must never let the root daemon write outside its conf/log dirs.
func safeDomain(domain string) error {
	if domain == "" || strings.Contains(domain, "/") || strings.Contains(domain, "..") ||
		!domainNameRe.MatchString(domain) {
		return fmt.Errorf("nginx: unsafe domain name %q", domain)
	}
	return nil
}

// safeSitePath validates that path is an absolute, traversal-free path
// strictly inside basedir (never basedir itself, never /). Every
// creating or destructive filesystem operation of the plugin goes through
// this check (design D4: never touch a path outside website_basedir).
func safeSitePath(path, basedir string) error {
	if basedir == "" || !filepath.IsAbs(basedir) {
		return fmt.Errorf("nginx: website_basedir %q is not an absolute path", basedir)
	}
	if path == "" || !filepath.IsAbs(path) {
		return fmt.Errorf("nginx: path %q is not absolute", path)
	}
	if strings.Contains(path, "..") {
		return fmt.Errorf("nginx: path %q contains a parent-directory traversal", path)
	}
	clean := filepath.Clean(path)
	base := filepath.Clean(basedir)
	if clean == "/" || clean == base {
		return fmt.Errorf("nginx: refusing to operate on %q", clean)
	}
	if !strings.HasPrefix(clean, base+string(filepath.Separator)) {
		return fmt.Errorf("nginx: path %q is outside website_basedir %q", clean, base)
	}
	return nil
}
