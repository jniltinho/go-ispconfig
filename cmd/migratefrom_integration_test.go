//go:build integration

// migrate-from integration suite: full CLI flows against the legacytest
// mock panel and a dockerized MariaDB (tasks 4.2/4.3).
package cmd

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"go-ispconfig/internal/database"
	"go-ispconfig/internal/legacy/importer"
	"go-ispconfig/internal/legacy/legacytest"
	"go-ispconfig/internal/model"
)

// setupMigrateDB starts MariaDB with the schema and a local server row.
func setupMigrateDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsnPrefix, name := database.StartMariaDB(t, "cli")
	database.MariaDBExec(t, name, "CREATE DATABASE cli CHARACTER SET utf8mb4")
	db, err := database.Open(dsnPrefix + "/cli?charset=utf8mb4&parseTime=True&loc=Local")
	require.NoError(t, err)
	created, err := database.Migrate(db)
	require.NoError(t, err)
	require.True(t, created)
	_, err = database.Seed(db, "panel.test", "test-admin-pw")
	require.NoError(t, err)
	return db
}

func TestMigrateFromFullRun(t *testing.T) {
	db := setupMigrateDB(t)
	s := legacytest.New()
	t.Cleanup(s.Close)
	openDB := func() (*gorm.DB, error) { return db, nil }
	base := migrateFromOpts{url: s.URL, user: s.Username, password: s.Password, only: "clients,sites,dns"}

	t.Run("clean dry-run exits zero and writes nothing", func(t *testing.T) {
		opts := base
		opts.dryRun = true
		out, _, err := run(t, opts, openDB)
		require.NoError(t, err)
		require.Contains(t, out, "Dry-run: no conflicts; nothing was written.")
		var n int64
		require.NoError(t, db.Model(&model.SysDatalog{}).Count(&n).Error)
		require.Zero(t, n)
	})

	t.Run("apply run exits zero with report", func(t *testing.T) {
		out, _, err := run(t, base, openDB)
		require.NoError(t, err)
		require.Contains(t, out, "Legacy inventory:")
		require.Contains(t, out, "web_domain")
		require.Contains(t, out, "Password reset REQUIRED")
		require.Contains(t, out, "rsync -a --usermap")
		require.Contains(t, out, "Operational order:")
		require.Contains(t, out, "web_domain: 1201/1201", "progress lines printed")
		// No credentials anywhere in the output.
		require.NotContains(t, out, s.Password)
	})

	t.Run("dry-run with conflicts exits non-zero naming them", func(t *testing.T) {
		// A foreign local owner takes over a legacy domain.
		require.NoError(t, db.Exec(
			"UPDATE web_domain SET sys_groupid = 999, sys_userid = 998 WHERE domain = 'site7.example.com'").Error)
		opts := base
		opts.dryRun = true
		out, _, err := run(t, opts, openDB)
		require.Error(t, err)
		require.Contains(t, err.Error(), "conflict")
		require.Contains(t, out, "site7.example.com")
		require.Contains(t, out, "owned by a different user")
	})

	t.Run("bulk reset tokens printed once, no plaintext passwords", func(t *testing.T) {
		var out strings.Builder
		users := []string{"reseller1", "client2", "client3"}
		require.NoError(t, printResetTokens(context.Background(), db, users, &out))
		text := out.String()
		for _, u := range users {
			require.Contains(t, text, u)
		}
		require.Contains(t, text, "shown once")

		// One 32-hex token per user, stored only as a digest.
		lines := 0
		for _, line := range strings.Split(text, "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && len(fields[1]) == 32 {
				lines++
				var u model.SysUser
				require.NoError(t, db.Where("username = ?", fields[0]).First(&u).Error)
				require.Equal(t, importer.HashResetToken(fields[1]), u.LostPasswordHash)
				require.Equal(t, importer.PlaceholderHash, u.Passwort)
			}
		}
		require.Equal(t, 3, lines)
	})
}
