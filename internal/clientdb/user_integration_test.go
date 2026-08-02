//go:build integration

package clientdb

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/database"
	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/model"
)

// TestDatabaseUserEvents covers task 3.8 against MariaDB: rename across
// hosts, password change (authenticates with the new plaintext) and the
// Create_user_priv='N' guarded drop.
func TestDatabaseUserEvents(t *testing.T) {
	dsnPrefix, container := database.StartMariaDB(t, "usr")
	database.MariaDBExec(t, container, "CREATE DATABASE panel CHARACTER SET utf8mb4")
	panel, err := database.Open(dsnPrefix + "/panel?charset=utf8mb4&parseTime=True&loc=Local")
	require.NoError(t, err)
	_, err = database.Migrate(panel)
	require.NoError(t, err)

	p := NewPlugin(panel, engine.ExecRunner{}, "", 1, nil)
	host, port := dsnHostPort(t, dsnPrefix)
	cfg := Config{Host: host, Port: port, User: "root", Password: "root"}
	p.OpenAdmin = func(context.Context) (*sql.DB, Config, error) {
		db, err := sql.Open("mysql", dsnPrefix+"/")
		return db, cfg, err
	}
	c, err := p.connect(context.Background())
	require.NoError(t, err)
	defer func() { _ = c.Close() }()
	ctx := context.Background()

	// Panel user + one database referencing it (remote 10.0.0.9).
	u1 := model.WebDatabaseUser{SysUserID: 1, SysGroupID: 1, ServerID: 1,
		DatabaseUser: "c2_u", DatabasePassword: "*14E65567ABDB5135D0CFD9A70B3032C179A49EE7"}
	require.NoError(t, panel.Create(&u1).Error)
	db1 := model.WebDatabase{SysUserID: 1, SysGroupID: 1, ServerID: 1, Type: "mysql",
		DatabaseName: "c2_app", DatabaseUserID: &u1.DatabaseUserID,
		RemoteAccess: "y", RemoteIps: "10.0.0.9", Active: "y"}
	require.NoError(t, panel.Create(&db1).Error)
	// Second database with wildcard remote access: the user update must
	// union the host lists of every database referencing the user.
	db2 := model.WebDatabase{SysUserID: 1, SysGroupID: 1, ServerID: 1, Type: "mysql",
		DatabaseName: "c2_wild", DatabaseUserID: &u1.DatabaseUserID,
		RemoteAccess: "y", RemoteIps: "", Active: "y"}
	require.NoError(t, panel.Create(&db2).Error)

	// Materialise the user on all three hosts.
	require.True(t, p.createDatabase(ctx, c, row{"database_name": "c2_app"}))
	userRow := row{"database_user": "c2_u",
		"database_password": "*14E65567ABDB5135D0CFD9A70B3032C179A49EE7"}
	require.True(t, p.grant(ctx, c, "c2_app", userRow, "localhost", "rw"))
	require.True(t, p.grant(ctx, c, "c2_app", userRow, "10.0.0.9", "rw"))
	require.True(t, p.grant(ctx, c, "c2_app", userRow, "%", "rw"))
	p.flushPrivileges(ctx, c)

	uid := fmt.Sprint(u1.DatabaseUserID)

	// Rename across both hosts, grants preserved by RENAME USER.
	require.NoError(t, p.dbUserUpdate(ctx, engine.Data{
		Old: map[string]any{"database_user_id": uid, "database_user": "c2_u",
			"database_password": "*14E65567ABDB5135D0CFD9A70B3032C179A49EE7"},
		New: map[string]any{"database_user_id": uid, "database_user": "c2_renamed",
			"database_password": "*14E65567ABDB5135D0CFD9A70B3032C179A49EE7"},
	}))
	assert.False(t, userExists(t, c, "c2_u", "localhost"))
	assert.True(t, userExists(t, c, "c2_renamed", "localhost"))
	assert.True(t, userExists(t, c, "c2_renamed", "10.0.0.9"))
	assert.True(t, userExists(t, c, "c2_renamed", "%"))
	assert.Contains(t, showGrants(t, c, "c2_renamed", "localhost"), "ALL PRIVILEGES ON `c2_app`.*")

	// Password change: hash of "newpass" (SELECT PASSWORD('newpass')).
	require.NoError(t, p.dbUserUpdate(ctx, engine.Data{
		Old: map[string]any{"database_user_id": uid, "database_user": "c2_renamed",
			"database_password": "*14E65567ABDB5135D0CFD9A70B3032C179A49EE7"},
		New: map[string]any{"database_user_id": uid, "database_user": "c2_renamed",
			"database_password": "*D8DECEC305209EEFEC43008E1D420E1AA06B19E0"},
	}))
	udb, err := sql.Open("mysql", "c2_renamed:newpass@tcp("+dsnAddr(t, dsnPrefix)+")/")
	require.NoError(t, err)
	defer func() { _ = udb.Close() }()
	require.NoError(t, udb.PingContext(ctx))

	// Delete: both hosts dropped via the mysql.user scan; root untouched.
	require.NoError(t, p.dbUserDelete(ctx, engine.Data{
		Old: map[string]any{"database_user_id": uid, "database_user": "c2_renamed"},
	}))
	assert.False(t, userExists(t, c, "c2_renamed", "localhost"))
	assert.False(t, userExists(t, c, "c2_renamed", "10.0.0.9"))
	assert.False(t, userExists(t, c, "c2_renamed", "%"))
	assert.True(t, userExists(t, c, "root", "localhost"))

	// Unknown panel user: no databases reference it — handler no-ops.
	require.NoError(t, p.dbUserUpdate(ctx, engine.Data{
		Old: map[string]any{"database_user_id": "999999", "database_user": "ghost",
			"database_password": "*AAA"},
		New: map[string]any{"database_user_id": "999999", "database_user": "ghost2",
			"database_password": "*BBB"},
	}))
}
