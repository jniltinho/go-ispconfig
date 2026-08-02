package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestExpandPrefixPlaceholders covers task 4.2 (tools_sites
// replacePrefix parity): placeholder expansion and literal preservation.
func TestExpandPrefixPlaceholders(t *testing.T) {
	assert.Equal(t, "", expandPrefixPlaceholders("", "acme", "7", 12))
	assert.Equal(t, "c7_", expandPrefixPlaceholders("c[CLIENTID]_", "acme", "7", 12))
	assert.Equal(t, "acme_", expandPrefixPlaceholders("[CLIENTNAME]_", "acme", "7", 0))
	assert.Equal(t, "d12_", expandPrefixPlaceholders("d[DOMAINID]_", "acme", "7", 12))
	assert.Equal(t, "d[DOMAINID]_", expandPrefixPlaceholders("d[DOMAINID]_", "acme", "7", 0),
		"missing domain keeps the literal (PHP parity)")
	assert.Equal(t, "c[CLIENTID]_", expandPrefixPlaceholders("c[CLIENTID]_", "[CLIENTNAME]", "[CLIENTID]", 0),
		"unresolvable client keeps the literal")
	assert.Equal(t, "plain_", expandPrefixPlaceholders("plain_", "acme", "7", 12))
}

// TestKeepStoredPrefix: getPrefix parity — an existing prefix (even "")
// wins; '#' means not recorded yet and falls back to the template.
func TestKeepStoredPrefix(t *testing.T) {
	assert.Equal(t, "c1_", keepStoredPrefix("c1_", "c9_"))
	assert.Equal(t, "", keepStoredPrefix("", "c9_"), "explicit no-prefix is preserved")
	assert.Equal(t, "c9_", keepStoredPrefix("#", "c9_"))
}

// TestCropName: 64/32-char crops of database_edit/database_user_edit.
func TestCropName(t *testing.T) {
	long := "c1_abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz0123456789"
	assert.Len(t, cropName(long, mysqlDatabaseNameMax), 64)
	assert.Len(t, cropName(long, mysqlUserNameMax), 32)
	assert.Equal(t, "c1_app", cropName("c1_app", mysqlDatabaseNameMax))
}
