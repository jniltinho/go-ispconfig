// Package getconf ports ISPConfig's getconf/ini_parser: it parses the
// serialized INI text stored in server.config and sys_ini.config into
// section maps and typed structs, and reads sys_config key/value entries
// (design D8: runtime server behavior lives in the database).
package getconf

import (
	"regexp"
	"strings"
)

// Sections is a parsed INI document: section name (lowercased) → key → value.
type Sections map[string]map[string]string

var (
	sectionRe = regexp.MustCompile(`^\[([\w\d_]+)\]$`)
	itemRe    = regexp.MustCompile(`^([\w\d_]+)=(.*)$`)
)

// ParseINI parses INI text with the exact semantics of ISPConfig's
// ini_parser::parse_ini_string: section headers `[name]` (lowercased), items
// `key=value` with both sides trimmed, lines outside any section or not
// matching either pattern silently ignored.
func ParseINI(ini string) Sections {
	config := Sections{}
	section := ""
	for _, line := range strings.Split(strings.ReplaceAll(ini, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if m := sectionRe.FindStringSubmatch(line); m != nil {
			section = strings.ToLower(m[1])
			continue
		}
		if section == "" {
			continue
		}
		if m := itemRe.FindStringSubmatch(line); m != nil {
			if config[section] == nil {
				config[section] = map[string]string{}
			}
			config[section][strings.TrimSpace(m[1])] = strings.TrimSpace(m[2])
		}
	}
	return config
}

// StripSlashes removes one level of backslash escaping, like PHP's
// stripslashes, which ISPConfig applies to server.config and sys_ini.config
// before parsing.
func StripSlashes(s string) string {
	if !strings.ContainsRune(s, '\\') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' {
			i++
			if i >= len(s) {
				// PHP stripslashes drops a trailing lone backslash:
				// verified with PHP 8.2, stripslashes("a\") == "a".
				break
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
