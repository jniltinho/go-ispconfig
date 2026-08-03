package powerdns

import (
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeriveDSNFromPanel(t *testing.T) {
	got, err := DeriveDSN(
		"ispconfig:s3cret@tcp(127.0.0.1:3306)/dbispconfig?charset=utf8mb4&parseTime=True&loc=Local",
		"",
	)
	require.NoError(t, err)
	cfg, err := mysql.ParseDSN(got)
	require.NoError(t, err)
	assert.Equal(t, DatabaseName, cfg.DBName)
	assert.Equal(t, "ispconfig", cfg.User)
	assert.Equal(t, "s3cret", cfg.Passwd)
	assert.Equal(t, "127.0.0.1:3306", cfg.Addr)
	assert.Equal(t, "utf8mb4", cfg.Params["charset"])
	assert.True(t, cfg.ParseTime)
}

func TestDeriveDSNOverride(t *testing.T) {
	override := "root:root@tcp(db:3306)/powerdns?parseTime=true"
	got, err := DeriveDSN("ignored@tcp(x)/dbispconfig", override)
	require.NoError(t, err)
	assert.Equal(t, override, got)

	// Whitespace-only override falls through to derive.
	got, err = DeriveDSN("u:p@tcp(127.0.0.1:3306)/other", "  ")
	require.NoError(t, err)
	cfg, err := mysql.ParseDSN(got)
	require.NoError(t, err)
	assert.Equal(t, DatabaseName, cfg.DBName)
}

func TestDeriveDSNErrors(t *testing.T) {
	_, err := DeriveDSN("", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty panel DSN")

	_, err = DeriveDSN("not-a-dsn", "")
	require.Error(t, err)
}

func TestOpenUnreachable(t *testing.T) {
	// Port nothing listens on — Open must fail clearly without hanging forever
	// (mysql driver default timeouts are short enough for unit tests).
	db, err := Open("", "root:root@tcp(127.0.0.1:1)/powerdns?timeout=1s&readTimeout=1s&writeTimeout=1s")
	require.Error(t, err)
	assert.Nil(t, db)
	assert.True(t, strings.Contains(err.Error(), "powerdns") || strings.Contains(err.Error(), "unreachable") ||
		strings.Contains(err.Error(), "connect"), "got: %v", err)
	assert.Contains(t, err.Error(), "powerdns")
}
