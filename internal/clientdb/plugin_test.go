package clientdb

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/engine"
)

// TestDenylists covers design D8: protected accounts and system schemas
// are refused case-insensitively; regular client names pass.
func TestDenylists(t *testing.T) {
	for _, name := range []string{"root", "ROOT", "debian-sys-maint", "Mysql.Infoschema"} {
		assert.True(t, deniedUser(name), name)
	}
	for _, name := range []string{"c1_app", "admin", "mysqluser"} {
		assert.False(t, deniedUser(name), name)
	}
	for _, name := range []string{"mysql", "MySQL", "information_schema", "PERFORMANCE_SCHEMA"} {
		assert.True(t, deniedDatabase(name), name)
	}
	for _, name := range []string{"c1_app", "mysql2", "performance"} {
		assert.False(t, deniedDatabase(name), name)
	}
}

// TestQuoting: identifier and string-literal quoting used in the
// account/identifier positions that refuse placeholders.
func TestQuoting(t *testing.T) {
	assert.Equal(t, "`c1_app`", quoteName("c1_app"))
	assert.Equal(t, "`c1``x`", quoteName("c1`x"))
	assert.Equal(t, "'c1_app'", quoteStr("c1_app"))
	assert.Equal(t, `'c1\'x'`, quoteStr("c1'x"))
	assert.Equal(t, `'c1\\x'`, quoteStr(`c1\x`))
}

// TestPluginSubscribesFiveEvents: the plugin handles the five events of
// design D7 and deliberately not database_user_insert.
func TestPluginSubscribesFiveEvents(t *testing.T) {
	p := NewPlugin(nil, nil, "", nil)
	h := p.handlers()
	for _, event := range []string{
		"database_insert", "database_update", "database_delete",
		"database_user_update", "database_user_delete",
	} {
		assert.Contains(t, h, event)
	}
	assert.NotContains(t, h, "database_user_insert", "stale user accounts are useless (D7)")
	assert.Len(t, h, 5)

	// All five subscriptions resolve against the module announcements.
	reg := engine.NewRegistry(nil)
	require.NoError(t, reg.Load([]engine.Module{NewModule()}, []engine.Plugin{p}))
}

// TestNonMySQLTypeSkipped: postgres/mongo rows never open an admin
// connection (PHP parity: type check before connect()).
func TestNonMySQLTypeSkipped(t *testing.T) {
	connects := 0
	p := NewPlugin(nil, nil, "", nil)
	p.OpenAdmin = func(context.Context) (*sql.DB, Config, error) {
		connects++
		return nil, Config{}, errors.New("should not connect")
	}
	ctx := context.Background()
	for _, typ := range []string{"pgsql", "mongo", ""} {
		require.NoError(t, p.dbInsert(ctx, engine.Data{New: map[string]any{"type": typ}}))
		require.NoError(t, p.dbUpdate(ctx, engine.Data{New: map[string]any{"type": typ, "active": "y"}}))
		require.NoError(t, p.dbDelete(ctx, engine.Data{Old: map[string]any{"type": typ}}))
	}
	assert.Zero(t, connects)
}

// TestInactiveToInactiveUpdateSkipped: an n→n update returns before any
// connection (PHP dbUpdate early-out).
func TestInactiveToInactiveUpdateSkipped(t *testing.T) {
	connects := 0
	p := NewPlugin(nil, nil, "", nil)
	p.OpenAdmin = func(context.Context) (*sql.DB, Config, error) {
		connects++
		return nil, Config{}, errors.New("no")
	}
	err := p.dbUpdate(context.Background(), engine.Data{
		Old: map[string]any{"type": "mysql", "active": "n"},
		New: map[string]any{"type": "mysql", "active": "n"},
	})
	require.NoError(t, err)
	assert.Zero(t, connects)
}

// TestConnectFailureAbortsEventQuietly: a failed admin connection logs
// and returns nil — the daemon run never fails on a client-DB outage
// (design D3).
func TestConnectFailureAbortsEventQuietly(t *testing.T) {
	p := NewPlugin(nil, nil, "", nil)
	p.OpenAdmin = func(context.Context) (*sql.DB, Config, error) {
		return nil, Config{}, errors.New("connection refused")
	}
	err := p.dbInsert(context.Background(), engine.Data{New: map[string]any{"type": "mysql"}})
	require.NoError(t, err)
}

// TestUserUpdateNoChangesSkipped: same name + unchanged (or empty)
// password is a no-op before connecting (PHP dbUserUpdate early-out).
func TestUserUpdateNoChangesSkipped(t *testing.T) {
	connects := 0
	p := NewPlugin(nil, nil, "", nil)
	p.OpenAdmin = func(context.Context) (*sql.DB, Config, error) {
		connects++
		return nil, Config{}, errors.New("no")
	}
	ctx := context.Background()
	// identical password
	require.NoError(t, p.dbUserUpdate(ctx, engine.Data{
		Old: map[string]any{"database_user": "c1_app", "database_password": "*AAA"},
		New: map[string]any{"database_user": "c1_app", "database_password": "*AAA"},
	}))
	// empty new password means unchanged
	require.NoError(t, p.dbUserUpdate(ctx, engine.Data{
		Old: map[string]any{"database_user": "c1_app", "database_password": "*AAA"},
		New: map[string]any{"database_user": "c1_app", "database_password": ""},
	}))
	assert.Zero(t, connects)
}

// TestUserDeleteDenylistRefused: dropping a protected account is refused
// before any connection (stricter than the PHP missing-return, per D8).
func TestUserDeleteDenylistRefused(t *testing.T) {
	connects := 0
	p := NewPlugin(nil, nil, "", nil)
	p.OpenAdmin = func(context.Context) (*sql.DB, Config, error) {
		connects++
		return nil, Config{}, errors.New("no")
	}
	err := p.dbUserDelete(context.Background(), engine.Data{
		Old: map[string]any{"database_user": "debian-sys-maint"},
	})
	require.NoError(t, err)
	assert.Zero(t, connects)
}

// TestPluginName identifies the plugin.
func TestPluginName(t *testing.T) {
	assert.Equal(t, "mysql_clientdb", NewPlugin(nil, nil, "", nil).Name())
}
