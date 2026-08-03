package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestDNSWiringMatrix covers the daemon DNS bootstrap matrix (tasks 3.5 and
// 4.4 of add-dns-powerdns-module): exactly one backend plugin per DNS server,
// dns_resign (Bind) never on the powerdns path, nothing on non-DNS servers.
func TestDNSWiringMatrix(t *testing.T) {
	tests := []struct {
		name      string
		dnsServer int8
		backend   string
		want      dnsWiring
	}{
		{"non-dns server", 0, "powerdns", dnsWiring{}},
		{"dns server default backend", 1, "", dnsWiring{Module: true, Bind: true}},
		{"dns server bind", 1, "bind", dnsWiring{Module: true, Bind: true}},
		{"dns server unknown backend", 1, "djbdns", dnsWiring{Module: true, Bind: true}},
		{"dns server powerdns", 1, "powerdns", dnsWiring{Module: true, PowerDNS: true}},
		{"dns server powerdns case-insensitive", 1, " PowerDNS ", dnsWiring{Module: true, PowerDNS: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, dnsWiringFor(tt.dnsServer, tt.backend))
		})
	}
}
