package api

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"go-ispconfig/internal/repository"
)

// relayTestDB holds the two rows the gate reads: the panel-wide INI and a
// client (group 2) whose limit_relayhost the caller sets per case.
func relayTestDB(t *testing.T, showOption, limitRelayhost string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.Exec(
		`CREATE TABLE sys_ini (sysini_id INTEGER PRIMARY KEY, config TEXT, default_logo TEXT, custom_logo TEXT)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE sys_group (groupid INTEGER PRIMARY KEY, client_id INTEGER)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE client (client_id INTEGER PRIMARY KEY, limit_relayhost TEXT)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO sys_ini (sysini_id, config) VALUES (1, ?)`,
		"[mail]\nshow_per_domain_relay_options="+showOption+"\n").Error)
	require.NoError(t, db.Exec(`INSERT INTO sys_group (groupid, client_id) VALUES (2, 7)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO client (client_id, limit_relayhost) VALUES (7, ?)`, limitRelayhost).Error)
	return db
}

// TestRelayAllowed covers the mail_domain_edit.php gate: global option off
// hides the relay fields from everyone, on shows them to admins and to
// clients that carry limit_relayhost = 'y'.
func TestRelayAllowed(t *testing.T) {
	admin := &repository.Identity{Typ: "admin", DefaultGroup: 1}
	client := &repository.Identity{Typ: "user", DefaultGroup: 2}

	t.Run("option off hides them from the admin too", func(t *testing.T) {
		d := &Deps{DB: relayTestDB(t, "n", "y")}
		assert.False(t, relayAllowed(context.Background(), d, admin))
		assert.False(t, relayAllowed(context.Background(), d, client))
	})

	t.Run("option on shows them to an admin and to an allowed client", func(t *testing.T) {
		d := &Deps{DB: relayTestDB(t, "y", "y")}
		assert.True(t, relayAllowed(context.Background(), d, admin))
		assert.True(t, relayAllowed(context.Background(), d, client))
	})

	t.Run("option on but limit_relayhost n hides them from the client", func(t *testing.T) {
		d := &Deps{DB: relayTestDB(t, "y", "n")}
		assert.False(t, relayAllowed(context.Background(), d, client))
		assert.True(t, relayAllowed(context.Background(), d, admin))
	})

	t.Run("no session never passes", func(t *testing.T) {
		d := &Deps{DB: relayTestDB(t, "y", "y")}
		assert.False(t, relayAllowed(context.Background(), d, nil))
	})
}
