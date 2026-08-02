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
// firewall.tform.php. Empty strings pass; valid tokens pass; everything
// else (letters, separators other than comma, ports >5 digits) fails.
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
		// Leading/trailing comma is rejected — PHP regex anchored at both ends.
		{",22", false},
		{"22,", false},
		// Letters rejected.
		{"abc", false},
		{"22,abc", false},
		// Semicolon rejected (we want comma only).
		{"21;22", false},
		// Six-digit port rejected at syntax level.
		{"123456", false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			assert.Equal(t, tt.want, PortListMatches(tt.in))
		})
	}
}