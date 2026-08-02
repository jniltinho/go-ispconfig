// Package firewall implements the daemon firewall module (openspec
// change add-firewall-module): the UFW apply plugin and the table-hook
// Module that maps sys_datalog changes to firewall_insert/update/delete
// events. Code-only — no schema changes; the firewall table exists
// byte-identical in internal/database/ispconfig3.sql.
package firewall

import (
	"regexp"
	"strconv"
	"strings"
)

// minPort and maxPort are the IANA valid range for TCP/UDP ports.
const (
	minPort = 1
	maxPort = 65535
)

// portRegex is the API-side validator ported byte-for-byte from
// firewall.tform.php (`tcp_ports_error_regex` / `udp_ports_error_regex`):
//
//	/^$|\d{1,5}(?::\d{1,5})?(?:,\d{1,5}(?::\d{1,5})?)*$/
//
// Empty input is allowed; otherwise each token is one to five digits,
// optionally followed by `:digits` (a range); tokens are joined by
// commas. This is purely a syntax check: range bounds and 1..65535 are
// enforced by CleanPorts at apply time.
//
// NOTE — Go RE2 vs PHP PCRE divergence (intentional, add-firewall-module
// task 1.2 / M3 review decision "Option B"). The Go regexp engine is
// RE2 and matches the PHP pattern literally (anchored at string start
// and end), but the alternation `^$ | <digit-pattern>$` has subtly
// different acceptance behaviour from PHP PCRE for pathological inputs
// like `",22"`, `"21;22"` and `"123456"`. We deliberately keep the
// regex byte-identical to PHP so the validator stays a faithful
// port of the tform behaviour; if upstream ISPConfig ever tightens it,
// both implementations can be patched together. Unit tests in
// ports_test.go document the actual Go RE2 acceptance set.
var portRegex = regexp.MustCompile(`^$|\d{1,5}(?::\d{1,5})?(?:,\d{1,5}(?::\d{1,5})?)*$`)

// PortListMatches reports whether s is a syntactically valid port list
// for the API-side REGEX validator (design D4 / task 1.2). The match
// does not enforce range semantics; daemon-side CleanPorts is the
// defence-in-depth cleanup of migrated / hand-edited rows.
func PortListMatches(s string) bool {
	return portRegex.MatchString(s)
}

// CleanPorts is the pure port of firewall_plugin::clean_ports($list,
// $spacer) from base/ispconfig3_install/server/plugins-available/
// firewall_plugin.inc.php. It splits the input on comma, validates each
// token against 1..65535 (or, for ranges, lower < higher), and rejoins
// survivors with the spacer. Empty input yields an empty string.
//
// Spacer is typically "," for the UFW apply path. Range tokens are
// written as "lower:higher".
func CleanPorts(portlist, spacer string) string {
	parts := strings.Split(portlist, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if cleaned, ok := cleanToken(p); ok {
			out = append(out, cleaned)
		}
	}
	return strings.Join(out, spacer)
}

// cleanToken normalizes one comma-separated token. A range "a:b" is
// kept as "a:b" when 1 <= a < b <= 65535; a single port is kept when
// 1 <= p <= 65535. Anything else is dropped (returns "", false).
func cleanToken(p string) (string, bool) {
	if idx := strings.Index(p, ":"); idx >= 0 {
		lo, err1 := strconv.Atoi(p[:idx])
		hi, err2 := strconv.Atoi(p[idx+1:])
		if err1 != nil || err2 != nil {
			return "", false
		}
		if lo < minPort || lo > maxPort || hi < minPort || hi > maxPort || lo >= hi {
			return "", false
		}
		return strconv.Itoa(lo) + ":" + strconv.Itoa(hi), true
	}
	n, err := strconv.Atoi(p)
	if err != nil {
		return "", false
	}
	if n < minPort || n > maxPort {
		return "", false
	}
	return strconv.Itoa(n), true
}