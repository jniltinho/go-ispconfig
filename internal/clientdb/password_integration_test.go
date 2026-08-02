//go:build integration

package clientdb

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSetPasswordIntegration covers task 3.4 against real MariaDB: the
// probe detects mariadb, the native SET PASSWORD path applies a stored
// hash and the user can then authenticate with the plaintext.
func TestSetPasswordIntegration(t *testing.T) {
	p, c, dsnPrefix := startAdmin(t, "pw")
	ctx := context.Background()

	dbType, version := c.serverInfo(ctx)
	assert.Equal(t, "mariadb", dbType)
	assert.False(t, strings.Contains(version, "-"), version)

	// validate_password is not active on stock MariaDB.
	assert.False(t, p.hasPasswordValidation(ctx, c))

	_, err := c.ExecContext(ctx, "CREATE USER 'pwtest'@'%'")
	require.NoError(t, err)

	// Native hash of "secret" (SELECT PASSWORD('secret')).
	user := row{
		"database_user":     "pwtest",
		"database_password": "*14E65567ABDB5135D0CFD9A70B3032C179A49EE7",
	}
	require.True(t, p.setPassword(ctx, c, user, "%"))
	_, err = c.ExecContext(ctx, "FLUSH PRIVILEGES")
	require.NoError(t, err)

	// The plaintext now authenticates over TCP.
	udb, err := sql.Open("mysql", "pwtest:secret@tcp("+dsnAddr(t, dsnPrefix)+")/")
	require.NoError(t, err)
	defer udb.Close()
	require.NoError(t, udb.PingContext(ctx))

	// Denylisted account refused.
	assert.False(t, p.setPassword(ctx, c, row{"database_user": "root", "database_password": "*AAA"}, "%"))
}
