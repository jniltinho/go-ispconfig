//go:build integration

// Package ftp integration suite (task 2.2): the panel-to-disk pipeline of an
// FTP account against a real docker MariaDB — repository write → sys_datalog
// row → daemon cycle → web module table hook → ftp plugin → login directory
// under a temp document root. The OS seam (chown, chattr) stays faked:
// integration here means database and datalog plumbing, not the operating
// system.
package ftp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/database"
	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/model"
	"go-ispconfig/internal/repository"
	"go-ispconfig/internal/web"
)

func TestDatalogToFTPPipeline(t *testing.T) {
	dsnPrefix, name := database.StartMariaDB(t, "ftp")
	database.MariaDBExec(t, name, "CREATE DATABASE ftp CHARACTER SET utf8mb4")
	db, err := database.Open(dsnPrefix + "/ftp?charset=utf8mb4&parseTime=True&loc=Local")
	require.NoError(t, err)
	_, err = database.Migrate(db)
	require.NoError(t, err)
	_, err = database.Seed(db, "server1.test", "test-password-123")
	require.NoError(t, err)

	base := t.TempDir()
	docroot := filepath.Join(base, "clients/client1/web1")
	require.NoError(t, db.Exec(`INSERT INTO web_domain
		(sys_userid, sys_groupid, sys_perm_user, sys_perm_group, server_id, domain,
		 type, document_root, system_user, system_group, active)
		VALUES (1, 1, 'riud', 'riud', 1, 'e2e.example', 'vhost', ?, 'web1', 'client1', 'y')`,
		docroot).Error)
	var domainID uint32
	require.NoError(t, db.Raw("SELECT domain_id FROM web_domain WHERE domain = 'e2e.example'").
		Scan(&domainID).Error)

	ctx := context.Background()
	admin := &repository.Identity{UserID: 1, Username: "admin", Typ: "admin", Groups: []uint32{1}}
	repo, err := repository.New[model.FTPUser](db)
	require.NoError(t, err)

	runner := &fakeRunner{}
	reg := engine.NewRegistry(nil)
	require.NoError(t, reg.Load([]engine.Module{web.NewModule()},
		[]engine.Plugin{NewPlugin(db, runner, nil)}))
	daemon, err := engine.NewDaemon(db, reg, engine.NewServices(nil, nil), nil, 0)
	require.NoError(t, err)

	rec := &model.FTPUser{
		SysUserID: 1, SysGroupID: 1, SysPermUser: "riud", SysPermGroup: "riud",
		ServerID: 1, ParentDomainID: domainID,
		Username: "web1ftp", UsernamePrefix: "web1", Password: "$1$fake",
		UID: "web1", GID: "client1", Dir: filepath.Join(docroot, "files"),
		Active: "y", UserType: "user",
	}
	uploads := filepath.Join(docroot, "files")

	t.Run("insert creates the login directory under the docroot", func(t *testing.T) {
		require.NoError(t, repo.Insert(ctx, admin, rec))
		require.NoError(t, daemon.RunCycle(ctx))

		assert.DirExists(t, uploads)
		assert.Contains(t, runner.all(), "chown web1:client1 "+uploads,
			"new components are owned by the site system user")
	})

	t.Run("update to a new dir creates it and drops the old .ftpquota", func(t *testing.T) {
		quota := filepath.Join(uploads, quotaFile)
		require.NoError(t, os.WriteFile(quota, []byte("1 2"), 0o644))

		require.NoError(t, repo.Get(ctx, admin, rec.FTPUserID, rec))
		moved := filepath.Join(docroot, "incoming")
		rec.Dir = moved
		require.NoError(t, repo.Update(ctx, admin, rec))
		require.NoError(t, daemon.RunCycle(ctx))

		assert.DirExists(t, moved)
		assert.NoFileExists(t, quota, "quota state of the previous location is stale")
		assert.DirExists(t, uploads, "the previous directory itself is never removed")
	})

	t.Run("delete removes only the .ftpquota file", func(t *testing.T) {
		moved := filepath.Join(docroot, "incoming")
		quota := filepath.Join(moved, quotaFile)
		require.NoError(t, os.WriteFile(quota, []byte("1 2"), 0o644))

		require.NoError(t, repo.Delete(ctx, admin, rec.FTPUserID))
		require.NoError(t, daemon.RunCycle(ctx))

		assert.NoFileExists(t, quota)
		assert.DirExists(t, moved, "the account's files survive the account")
		assert.NotContains(t, runner.all(), "userdel web1ftp", "FTP accounts are virtual")
	})
}
