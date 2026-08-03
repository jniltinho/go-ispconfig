package powerdns

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStripTrailingDot(t *testing.T) {
	assert.Equal(t, "example.com", stripTrailingDot("example.com."))
	assert.Equal(t, "example.com", stripTrailingDot("example.com"))
	assert.Equal(t, "", stripTrailingDot(""))
	assert.Equal(t, "", stripTrailingDot("."))
}

func TestSOAContentPHPFixtures(t *testing.T) {
	// Active zone example.com. with absolute ns/mbox (PHP substr trailing dots).
	got := SOAContent("ns1.example.com.", "hostmaster.example.com.", "example.com",
		2026080201, 7200, 3600, 1209600, 86400)
	assert.Equal(t, "ns1.example.com hostmaster.example.com 2026080201 7200 3600 1209600 86400", got)

	// Relative ns → ns.origin; empty ns → origin.
	got = SOAContent("ns1", "hostmaster.example.com.", "example.com", 1, 2, 3, 4, 5)
	assert.Equal(t, "ns1.example.com hostmaster.example.com 1 2 3 4 5", got)

	got = SOAContent("", "a.b.", "example.com", 1, 2, 3, 4, 5)
	assert.Equal(t, "example.com a.b 1 2 3 4 5", got)
}

func TestRRNamePHPFixtures(t *testing.T) {
	origin := "example.com"
	assert.Equal(t, "www.example.com", RRName("www", origin))
	assert.Equal(t, "example.com", RRName("", origin))
	assert.Equal(t, "mail.other.net", RRName("mail.other.net.", origin))
	assert.Equal(t, "example.com", RRName(".", origin)) // strip to empty → origin via absoluteOrRelative? "." → ""
	// stripTrailingDot(".") = ""; absoluteOrRelative: not suffix after strip wait — HasSuffix(".", ".") true → strip → ""
	// RRName: absoluteOrRelative returns ""; then RRName returns origin. Good.
}

func TestRRContentPHPFixtures(t *testing.T) {
	origin := "example.com"
	// A: raw data
	assert.Equal(t, "1.2.3.4", RRContent("A", "1.2.3.4", origin))
	// MX relative → absolute
	assert.Equal(t, "mail.example.com", RRContent("MX", "mail", origin))
	// MX absolute
	assert.Equal(t, "mail.other.net", RRContent("MX", "mail.other.net.", origin))
	// CNAME / NS / PTR / SRV / ALIAS same rule
	assert.Equal(t, "target.example.com", RRContent("CNAME", "target", origin))
	assert.Equal(t, "ns2.example.com", RRContent("NS", "ns2", origin))
	// TXT raw
	assert.Equal(t, `"v=spf1 -all"`, RRContent("TXT", `"v=spf1 -all"`, origin))
}

func TestHINFOTransform(t *testing.T) {
	// PHP: between quotes (spaces→_), then substr($content, quote2_rel+2).
	// `"PC 386" UNIX`: between=PC_386, remainder starts at index 8 → " UNIX".
	assert.Equal(t, "PC_386 UNIX", transformHINFO(`"PC 386" UNIX`))
	// No quotes: unchanged
	assert.Equal(t, "PC-INTEL UNIX", transformHINFO("PC-INTEL UNIX"))
	// Single quote: unchanged
	assert.Equal(t, `"only`, transformHINFO(`"only`))
}
