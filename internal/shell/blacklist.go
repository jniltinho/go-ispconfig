// Package shell implements the shell-user side of the sites module
// (openspec change add-ftp-shell-module): the embedded username blacklist
// shared by the API validators and, later, the daemon plugin that is the Go
// port of ISPConfig3's server/plugins-available/shelluser_base_plugin.inc.php.
package shell

import (
	_ "embed"
	"strings"
)

// blacklistFile is the verbatim port of ISPConfig3's
// interface/lib/shelluser_blacklist: one system account name per line that a
// shell user may never be called.
//
//go:embed shelluser_blacklist
var blacklistFile string

// blacklist holds the lowercased entries of blacklistFile.
var blacklist = func() map[string]struct{} {
	m := map[string]struct{}{}
	for line := range strings.SplitSeq(blacklistFile, "\n") {
		if name := strings.ToLower(strings.TrimSpace(line)); name != "" {
			m[name] = struct{}{}
		}
	}
	return m
}()

// Blacklisted reports whether username is a reserved system account name.
// The comparison is trimmed and case-insensitive, like the PHP loop in
// shell_user_edit.php onBeforeInsert/onBeforeUpdate. Callers must check the
// name the operator typed, before the shelluser_prefix is prepended.
func Blacklisted(username string) bool {
	_, found := blacklist[strings.ToLower(strings.TrimSpace(username))]
	return found
}
