//go:build integration

package clientdb

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/database"
)

// startAdmin spins up a MariaDB container and returns a plugin wired to
// it via OpenAdmin, an open admin connection for direct assertions and
// the root DSN prefix ("root:root@tcp(host:port)").
func startAdmin(t *testing.T, suffix string) (*Plugin, *adminConn, string) {
	t.Helper()
	dsnPrefix, _ := database.StartMariaDB(t, suffix)
	p := NewPlugin(nil, nil, "", nil)
	p.OpenAdmin = func(context.Context) (*sql.DB, Config, error) {
		db, err := sql.Open("mysql", dsnPrefix+"/")
		return db, Config{Host: "127.0.0.1", User: "root", Password: "root"}, err
	}
	c, err := p.connect(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	return p, c, dsnPrefix
}

// dsnAddr extracts the host:port from a StartMariaDB DSN prefix.
func dsnAddr(t *testing.T, dsnPrefix string) string {
	t.Helper()
	start := strings.Index(dsnPrefix, "tcp(")
	end := strings.LastIndex(dsnPrefix, ")")
	require.True(t, start >= 0 && end > start)
	return dsnPrefix[start+4 : end]
}

// schemaCharset returns the DEFAULT_CHARACTER_SET_NAME of a schema, or
// "" when the schema does not exist.
func schemaCharset(t *testing.T, c *adminConn, name string) string {
	t.Helper()
	var charset string
	err := c.QueryRowContext(context.Background(),
		"SELECT DEFAULT_CHARACTER_SET_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME = ?", name).
		Scan(&charset)
	if err == sql.ErrNoRows {
		return ""
	}
	require.NoError(t, err)
	return charset
}

// TestCreateDeleteDatabase covers task 3.3: CREATE DATABASE with and
// without charset, DROP DATABASE, and the system-schema denylist.
func TestCreateDeleteDatabase(t *testing.T) {
	p, c, _ := startAdmin(t, "prov")
	ctx := context.Background()

	// with charset
	require.True(t, p.createDatabase(ctx, c, row{"database_name": "c1_app", "database_charset": "latin1"}))
	assert.Equal(t, "latin1", schemaCharset(t, c, "c1_app"))

	// without charset: server default applies, schema exists
	require.True(t, p.createDatabase(ctx, c, row{"database_name": "c1_plain"}))
	assert.NotEmpty(t, schemaCharset(t, c, "c1_plain"))

	// duplicate create fails gracefully (logged, false)
	assert.False(t, p.createDatabase(ctx, c, row{"database_name": "c1_app"}))

	// denylist refusals leave system schemas alone
	assert.False(t, p.createDatabase(ctx, c, row{"database_name": "mysql"}))
	assert.False(t, p.deleteDatabase(ctx, c, row{"database_name": "mysql"}))
	assert.NotEmpty(t, schemaCharset(t, c, "mysql"))

	// drop both
	require.True(t, p.deleteDatabase(ctx, c, row{"database_name": "c1_app"}))
	assert.Empty(t, schemaCharset(t, c, "c1_app"))
	require.True(t, p.deleteDatabase(ctx, c, row{"database_name": "c1_plain"}))

	// dropping a missing database fails gracefully
	assert.False(t, p.deleteDatabase(ctx, c, row{"database_name": "c1_app"}))
}
