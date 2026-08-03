package apache2

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// domainNameRe matches a safe DNS domain / wildcard: dot-separated labels
// with an optional leading "*.". Labels may not start or end with a hyphen
// (invalid DNS, and a leading "-" could be parsed as an option flag by a
// privileged command), and nothing usable as a path segment — slashes,
// "..", spaces — can match.
var domainNameRe = regexp.MustCompile(`^(\*\.)?([a-zA-Z0-9_]([a-zA-Z0-9_-]*[a-zA-Z0-9_])?\.)*[a-zA-Z0-9_]([a-zA-Z0-9_-]*[a-zA-Z0-9_])?$`)

// safeDomain rejects a domain that is unsafe to use as a filesystem path
// segment (vhost file name, log dir, ssl file name). Defense in depth: the
// sites API validates the domain, but migrated rows or a compromised row
// must never let the root daemon write outside its conf/log dirs.
func safeDomain(domain string) error {
	if domain == "" || strings.Contains(domain, "/") || strings.Contains(domain, "..") ||
		!domainNameRe.MatchString(domain) {
		return fmt.Errorf("apache2: unsafe domain name %q", domain)
	}
	return nil
}

// safeSitePath validates that path is an absolute, traversal-free path
// strictly inside basedir (never basedir itself, never /). Every creating or
// destructive filesystem operation of the plugin goes through this check.
func safeSitePath(path, basedir string) error {
	if basedir == "" || !filepath.IsAbs(basedir) {
		return fmt.Errorf("apache2: website_basedir %q is not an absolute path", basedir)
	}
	if path == "" || !filepath.IsAbs(path) {
		return fmt.Errorf("apache2: path %q is not absolute", path)
	}
	if strings.Contains(path, "..") {
		return fmt.Errorf("apache2: path %q contains a parent-directory traversal", path)
	}
	clean := filepath.Clean(path)
	base := filepath.Clean(basedir)
	if clean == "/" || clean == base {
		return fmt.Errorf("apache2: refusing to operate on %q", clean)
	}
	if !strings.HasPrefix(clean, base+string(filepath.Separator)) {
		return fmt.Errorf("apache2: path %q is outside website_basedir %q", clean, base)
	}
	return nil
}

// vhostFileName is the sites-available file of a domain. Apache only reads
// files matching its IncludeOptional glob, which on Debian is "*.conf".
func vhostFileName(domain string) string { return domain + ".vhost" }
