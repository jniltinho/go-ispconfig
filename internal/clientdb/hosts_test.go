package clientdb

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestHostList covers design D4 (port of getHostList): localhost always,
// remote_access gating, IP filtering, '%' fallback, unique + sorted.
func TestHostList(t *testing.T) {
	tests := []struct {
		name         string
		remoteAccess string
		remoteIps    string
		want         []string
	}{
		{"no remote access", "n", "10.0.0.1", []string{"localhost"}},
		{"remote with empty ips wildcard", "y", "", []string{"%", "localhost"}},
		{"remote with valid ips", "y", "10.0.0.2,10.0.0.1", []string{"10.0.0.1", "10.0.0.2", "localhost"}},
		{"invalid ips filtered", "y", "not-an-ip,10.0.0.1", []string{"10.0.0.1", "localhost"}},
		{"all invalid falls back to wildcard", "y", "nope,also-nope", []string{"%", "localhost"}},
		{"whitespace trimmed", "y", " 10.0.0.1 , 192.168.1.5 ", []string{"10.0.0.1", "192.168.1.5", "localhost"}},
		{"ipv6 accepted", "y", "2001:db8::1", []string{"2001:db8::1", "localhost"}},
		{"duplicates collapsed", "y", "10.0.0.1,10.0.0.1", []string{"10.0.0.1", "localhost"}},
		{"empty record", "", "", []string{"localhost"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hostList(row{"remote_access": tt.remoteAccess, "remote_ips": tt.remoteIps})
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestUnionHostLists: union of several database host lists, unique with
// first-seen order (PHP array_unique parity in getOtherHostList).
func TestUnionHostLists(t *testing.T) {
	got := unionHostLists([]row{
		{"remote_access": "y", "remote_ips": "10.0.0.1"},
		{"remote_access": "n", "remote_ips": ""},
		{"remote_access": "y", "remote_ips": "10.0.0.2,10.0.0.1"},
	})
	assert.Equal(t, []string{"10.0.0.1", "localhost", "10.0.0.2"}, got)

	assert.Empty(t, unionHostLists(nil))
}
