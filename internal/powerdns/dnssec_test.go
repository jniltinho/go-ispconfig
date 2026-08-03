package powerdns

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"go-ispconfig/internal/engine"
)

// pdnsutil (PowerDNS 4.x) show-zone output shape: "Active ( ALGO )".
var showZone4 = []string{
	"This is a Master zone",
	"Metadata items: None",
	"Zone has NSEC3 semantics, configuration: 1 0 10 deadbeef",
	"keys: ",
	"ID = 1 (KSK), flags = 257, tag = 25693, algo = 8, bits = 2048	Active ( RSASHA256 )",
	"KSK DNSKEY = example.com. IN DNSKEY 257 3 8 AwEAAcKSKKEY ; ( RSASHA256 )",
	"DS = example.com. IN DS 25693 8 1 aabbccdd ; ( SHA1 digest )",
	"DS = example.com. IN DS 25693 8 2 eeff0011 ; ( SHA256 digest )",
	"ID = 2 (ZSK), flags = 256, tag = 51234, algo = 8, bits = 1024	Active ( RSASHA256 )",
	"ID = 3 (KSK), flags = 257, tag = 999, algo = 8, bits = 2048	Inactive ( RSASHA256 )",
}

// pdnssec (PowerDNS 3.x) shape: "Active: 1".
var showZone3 = []string{
	"Zone is not presigned",
	"Zone has NSEC3 semantics",
	"keys: ",
	"ID = 7 (CSK), tag = 111, algo = 8, bits = 2048	Active: 1 ( RSASHA256 )",
	"CSK DNSKEY = example.com. IN DNSKEY 257 3 8 AwEAAcCSKKEY ; ( RSASHA256 )",
	"DS = example.com. IN DS 111 8 2 deadbeef ; ( SHA256 digest )",
}

func TestFormatDNSSECPubkeysPdnsutil4(t *testing.T) {
	got := FormatDNSSECPubkeys(showZone4)
	assert.Equal(t, []string{
		"KSK key tag: 25693",
		"Algo: 8 (RSASHA256)",
		"Bits: 2048",
		"DNSKEY: AwEAAcKSKKEY",
		"  - DS key tag: 25693",
		"    Algo: 8",
		"    Digest: 1 (SHA1 digest)",
		"    Public key: aabbccdd",
		"  - DS key tag: 25693",
		"    Algo: 8",
		"    Digest: 2 (SHA256 digest)",
		"    Public key: eeff0011",
	}, got)
}

func TestFormatDNSSECPubkeysPdnssec3(t *testing.T) {
	got := FormatDNSSECPubkeys(showZone3)
	assert.Equal(t, []string{
		"CSK key tag: 111",
		"Algo: 8 (RSASHA256)",
		"Bits: 2048",
		"DNSKEY: AwEAAcCSKKEY",
		"  - DS key tag: 111",
		"    Algo: 8",
		"    Digest: 2 (SHA256 digest)",
		"    Public key: deadbeef",
	}, got)
}

func TestFormatDNSSECPubkeysSkipsZSKAndInactive(t *testing.T) {
	got := FormatDNSSECPubkeys([]string{
		"skip1", "skip2",
		"ID = 2 (ZSK), flags = 256, tag = 51234, algo = 8, bits = 1024	Active ( RSASHA256 )",
		"ID = 3 (KSK), flags = 257, tag = 999, algo = 8, bits = 2048	Inactive ( RSASHA256 )",
	})
	assert.Empty(t, got)
}

func TestFormatDNSSECPubkeysShortInput(t *testing.T) {
	assert.Empty(t, FormatDNSSECPubkeys(nil))
	assert.Empty(t, FormatDNSSECPubkeys([]string{"one", "two"}))
}

// dnssecPlugin wires a plugin with a stubbed pdnsutil returning show-zone
// output; panelDB stays nil (SQL side is covered by integration tests).
func dnssecPlugin(t *testing.T) (*Plugin, *pathRunner) {
	t.Helper()
	r := &pathRunner{bins: map[string]string{"pdnsutil": strings.Join(showZone4, "\n")}}
	p := NewPlugin(nil, nil, nil, r, 1, nil)
	p.SetToolsForTest("pdns_control", "pdnsutil", "4.8.0")
	r.log = nil
	return p, r
}

func TestHandleDNSSECCreate(t *testing.T) {
	p, r := dnssecPlugin(t)
	p.doHandleDNSSEC(context.Background(), engine.Data{
		New: map[string]any{"id": 5, "origin": "example.com.", "dnssec_wanted": "Y"},
		Old: map[string]any{"id": 5, "origin": "example.com.", "dnssec_wanted": "N"},
	})
	assert.Equal(t, []string{
		"pdnsutil add-zone-key example.com ksk active 2048 rsasha256",
		"pdnsutil add-zone-key example.com zsk active 1024 rsasha256",
		"pdnsutil set-nsec3 example.com 1 0 10 deadbeef",
		"pdnsutil show-zone example.com",
	}, r.log)
}

func TestHandleDNSSECDisable(t *testing.T) {
	p, r := dnssecPlugin(t)
	p.doHandleDNSSEC(context.Background(), engine.Data{
		New: map[string]any{"id": 5, "origin": "example.com.", "dnssec_wanted": "N"},
		Old: map[string]any{"id": 5, "origin": "example.com.", "dnssec_wanted": "Y"},
	})
	assert.Equal(t, []string{"pdnsutil disable-dnssec example.com"}, r.log)
}

// Origin change on an initialized zone disables the old zone first, then
// re-creates keys under the new origin.
func TestHandleDNSSECOriginChange(t *testing.T) {
	p, r := dnssecPlugin(t)
	p.doHandleDNSSEC(context.Background(), engine.Data{
		New: map[string]any{"id": 5, "origin": "new.com.", "dnssec_wanted": "Y"},
		Old: map[string]any{
			"id": 5, "origin": "old.com.",
			"dnssec_wanted": "N", "dnssec_initialized": "Y",
		},
	})
	assert.Equal(t, "pdnsutil disable-dnssec old.com", r.log[0])
	assert.Contains(t, strings.Join(r.log, "\n"), "pdnsutil add-zone-key new.com ksk")
}

// Unsupported major version and missing binary must not shell out.
func TestHandleDNSSECVersionGate(t *testing.T) {
	data := engine.Data{
		New: map[string]any{"id": 5, "origin": "example.com.", "dnssec_wanted": "Y"},
		Old: map[string]any{"id": 5, "dnssec_wanted": "N"},
	}
	for _, tc := range []struct{ util, version string }{
		{"pdnsutil", "2.9.22"},
		{"", "4.8.0"},
	} {
		p, r := dnssecPlugin(t)
		p.SetToolsForTest("pdns_control", tc.util, tc.version)
		p.doHandleDNSSEC(context.Background(), data)
		assert.Empty(t, r.log, "util=%q version=%q", tc.util, tc.version)
	}
}

func TestOutputLines(t *testing.T) {
	assert.Equal(t, []string{"a", "b"}, outputLines([]byte("a\r\nb\r\n")))
	assert.Nil(t, outputLines(nil))
}
