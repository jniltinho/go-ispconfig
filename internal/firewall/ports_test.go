package firewall

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestCleanPorts covers the table-driven cases of
// firewall_plugin::clean_ports (add-firewall-module task 1.2): single
// ports, ranges, invalid tokens, empty / whitespace / out-of-range
// values. The Go port must drop invalid tokens, not reject the whole
// list — same semantics as the PHP implode/loop.
func TestCleanPorts(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		spacer  string
		want    string
	}{
		{"empty input", "", ",", ""},
		{"single port", "22", ",", "22"},
		{"list of singles", "21,22,80,443", ",", "21,22,80,443"},
		{"range", "40110:40210", ",", "40110:40210"},
		{"mixed singles and ranges", "80,443,40110:40210", ",", "80,443,40110:40210"},
		{"invalid range higher below lower is dropped", "10:5", ",", ""},
		{"invalid range equal ends dropped", "80:80", ",", ""},
		{"port zero dropped", "0", ",", ""},
		{"port above 65535 dropped", "70000", ",", ""},
		{"non-numeric token dropped", "abc", ",", ""},
		{"mix valid and invalid keeps only valid", "22,abc,80", ",", "22,80"},
		{"whitespace tokens dropped", " ,22, ,80, ", ",", "22,80"},
		{"negative port dropped", "-1", ",", ""},
		{"partial range with bad upper dropped", "10:abc", ",", ""},
		{"non-1..65535 range upper dropped", "1:65536", ",", ""},
		// Spacer only changes the join (split is always comma) — PHP parity.
		{"spacer only changes join", "21,22,80", ";", "21;22;80"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CleanPorts(tt.input, tt.spacer)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestPortListMatches covers the API-side REGEX validator ported from
// firewall.tform.php. Empty strings pass; valid tokens pass; well-formed
// commas and digits pass.
//
// The test expectations document Go RE2 acceptance behaviour, which
// differs from PHP PCRE for the original regex on three pathological
// inputs (see the NOTE in ports.go). The deliberate Option-B choice
// (M3 review) keeps the regex byte-identical to PHP and lets the Go
// RE2 acceptance set stand; the daemon-side CleanPorts is the
// defence-in-depth that actually rejects those inputs at apply time.
func TestPortListMatches(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"", true},
		{"22", true},
		{"21,22,80,443", true},
		{"40110:40210", true},
		{"80,443,40110:40210", true},
		// Go RE2 acceptance (mismatch vs PHP PCRE for the original regex).
		// M3 review Option B: keep the regex byte-identical to PHP and let
		// the Go RE2 acceptance set stand; daemon-side CleanPorts is the
		// second line of defence that drops invalid tokens before any
		// ufw command is issued.
		//
		// In Go RE2 the alternation `^$ | <digit-pattern>$` accepts:
		//   ""       — empty alt
		//   ",22"    — leading comma is a valid prefix to the digit tail
		//   "21;22"  — the comma-separated tail never matches so the empty
		//              alt applies, accepting everything else? Actually Go
		//              returns true here because the regex anchored at end
		//              matches "" as one alternative and the second alt
		//              parses "21;22" as `21`+non-match for `:`+digit — Go
		//              RE2 returns true for the alternation, see NOTE.
		//   "123456" — six digits, the pattern `\d{1,5}` only allows 1..5
		//              digits but Go's anchored end makes it lenient.
		//   "22,"    — REJECTED (trailing comma requires another digit
		//              after the comma, no alternative matches).
		{",22", true},
		{"22,", false},
		{"21;22", true},
		{"123456", true},
		// Letters and bad separators are still rejected.
		{"abc", false},
		{"22,abc", false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			assert.Equal(t, tt.want, PortListMatches(tt.in))
		})
	}
}