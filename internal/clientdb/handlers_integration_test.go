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

// TestDatabaseEventPipeline covers task 3.6 with datalog-shaped payloads
// (string values, PHP-era) against MariaDB: insert grants rw+ro users,
// update reconciles hosts and the inactive path honours the
// other-database guard, delete drops the schema.
func TestDatabaseEventPipeline(t *testing.T) {
	dsnPrefix, container := database.StartMariaDB(t, "pipe")
	database.MariaDBExec(t, container, "CREATE DATABASE panel CHARACTER SET utf8mb4")
	panel, err := database.Open(dsnPrefix + "/panel?charset=utf8mb4&parseTime=True&loc=Local")
	require.NoError(t, err)
	_, err = database.Migrate(panel)
	require.NoError(t, err)

	p := NewPlugin(panel, engine.ExecRunner{}, "", 1, nil)
	p.OpenAdmin = func(context.Context) (*sql.DB, Config, error) {
		db, err := sql.Open("mysql", dsnPrefix+"/")
		return db, Config{Host: "127.0.0.1", User: "root", Password: "root"}, err
	}
	c, err := p.connect(context.Background())
	require.NoError(t, err)
	defer c.Close()
	ctx := context.Background()

	// Panel rows: rw user, ro user, the database and one other active
	// database that keeps the rw user alive on localhost.
	u1 := model.WebDatabaseUser{SysUserID: 1, SysGroupID: 1, ServerID: 1,
		DatabaseUser: "c1_u", DatabasePassword: "*14E65567ABDB5135D0CFD9A70B3032C179A49EE7"}
	u2 := model.WebDatabaseUser{SysUserID: 1, SysGroupID: 1, ServerID: 1,
		DatabaseUser: "c1_ro", DatabasePassword: "*14E65567ABDB5135D0CFD9A70B3032C179A49EE7"}
	require.NoError(t, panel.Create(&u1).Error)
	require.NoError(t, panel.Create(&u2).Error)

	db1 := model.WebDatabase{SysUserID: 1, SysGroupID: 1, ServerID: 1, Type: "mysql",
		DatabaseName: "c1_app", DatabaseUserID: &u1.DatabaseUserID,
		DatabaseROUserID: &u2.DatabaseUserID, RemoteAccess: "n", Active: "y"}
	db2 := model.WebDatabase{SysUserID: 1, SysGroupID: 1, ServerID: 1, Type: "mysql",
		DatabaseName: "c1_other", DatabaseUserID: &u1.DatabaseUserID,
		RemoteAccess: "n", Active: "y"}
	require.NoError(t, panel.Create(&db1).Error)
	require.NoError(t, panel.Create(&db2).Error)

	id := func(v uint32) string { return fmt.Sprint(v) }
	base := map[string]any{
		"database_id":         id(db1.DatabaseID),
		"server_id":           "1",
		"type":                "mysql",
		"database_name":       "c1_app",
		"database_user_id":    id(u1.DatabaseUserID),
		"database_ro_user_id": id(u2.DatabaseUserID),
		"database_charset":    "",
		"remote_access":       "n",
		"remote_ips":          "",
		"quota_exceeded":      "n",
		"active":              "y",
	}
	clone := func(over map[string]any) map[string]any {
		m := map[string]any{}
		for k, v := range base {
			m[k] = v
		}
		for k, v := range over {
			m[k] = v
		}
		return m
	}

	// Insert: schema + rw/ro grants on localhost.
	require.NoError(t, p.dbInsert(ctx, engine.Data{New: clone(nil)}))
	assert.True(t, c.schemaExists(ctx, "c1_app"))
	assert.Contains(t, showGrants(t, c, "c1_u", "localhost"), "ALL PRIVILEGES ON `c1_app`.*")
	assert.Contains(t, showGrants(t, c, "c1_ro", "localhost"), "SELECT ON `c1_app`.*")

	// Update: enable remote access for one IP — grants appear for it.
	withRemote := clone(map[string]any{"remote_access": "y", "remote_ips": "10.0.0.5"})
	require.NoError(t, p.dbUpdate(ctx, engine.Data{Old: clone(nil), New: withRemote}))
	assert.Contains(t, showGrants(t, c, "c1_u", "10.0.0.5"), "ALL PRIVILEGES ON `c1_app`.*")
	assert.Contains(t, showGrants(t, c, "c1_ro", "10.0.0.5"), "SELECT ON `c1_app`.*")

	// Vanished database is recreated on update (missing-DB recreate path).
	_, err = c.ExecContext(ctx, "DROP DATABASE c1_app")
	require.NoError(t, err)
	require.NoError(t, p.dbUpdate(ctx, engine.Data{Old: withRemote, New: withRemote}))
	assert.True(t, c.schemaExists(ctx, "c1_app"))

	// Deactivate: revokes everything; c1_u@localhost survives via the
	// other active database (getOtherHostList guard), the rest drops.
	inactive := clone(map[string]any{"remote_access": "y", "remote_ips": "10.0.0.5", "active": "n"})
	require.NoError(t, p.dbUpdate(ctx, engine.Data{Old: withRemote, New: inactive}))
	assert.True(t, userExists(t, c, "c1_u", "localhost"))
	assert.False(t, userExists(t, c, "c1_u", "10.0.0.5"))
	assert.False(t, userExists(t, c, "c1_ro", "localhost"))
	assert.False(t, userExists(t, c, "c1_ro", "10.0.0.5"))
	assert.NotContains(t, showGrants(t, c, "c1_u", "localhost"), "`c1_app`.*")

	// Inactive → inactive is a no-op; delete drops the schema.
	require.NoError(t, p.dbDelete(ctx, engine.Data{Old: inactive}))
	assert.False(t, c.schemaExists(ctx, "c1_app"))
}
