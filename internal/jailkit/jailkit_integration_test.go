//go:build integration

// Package jailkit integration suite (task 4.4): repository write → datalog →
// daemon cycle → shell base plugin (useradd) → jailkit plugin (jk_init /
// jk_jailuser / unlock) against a real docker MariaDB. OS tools stay faked.
package jailkit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/database"
	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/model"
	"go-ispconfig/internal/repository"
	"go-ispconfig/internal/shell"
	"go-ispconfig/internal/web"
)

// sharedFakeRunner is shared by the shell and jailkit plugins so the test
// can assert the full command sequence of one daemon cycle.
type sharedFakeRunner struct {
	mu   sync.Mutex
	runs [][]string
}

func (f *sharedFakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runs = append(f.runs, append([]string{name}, args...))
	return nil, nil
}

func (f *sharedFakeRunner) RunWithStdin(ctx context.Context, _ []byte, name string, args ...string) ([]byte, error) {
	return f.Run(ctx, name, args...)
}

func (f *sharedFakeRunner) all() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.runs))
	for _, r := range f.runs {
		out = append(out, strings.Join(r, " "))
	}
	return out
}

func TestDatalogToJailkitPipeline(t *testing.T) {
	dsnPrefix, name := database.StartMariaDB(t, "jailkit")
	database.MariaDBExec(t, name, "CREATE DATABASE jailkit CHARACTER SET utf8mb4")
	db, err := database.Open(dsnPrefix + "/jailkit?charset=utf8mb4&parseTime=True&loc=Local")
	require.NoError(t, err)
	_, err = database.Migrate(db)
	require.NoError(t, err)
	_, err = database.Seed(db, "server1.test", "test-password-123")
	require.NoError(t, err)

	// Site root outside /tmp (is_allowed_path).
	cwd, err := os.Getwd()
	require.NoError(t, err)
	base, err := os.MkdirTemp(cwd, "jk-e2e-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	base, err = filepath.EvalSymlinks(base)
	require.NoError(t, err)
	docroot := filepath.Join(base, "web1")
	require.NoError(t, os.MkdirAll(docroot, 0o755))

	require.NoError(t, db.Exec(`INSERT INTO web_domain
		(sys_userid, sys_groupid, sys_perm_user, sys_perm_group, server_id, domain,
		 type, document_root, system_user, system_group, php, active, delete_unused_jailkit)
		VALUES (1, 1, 'riud', 'riud', 1, 'jk.example', 'vhost', ?, 'web1', 'client1', 'no', 'y', 'y')`,
		docroot).Error)
	var domainID uint32
	require.NoError(t, db.Raw("SELECT domain_id FROM web_domain WHERE domain = 'jk.example'").
		Scan(&domainID).Error)

	ctx := context.Background()
	admin := &repository.Identity{UserID: 1, Username: "admin", Typ: "admin", Groups: []uint32{1}}
	repo, err := repository.New[model.ShellUser](db)
	require.NoError(t, err)

	runner := &sharedFakeRunner{}
	created := map[string]bool{}
	shellPlugin := shell.NewPlugin(db, runner, nil)
	shellPlugin.LookupUID = func(username string) (int, bool) {
		if username == "web1" || created[username] {
			return 5001, true
		}
		return 0, false
	}
	shellPlugin.LookupGID = func(groupname string) (int, bool) {
		return 5001, groupname == "client1"
	}
	shellPlugin.RootAuthorizedKeys = ""

	jkPlugin := NewPlugin(db, runner, nil)
	jkPlugin.LookupUID = shellPlugin.LookupUID
	jkPlugin.LookupGID = shellPlugin.LookupGID
	jkPlugin.RootAuthorizedKeys = ""

	reg := engine.NewRegistry(nil)
	require.NoError(t, reg.Load(
		[]engine.Module{web.NewModule()},
		[]engine.Plugin{shellPlugin, jkPlugin},
	))
	daemon, err := engine.NewDaemon(db, reg, engine.NewServices(nil, nil), nil, 0)
	require.NoError(t, err)

	rec := &model.ShellUser{
		SysUserID: 1, SysGroupID: 1, SysPermUser: "riud", SysPermGroup: "riud",
		ServerID: 1, ParentDomainID: domainID,
		Username: "web1jk", UsernamePrefix: "web1", Password: "$6$salt$hash",
		Active: "y", PUser: "web1", PGroup: "client1", Shell: "/bin/bash",
		Dir: docroot, Chroot: "jailkit",
	}

	t.Run("insert: base creates the account, jailkit builds the chroot", func(t *testing.T) {
		require.NoError(t, repo.Insert(ctx, admin, rec))
		// The shell base plugin marks the user as existing only after useradd;
		// the jailkit plugin needs LookupUID to see it once useradd has run.
		// We flip the flag after the first cycle would have created it by
		// intercepting: mark created just before RunCycle and rely on the
		// jailkit path running after shell within the same event dispatch —
		// so pre-mark as "will exist once shell runs" is wrong. Instead, make
		// LookupUID return true after any useradd for that name has been
		// recorded by the shared runner.
		shellPlugin.LookupUID = func(username string) (int, bool) {
			if username == "web1" {
				return 5001, true
			}
			for _, c := range runner.all() {
				if strings.HasPrefix(c, "useradd ") && strings.HasSuffix(c, " "+username) {
					return 5001, true
				}
			}
			return 0, false
		}
		jkPlugin.LookupUID = shellPlugin.LookupUID

		require.NoError(t, daemon.RunCycle(ctx))
		created["web1jk"] = true

		cmds := runner.all()
		// Base shell first.
		assert.Contains(t, cmds, "useradd -d "+filepath.Join(docroot, "home", "web1jk")+
			" -g client1 -o -s /bin/false -u 5001 web1jk",
			"jailkit insert parks the account on /bin/false until the jail is ready")
		// Then jailkit.
		var sawInit, sawJailuser, sawUnlock bool
		for _, c := range cmds {
			if strings.HasPrefix(c, "jk_init ") && strings.Contains(c, docroot) {
				sawInit = true
			}
			if strings.Contains(c, "jk_jailuser") && strings.Contains(c, "web1jk") {
				sawJailuser = true
			}
			if c == "usermod -U web1jk" {
				sawUnlock = true
			}
		}
		assert.True(t, sawInit, "jk_init must run: %v", cmds)
		assert.True(t, sawJailuser, "jk_jailuser must run: %v", cmds)
		assert.True(t, sawUnlock, "account unlocked after jail setup: %v", cmds)
		assert.DirExists(t, filepath.Join(docroot, "home", "web1jk"))

		// Hash stamped on the site row.
		var hash string
		require.NoError(t, db.Raw("SELECT last_jailkit_hash FROM web_domain WHERE domain_id = ?", domainID).
			Scan(&hash).Error)
		assert.NotEmpty(t, hash)
	})

	t.Run("delete: removes the OS user and tears down the unused jail", func(t *testing.T) {
		// Seed jail markers so teardown has something to remove.
		require.NoError(t, os.MkdirAll(filepath.Join(docroot, "etc", "jailkit"), 0o755))
		require.NoError(t, os.MkdirAll(filepath.Join(docroot, "bin"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(docroot, "etc", "passwd"),
			[]byte("web1jk:x:5001:5001::/home/web1jk:/bin/bash\n"), 0o644))

		// Base shell plugin skips userdel for jailkit; jailkit owns it.
		require.NoError(t, repo.Delete(ctx, admin, rec.ShellUserID))
		require.NoError(t, daemon.RunCycle(ctx))

		cmds := runner.all()
		assert.Contains(t, cmds, "userdel -f web1jk")
		assert.NoDirExists(t, filepath.Join(docroot, "etc"),
			"delete_unused_jailkit=y removes the jail tree")
		var hash *string
		require.NoError(t, db.Raw("SELECT last_jailkit_hash FROM web_domain WHERE domain_id = ?", domainID).
			Scan(&hash).Error)
		assert.True(t, hash == nil || *hash == "", "hash cleared after unused-jail teardown")
	})
}
