// Package getconf ports ISPConfig's getconf/ini_parser: it parses the
// serialized INI text stored in server.config and sys_ini.config into
// section maps and typed structs, and reads sys_config key/value entries
// (design D8: runtime server behavior lives in the database).
package getconf

import (
	"maps"
	"regexp"
	"slices"
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

// String re-serialises the document into the INI text stored in
// server.config (port of ini_parser::get_ini_string). PHP preserves the
// original key order; Go maps have none, so sections and keys are written
// sorted — the daemon parses by name, and a stable order keeps a
// read/modify/write round trip diff-free.
func (s Sections) String() string {
	var b strings.Builder
	for _, name := range slices.Sorted(maps.Keys(s)) {
		b.WriteString("[")
		b.WriteString(name)
		b.WriteString("]\n")
		for _, key := range slices.Sorted(maps.Keys(s[name])) {
			b.WriteString(key)
			b.WriteString("=")
			b.WriteString(s[name][key])
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	return b.String()
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
