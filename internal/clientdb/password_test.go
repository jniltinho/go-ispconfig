package clientdb

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVersionLess(t *testing.T) {
	assert.True(t, versionLess("5.5.68", "5.7"))
	assert.True(t, versionLess("5.7", "8.0"))
	assert.False(t, versionLess("5.7.44", "5.7"))
	assert.False(t, versionLess("8.0.36", "5.7"))
	assert.False(t, versionLess("8.0", "8.0"))
	assert.True(t, versionLess("0.0.0-unknown", "5.7"))
	assert.True(t, versionLess("10.11.6", "11.0"))
	assert.False(t, versionLess("10.11.6", "5.7"))
}

// TestPasswordStatement covers the design-D6 decision tree with stubbed
// server type/version (task 3.4).
func TestPasswordStatement(t *testing.T) {
	const native = "*2470C0C06DEE42FD1618BB99005ADCA2EC9D1E19"
	const sha2 = "$A$005$abcdefghijklmnopqrstuvwxyz012345"

	tests := []struct {
		name       string
		dbType     string
		version    string
		nativeHash string
		sha2Hash   string
		wantQuery  string
		wantPlugin string
	}{
		{
			"mariadb uses SET PASSWORD", "mariadb", "10.11.6", native, sha2,
			"SET PASSWORD FOR 'c1_app'@'localhost' = '" + native + "'",
			"mysql_native_password",
		},
		{
			"mysql < 5.7 uses SET PASSWORD", "mysql", "5.5.68", native, "",
			"SET PASSWORD FOR 'c1_app'@'localhost' = '" + native + "'",
			"mysql_native_password",
		},
		{
			"mysql 5.7 without sha2 uses native ALTER USER", "mysql", "5.7.44", native, "",
			"ALTER USER IF EXISTS 'c1_app'@'localhost' IDENTIFIED WITH mysql_native_password AS '" + native + "'",
			"mysql_native_password",
		},
		{
			"mysql 8 with sha2 uses caching_sha2_password", "mysql", "8.0.36", native, sha2,
			"ALTER USER IF EXISTS 'c1_app'@'localhost' IDENTIFIED WITH caching_sha2_password AS '" + sha2 + "'",
			"caching_sha2_password",
		},
		{
			"mysql 8 without sha2 falls back to native", "mysql", "8.0.36", native, "",
			"ALTER USER IF EXISTS 'c1_app'@'localhost' IDENTIFIED WITH mysql_native_password AS '" + native + "'",
			"mysql_native_password",
		},
		{
			"mysql 5.7 with sha2 still native", "mysql", "5.7.44", native, sha2,
			"ALTER USER IF EXISTS 'c1_app'@'localhost' IDENTIFIED WITH mysql_native_password AS '" + native + "'",
			"mysql_native_password",
		},
		{
			"empty native hash locks the account", "mariadb", "10.6.1", "", "",
			"SET PASSWORD FOR 'c1_app'@'localhost' = '" + invalidPasswordPlaceholder + "'",
			"mysql_native_password",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query, plugin := passwordStatement(tt.dbType, tt.version, "c1_app", "localhost", tt.nativeHash, tt.sha2Hash)
			assert.Equal(t, tt.wantQuery, query)
			assert.Equal(t, tt.wantPlugin, plugin)
		})
	}
}
