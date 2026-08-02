//go:build integration

package clientdb

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// showGrants returns the concatenated SHOW GRANTS output for user@host
// ("" when the account does not exist).
func showGrants(t *testing.T, c *adminConn, user, host string) string {
	t.Helper()
	rows, err := c.QueryContext(context.Background(), "SHOW GRANTS FOR "+quoteStr(user)+"@"+quoteStr(host))
	if err != nil {
		return ""
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var g string
		require.NoError(t, rows.Scan(&g))
		out = append(out, g)
	}
	require.NoError(t, rows.Err())
	return strings.Join(out, "\n")
}

// userExists checks mysql.user for user@host.
func userExists(t *testing.T, c *adminConn, user, host string) bool {
	t.Helper()
	var n int
	require.NoError(t, c.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM mysql.user WHERE User = ? AND Host = ?", user, host).Scan(&n))
	return n > 0
}

// TestGrantModesAndRevoke covers task 3.5 / design D5 against MariaDB:
// rw grants ALL, rd downgrades to the quota-restricted set, r grants
// SELECT only; revokeAndDrop removes grants and optionally the account.
func TestGrantModesAndRevoke(t *testing.T) {
	p, c, _ := startAdmin(t, "grant")
	ctx := context.Background()

	require.True(t, p.createDatabase(ctx, c, row{"database_name": "c1_app"}))
	user := row{
		"database_user":     "c1_u",
		"database_password": "*14E65567ABDB5135D0CFD9A70B3032C179A49EE7",
	}
	roUser := row{
		"database_user":     "c1_ro",
		"database_password": "*14E65567ABDB5135D0CFD9A70B3032C179A49EE7",
	}

	// rw: ALL PRIVILEGES, user materialised via CREATE USER IF NOT EXISTS.
	require.True(t, p.grant(ctx, c, "c1_app", user, "localhost", "rw"))
	grants := showGrants(t, c, "c1_u", "localhost")
	assert.Contains(t, grants, "ALL PRIVILEGES ON `c1_app`.*")

	// rd: quota exceeded downgrades to SELECT, DELETE, ALTER, DROP.
	require.True(t, p.grant(ctx, c, "c1_app", user, "localhost", "rd"))
	grants = showGrants(t, c, "c1_u", "localhost")
	assert.NotContains(t, grants, "ALL PRIVILEGES ON `c1_app`.*")
	// MariaDB normalises the privilege order in SHOW GRANTS.
	assert.Contains(t, grants, "SELECT, DELETE, DROP, ALTER ON `c1_app`.*")

	// r: read-only user gets SELECT only.
	require.True(t, p.grant(ctx, c, "c1_app", roUser, "localhost", "r"))
	roGrants := showGrants(t, c, "c1_ro", "localhost")
	assert.Contains(t, roGrants, "SELECT ON `c1_app`.*")
	assert.NotContains(t, roGrants, "ALL PRIVILEGES")

	// unknown mode refused
	assert.False(t, p.grant(ctx, c, "c1_app", user, "localhost", "bogus"))

	// denylist refused
	assert.False(t, p.grant(ctx, c, "c1_app", row{"database_user": "root"}, "localhost", "rw"))

	// revoke without drop: grants gone, account remains.
	require.True(t, p.revokeAndDrop(ctx, c, "c1_app", "c1_u", "localhost", false))
	assert.NotContains(t, showGrants(t, c, "c1_u", "localhost"), "`c1_app`.*")
	assert.True(t, userExists(t, c, "c1_u", "localhost"))

	// revoke with drop: account gone.
	require.True(t, p.revokeAndDrop(ctx, c, "c1_app", "c1_ro", "localhost", true))
	assert.False(t, userExists(t, c, "c1_ro", "localhost"))

	// denylisted user never dropped.
	assert.False(t, p.revokeAndDrop(ctx, c, "c1_app", "root", "localhost", true))
	assert.True(t, userExists(t, c, "root", "localhost"))
}
