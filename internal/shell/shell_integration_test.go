//go:build integration

// Package shell integration suite (task 3.5): the panel-to-system pipeline
// of a shell account against a real docker MariaDB — repository write →
// sys_datalog row → daemon cycle → web module table hook → shell plugin →
// the exact useradd/usermod/userdel argv, plus the home layout on a temp
// document root. The OS seam (useradd, chown, chattr, /etc/passwd lookups)
// stays faked: integration here means database and datalog plumbing, not
// the operating system.
package shell

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

func TestDatalogToShellPipeline(t *testing.T) {
	dsnPrefix, name := database.StartMariaDB(t, "shell")
	database.MariaDBExec(t, name, "CREATE DATABASE shell CHARACTER SET utf8mb4")
	db, err := database.Open(dsnPrefix + "/shell?charset=utf8mb4&parseTime=True&loc=Local")
	require.NoError(t, err)
	_, err = database.Migrate(db)
	require.NoError(t, err)
	_, err = database.Seed(db, "server1.test", "test-password-123")
	require.NoError(t, err)

	docroot := siteRoot(t)
	require.NoError(t, db.Exec(`INSERT INTO web_domain
		(sys_userid, sys_groupid, sys_perm_user, sys_perm_group, server_id, domain,
		 type, document_root, system_user, system_group, php, active)
		VALUES (1, 1, 'riud', 'riud', 1, 'e2e.example', 'vhost', ?, 'web1', 'client1', 'php-fpm', 'y')`,
		docroot).Error)
	var domainID uint32
	require.NoError(t, db.Raw("SELECT domain_id FROM web_domain WHERE domain = 'e2e.example'").
		Scan(&domainID).Error)

	ctx := context.Background()
	admin := &repository.Identity{UserID: 1, Username: "admin", Typ: "admin", Groups: []uint32{1}}
	repo, err := repository.New[model.ShellUser](db)
	require.NoError(t, err)

	runner := &fakeRunner{}
	plugin := NewPlugin(db, runner, nil)
	// The host has no web1 account, and the shell user only appears once the
	// (faked) useradd has run.
	created := map[string]bool{}
	plugin.LookupUID = func(username string) (int, bool) {
		if username == "web1" || created[username] {
			return 5001, true
		}
		return 0, false
	}
	plugin.LookupGID = func(groupname string) (int, bool) {
		return 5001, groupname == "client1"
	}
	plugin.RootAuthorizedKeys = ""

	reg := engine.NewRegistry(nil)
	require.NoError(t, reg.Load([]engine.Module{web.NewModule()}, []engine.Plugin{plugin}))
	daemon, err := engine.NewDaemon(db, reg, engine.NewServices(nil, nil), nil, 0)
	require.NoError(t, err)

	rec := &model.ShellUser{
		SysUserID: 1, SysGroupID: 1, SysPermUser: "riud", SysPermGroup: "riud",
		ServerID: 1, ParentDomainID: domainID,
		Username: "web1user", UsernamePrefix: "web1", Password: "$6$salt$hash",
		Active: "y", PUser: "web1", PGroup: "client1", Shell: "/bin/bash",
		Dir: docroot, Chroot: "", SSHRsa: keyA,
	}
	homedir := filepath.Join(docroot, "home", "web1user")

	t.Run("insert creates the account and the home layout", func(t *testing.T) {
		require.NoError(t, repo.Insert(ctx, admin, rec))
		require.NoError(t, daemon.RunCycle(ctx))
		created["web1user"] = true

		assert.Contains(t, runner.all(),
			"useradd -d "+homedir+" -g client1 -o -s /bin/bash -u 5001 web1user")
		assert.Equal(t, "web1user:$6$salt$hash\n", runner.stdin["chpasswd"])
		assert.DirExists(t, homedir)
		assert.FileExists(t, filepath.Join(homedir, ".profile"))
		assert.Equal(t, []string{keyA}, authorizedKeys(t, homedir),
			"the ssh_rsa key of the row reaches authorized_keys through the datalog")
	})

	t.Run("update renames the login and moves the home", func(t *testing.T) {
		require.NoError(t, repo.Get(ctx, admin, rec.ShellUserID, rec))
		rec.Username = "web1dev"
		rec.Shell = "/bin/sh"
		require.NoError(t, repo.Update(ctx, admin, rec))
		require.NoError(t, daemon.RunCycle(ctx))
		created["web1dev"] = true

		newHome := filepath.Join(docroot, "home", "web1dev")
		assert.Contains(t, runner.all(),
			"usermod -d "+newHome+" -g client1 -s /bin/sh -l web1dev web1user")
		assert.DirExists(t, newHome)
		assert.NoDirExists(t, homedir)
	})

	t.Run("deactivating the account takes its shell away", func(t *testing.T) {
		require.NoError(t, repo.Get(ctx, admin, rec.ShellUserID, rec))
		rec.Active = "n"
		require.NoError(t, repo.Update(ctx, admin, rec))
		require.NoError(t, daemon.RunCycle(ctx))

		assert.Contains(t, runner.all(),
			"usermod -d "+filepath.Join(docroot, "home", "web1dev")+" -g client1 -s /bin/false web1dev")
	})

	t.Run("delete stops php-fpm, removes the account and cleans owned files", func(t *testing.T) {
		newHome := filepath.Join(docroot, "home", "web1dev")
		// The cleanup only removes what the departing uid owns; the test
		// process is that uid here (chown was faked during insert/update).
		plugin.LookupUID = func(username string) (int, bool) {
			if username == "web1" || created[username] {
				return os.Getuid(), true
			}
			return 0, false
		}

		require.NoError(t, repo.Delete(ctx, admin, rec.ShellUserID))
		require.NoError(t, daemon.RunCycle(ctx))

		cmds := runner.all()
		tail := cmds[len(cmds)-4:]
		assert.Equal(t, []string{
			// Seeded server.ini [web] php_fpm_init_script.
			"systemctl stop php8.3-fpm",
			"killall -u web1dev",
			"userdel -f web1dev",
			"systemctl start php8.3-fpm",
		}, tail, "the pool shares the uid, so it is down for the userdel and back up after")
		// PHP only unlinks known owned dotfiles/dirs; layout leftovers
		// (.bashrc.d, .local, web/log/private symlinks) keep the home.
		assert.NoFileExists(t, filepath.Join(newHome, ".profile"))
		assert.NoDirExists(t, filepath.Join(newHome, ".ssh"))
	})
}
