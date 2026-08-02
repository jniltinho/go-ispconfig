// Package nginx implements the nginx server plugin of the web module
// (openspec change add-web-nginx-module): site filesystem provisioning,
// vhost generation from nginx_vhost.conf.master, custom directive merging
// with blacklist enforcement, nginx -t validated activation with rollback,
// and PHP-FPM pool lifecycle. It is the Go port of the nginx-only paths of
// ISPConfig3's server/plugins-available/nginx_plugin.inc.php.
package nginx

import (
	_ "embed"
	"fmt"
	"regexp"
	"strings"
)

// blacklistRaw is the ISPConfig security/nginx_directives.blacklist file,
// embedded verbatim: one PCRE per line (PHP delimiter syntax, e.g.
// /^\s*(load_module)(\s+|[\\\\])/mi) matching forbidden custom directives.
//
//go:embed security/nginx_directives.blacklist
var blacklistRaw string

// blacklist holds the compiled blacklist patterns, converted from the PHP
// /pattern/flags syntax at package init. The embedded file is static, so a
// conversion failure is a programming error caught by the package tests.
var blacklist = mustCompileBlacklist(blacklistRaw)

// mustCompileBlacklist converts each non-empty line of the PHP-style
// blacklist into a Go regexp: the surrounding /.../ delimiters are stripped
// and the trailing flags become an inline (?...) group (only i, m and s are
// meaningful for RE2).
func mustCompileBlacklist(raw string) []*regexp.Regexp {
	var patterns []*regexp.Regexp
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		body := line
		if strings.HasPrefix(line, "/") {
			end := strings.LastIndex(line, "/")
			if end > 0 {
				flags := ""
				for _, f := range line[end+1:] {
					if strings.ContainsRune("ims", f) {
						flags += string(f)
					}
				}
				body = line[1:end]
				if flags != "" {
					body = "(?" + flags + ")" + body
				}
			}
		}
		patterns = append(patterns, regexp.MustCompile(body))
	}
	return patterns
}

// isBlacklisted reports whether one directive line matches a blacklist
// pattern.
func isBlacklisted(line string) bool {
	for _, re := range blacklist {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}

// BlockedDirectives returns the trimmed blacklisted lines of a custom
// nginx_directives value. The sites API uses it to reject offending
// directives at save time with a per-field validation error; the daemon's
// render-time strip (filterBlacklistedDirectives) stays as defense in depth.
func BlockedDirectives(directives string) []string {
	var blocked []string
	for line := range strings.SplitSeq(directives, "\n") {
		if isBlacklisted(line) {
			blocked = append(blocked, strings.TrimSpace(line))
		}
	}
	return blocked
}

// filterBlacklistedDirectives strips every line of the custom nginx
// directives that matches a blacklist pattern (render-time defense in depth;
// the sites API rejects the same lines at save time). It returns the kept
// text and one error per rejected line for datalog error reporting.
func filterBlacklistedDirectives(directives string) (kept string, rejected []error) {
	if directives == "" {
		return "", nil
	}
	lines := strings.Split(directives, "\n")
	out := lines[:0]
	for _, line := range lines {
		if isBlacklisted(line) {
			rejected = append(rejected,
				fmt.Errorf("nginx: blacklisted directive rejected: %s", strings.TrimSpace(line)))
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n"), rejected
}
