package api

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/validator"
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

// TestCheckRemoteIPList covers task 4.3 (valid_ip_list parity): empty
// list ok, IPs and hostnames accepted, garbage rejected.
func TestCheckRemoteIPList(t *testing.T) {
	assert.Empty(t, checkRemoteIPList(nil, ""))
	assert.Empty(t, checkRemoteIPList(nil, "  "))
	assert.Empty(t, checkRemoteIPList(nil, "10.0.0.1"))
	assert.Empty(t, checkRemoteIPList(nil, "10.0.0.1, 192.168.1.2"))
	assert.Empty(t, checkRemoteIPList(nil, "2001:db8::1"))
	assert.Empty(t, checkRemoteIPList(nil, "db.example.com"))
	assert.Empty(t, checkRemoteIPList(nil, "10.0.0.1,db.example.com"))
	assert.Equal(t, "database_remote_error_ips", checkRemoteIPList(nil, "not valid!"))
	assert.Equal(t, "database_remote_error_ips", checkRemoteIPList(nil, "10.0.0.1,,10.0.0.2"))
	assert.Equal(t, "database_remote_error_ips", checkRemoteIPList(nil, "under_score.host"))
}

// TestCheckDatabaseCharset: only the tform SELECT set is accepted.
func TestCheckDatabaseCharset(t *testing.T) {
	for _, ok := range []string{"", "latin1", "utf8", "utf8mb4"} {
		assert.Empty(t, checkDatabaseCharset(nil, ok), ok)
	}
	assert.Equal(t, "database_charset_error_regex", checkDatabaseCharset(nil, "latin2"))
	assert.Equal(t, "database_charset_error_regex", checkDatabaseCharset(nil, "utf8mb4; DROP"))
}

// TestDatabaseNameBlacklisted: panel DB and mysql are refused.
func TestDatabaseNameBlacklisted(t *testing.T) {
	assert.True(t, databaseNameBlacklisted("mysql", "dbispconfig"))
	assert.True(t, databaseNameBlacklisted("dbispconfig", "dbispconfig"))
	assert.False(t, databaseNameBlacklisted("c1_app", "dbispconfig"))
	assert.False(t, databaseNameBlacklisted("", ""), "empty name is the NOTEMPTY rule's job")
}

// TestDatabaseNameRules: the tform regex accepts 2–64 word chars on the
// full (prefixed) name.
func TestDatabaseNameRules(t *testing.T) {
	rules := databaseNameRules()
	vc := &validator.Context{}
	check := func(value string) []string {
		var keys []string
		for _, r := range rules {
			key, err := r.Validate(vc, value)
			require.NoError(t, err)
			if key != "" {
				keys = append(keys, key)
			}
		}
		return keys
	}
	assert.Empty(t, check("c1_app"))
	assert.Contains(t, check(""), "database_name_error_empty")
	assert.Contains(t, check("a"), "database_name_error_regex")
	assert.Contains(t, check("bad-name"), "database_name_error_regex")
	assert.Contains(t, check(strings.Repeat("a", 65)), "database_name_error_regex")
}

// TestPasswordStrength covers the validate_password score table.
func TestPasswordStrength(t *testing.T) {
	assert.Equal(t, 1, passwordStrength("abc"))
	assert.Equal(t, 1, passwordStrength("abcde"))
	assert.Equal(t, 2, passwordStrength("abcdefg"))
	assert.Equal(t, 3, passwordStrength("abcdefghi"))
	assert.Equal(t, 3, passwordStrength("Abc1def"))         // points 2, len 7
	assert.Equal(t, 5, passwordStrength("Abc1defghijk"))    // points 2, len > 10
	assert.Equal(t, 5, passwordStrength("Abc1!defg"))       // points 3, len 9
	assert.Equal(t, 3, passwordStrength("A1b!cd"))          // points 3, len 6
	assert.Equal(t, 5, passwordStrength("Str0ng!Password")) // points 3, len 15
	assert.Equal(t, 1, passwordStrength("123456"))          // digits only, len 6
}

// TestCheckPasswordPolicy: defaults (len 8, strength 0) without a DB.
func TestCheckPasswordPolicy(t *testing.T) {
	assert.Empty(t, checkPasswordPolicy(nil, ""), "empty means unchanged")
	assert.Equal(t, "weak_password_txt", checkPasswordPolicy(nil, "short"))
	assert.Empty(t, checkPasswordPolicy(nil, "longenough8"))
}

// TestDatabaseUserBlacklisted: root/mysql/panel user refused.
func TestDatabaseUserBlacklisted(t *testing.T) {
	assert.True(t, databaseUserBlacklisted("root", "ispconfig"))
	assert.True(t, databaseUserBlacklisted("mysql", ""))
	assert.True(t, databaseUserBlacklisted("ispconfig", "ispconfig"))
	assert.False(t, databaseUserBlacklisted("c1_app", "ispconfig"))
}

// TestCropName: 64/32-char crops of database_edit/database_user_edit.
func TestCropName(t *testing.T) {
	long := "c1_abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz0123456789"
	assert.Len(t, cropName(long, mysqlDatabaseNameMax), 64)
	assert.Len(t, cropName(long, mysqlUserNameMax), 32)
	assert.Equal(t, "c1_app", cropName("c1_app", mysqlDatabaseNameMax))
}

// TestConvertClientName pins tools_sites::convertClientName, the sanitiser the
// legacy runs on a group name before it is prepended to a username. Reachable
// only since the panel seeds a non-empty [sites] prefix.
func TestConvertClientName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"clienta", "clienta"},
		{"Acme", "acme"},
		{"Acme Ltd", "acmeltd"},        // spaces are dropped, not replaced
		{"acme.ltd", "acme_ltd"},       // anything else becomes an underscore
		{"a-b", "a_b"},
		{"under_score", "under_score"}, // already allowed
		{"", "default"},                // a nameless group must still namespace
		{"   ", "default"},
		{"café", "caf__"},              // PHP indexes bytes: two for the accent
	}
	for _, tc := range tests {
		assert.Equal(t, tc.want, convertClientName(tc.in), "convertClientName(%q)", tc.in)
	}
}

// A sanitised name must reach the expanded prefix, not just the helper.
func TestExpandPrefixUsesSanitisedName(t *testing.T) {
	got := expandPrefixPlaceholders("[CLIENTNAME]", convertClientName("Acme Ltd."), "3", 7)
	assert.Equal(t, "acmeltd_", got, "a group name with a space and a dot must not reach a username raw")
}

// An unresolved placeholder must never reach a stored name: brackets fail the
// name regex and the error would blame what the caller typed.
func TestUnresolvedPrefixIsDropped(t *testing.T) {
	assert.Equal(t, "", expandSitesPrefix(context.Background(), nil, nil, "c[CLIENTID]", nil),
		"no identity and no parent site leaves nothing to expand")
	assert.Equal(t, "", expandSitesPrefix(context.Background(), nil, nil, "[CLIENTNAME]", nil))
	assert.Equal(t, "", expandSitesPrefix(context.Background(), nil, nil, "", nil),
		"an empty template stays empty")
}

