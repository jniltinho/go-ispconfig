package mail

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateDeriveValidateDKIM(t *testing.T) {
	priv, pub, err := GenerateDKIMKey(1024) // small for test speed
	require.NoError(t, err)
	assert.Contains(t, priv, "RSA PRIVATE KEY")
	assert.Contains(t, pub, "BEGIN PUBLIC KEY")

	derived, err := DeriveDKIMPublic(priv)
	require.NoError(t, err)
	assert.Equal(t, pub, derived, "derived public matches the generated one")

	_, err = ParseDKIMPrivate("not a key")
	assert.ErrorIs(t, err, ErrInvalidDKIMKey)
	_, err = ParseDKIMPrivate("-----BEGIN RSA PRIVATE KEY-----\ngarbage\n-----END RSA PRIVATE KEY-----")
	assert.ErrorIs(t, err, ErrInvalidDKIMKey)

	// Weak strength floors to 2048.
	privBig, _, err := GenerateDKIMKey(0)
	require.NoError(t, err)
	key, err := ParseDKIMPrivate(privBig)
	require.NoError(t, err)
	assert.Equal(t, 2048, key.N.BitLen())
}

func TestDKIMSelectorAndRecord(t *testing.T) {
	assert.True(t, ValidDKIMSelector("default"))
	assert.True(t, ValidDKIMSelector("k2026"))
	assert.True(t, ValidDKIMSelector("default.aug"), "two-part selector, as the tform regex allows")
	for _, bad := range []string{"", "Bad", "sel_ector", "a-b", "a.b.c", "a.", strings.Repeat("x", 64)} {
		assert.False(t, ValidDKIMSelector(bad), bad)
	}
	assert.Equal(t, "default._domainkey.example.com.", DKIMRecordName("default", "example.com"))

	// mail_domain_edit.htm:170 verbatim: three spaces before IN, tabs
	// around TXT.
	assert.Equal(t,
		"default._domainkey.example.com. 3600   IN\tTXT\t\"v=DKIM1; t=s; p=AAAA\"",
		DKIMSuggestedRecord(DKIMRecordName("default", "example.com"), "v=DKIM1; t=s; p=AAAA"))
}

func TestDKIMTXTValue(t *testing.T) {
	pub := "-----BEGIN PUBLIC KEY-----\nAAAA\nBBBB\n-----END PUBLIC KEY-----\n"
	assert.Equal(t, "v=DKIM1; t=s; p=AAAABBBB", DKIMTXTValue(pub))
}
