//go:build integration

package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/ftp"
	"go-ispconfig/internal/jailkit"
	"go-ispconfig/internal/model"
	"go-ispconfig/internal/shell"
	"go-ispconfig/internal/web"
)

// sharedFakeRunner records every argv (and stdin for chpasswd) so the
// end-to-end suite can assert shell/jailkit sequencing across plugins.
type sharedFakeRunner struct {
	mu    sync.Mutex
	runs  [][]string
	stdin map[string]string
}

func (f *sharedFakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runs = append(f.runs, append([]string{name}, args...))
	return nil, nil
}

func (f *sharedFakeRunner) RunWithStdin(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
	f.mu.Lock()
	if f.stdin == nil {
		f.stdin = map[string]string{}
	}
	f.stdin[name] = string(stdin)
	f.mu.Unlock()
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

// siteRootOutsideTmp builds a document root the shell/jailkit path guards
// accept (system.IsAllowedPath refuses /tmp, where t.TempDir lives).
func siteRootOutsideTmp(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	require.NoError(t, err)
	base, err := os.MkdirTemp(cwd, "ftp-shell-flow-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	base, err = filepath.EvalSymlinks(base)
	require.NoError(t, err)
	docroot := filepath.Join(base, "web1")
	require.NoError(t, os.MkdirAll(docroot, 0o755))
	return docroot
}

// TestFTPShellEndToEndFlow (task 7.1): API FTP/shell create → sys_datalog →
// daemon cycle → FTP directory on disk and expected shell/jailkit runner
// argv. OS tools stay faked; integration covers the panel-to-daemon pipeline.
func TestFTPShellEndToEndFlow(t *testing.T) {
	env := newSitesTestEnv(t, "ftpshellflow")
	db, srv := env.db, env.srv
	ctx := context.Background()
	docroot := siteRootOutsideTmp(t)

	runner := &sharedFakeRunner{}
	ftpPlugin := ftp.NewPlugin(db, runner, nil)
	shellPlugin := shell.NewPlugin(db, runner, nil)
	jkPlugin := jailkit.NewPlugin(db, runner, nil)

	// Parent site system_user is not a real host account; inject a safe UID.
	// After useradd is recorded, the shell username also resolves so jailkit
	// can finish setup in the same daemon cycle.
	lookupUID := func(username string) (int, bool) {
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
	lookupGID := func(groupname string) (int, bool) {
		return 5001, groupname == "client0" || groupname == "client1"
	}
	shellPlugin.LookupUID = lookupUID
	shellPlugin.LookupGID = lookupGID
	shellPlugin.RootAuthorizedKeys = ""
	jkPlugin.LookupUID = lookupUID
	jkPlugin.LookupGID = lookupGID
	jkPlugin.RootAuthorizedKeys = ""

	reg := engine.NewRegistry(nil)
	require.NoError(t, reg.Load(
		[]engine.Module{web.NewModule()},
		[]engine.Plugin{ftpPlugin, shellPlugin, jkPlugin},
	))
	daemon, err := engine.NewDaemon(db, reg, engine.NewServices(nil, nil), nil)
	require.NoError(t, err)

	// Drain seed backlog so later cycles only see this test's rows.
	require.NoError(t, daemon.RunCycle(ctx))

	// Parent website via the real API, then relocate document_root to a path
	// the daemon plugins may write under (and that IsAllowedPath accepts).
	domainID := env.createDomain(t, env.adminCookie, env.adminCSRF,
		map[string]any{"server_id": 1, "domain": "ftpshell-flow.example.com", "type": "vhost"})
	require.NoError(t, db.Model(&model.WebDomain{}).Where("domain_id = ?", int(domainID)).
		Updates(map[string]any{
			"document_root":         docroot,
			"system_user":           "web1",
			"system_group":          "client0",
			"php":                   "no",
			"delete_unused_jailkit": "y",
		}).Error)
	// Consume the domain insert datalog (no nginx plugin loaded → no-op).
	require.NoError(t, daemon.RunCycle(ctx))

	t.Run("API FTP create → datalog → daemon → directory exists", func(t *testing.T) {
		loginDir := filepath.Join(docroot, "files")
		status, data := call(t, srv, http.MethodPost, "/api/sites/ftp-users",
			env.adminCookie, env.adminCSRF, map[string]any{
				"parent_domain_id": domainID,
				"username":         "flowftp",
				"password":         "Sup3r-Secret!",
				"dir":              loginDir,
			})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		ftpID := int(rec["ftp_user_id"].(float64))
		require.Equal(t, loginDir, rec["dir"])
		require.NotContains(t, rec, "password")

		var dl model.SysDatalog
		require.NoError(t, db.Where("dbtable = ? AND action = 'i' AND dbidx = ?",
			"ftp_user", fmt.Sprintf("ftp_user_id:%d", ftpID)).
			Order("datalog_id DESC").First(&dl).Error)

		require.NoError(t, daemon.RunCycle(ctx))
		assert.DirExists(t, loginDir)
		assert.Contains(t, runner.all(), "chown web1:client0 "+loginDir,
			"new FTP dir components are owned by the site system user")

		// Cleanup so the site is free for shell tests (no need for daemon
		// side-effects on delete beyond .ftpquota).
		status, _ = call(t, srv, http.MethodDelete,
			fmt.Sprintf("/api/sites/ftp-users/%d", ftpID), env.adminCookie, env.adminCSRF, nil)
		require.Equal(t, http.StatusNoContent, status)
		require.NoError(t, daemon.RunCycle(ctx))
	})

	t.Run("API shell create (non-jailkit) → useradd/chpasswd/ssh", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/sites/shell-users",
			env.adminCookie, env.adminCSRF, map[string]any{
				"parent_domain_id": domainID,
				"username":         "flowshell",
				"password":         "Sup3r-Secret!",
				"chroot":           "",
				"ssh_rsa":          "ssh-ed25519 AAAA flowshell@test",
			})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		shellID := int(rec["shell_user_id"].(float64))
		require.Equal(t, docroot, rec["dir"])
		require.NotContains(t, rec, "password")

		before := len(runner.all())
		require.NoError(t, daemon.RunCycle(ctx))
		cmds := runner.all()[before:]

		homedir := filepath.Join(docroot, "home", "flowshell")
		assert.Contains(t, cmds,
			"useradd -d "+homedir+" -g client0 -o -s /bin/bash -u 5001 flowshell")
		assert.Contains(t, runner.stdin["chpasswd"], "flowshell:")
		assert.DirExists(t, homedir)
		assert.FileExists(t, filepath.Join(homedir, ".ssh", "authorized_keys"))
		keys, err := os.ReadFile(filepath.Join(homedir, ".ssh", "authorized_keys"))
		require.NoError(t, err)
		assert.Contains(t, string(keys), "ssh-ed25519 AAAA flowshell@test")

		// Delete and drain so the jailkit subtest starts clean.
		status, _ = call(t, srv, http.MethodDelete,
			fmt.Sprintf("/api/sites/shell-users/%d", shellID), env.adminCookie, env.adminCSRF, nil)
		require.Equal(t, http.StatusNoContent, status)
		// UID lookup for cleanup: pretend the shell user is this process so
		// owned-dotfile cleanup can run without root.
		shellPlugin.LookupUID = func(username string) (int, bool) {
			if username == "flowshell" {
				return os.Getuid(), true
			}
			return lookupUID(username)
		}
		require.NoError(t, daemon.RunCycle(ctx))
		shellPlugin.LookupUID = lookupUID
	})

	t.Run("API shell create chroot=jailkit → jailkit after base", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/sites/shell-users",
			env.adminCookie, env.adminCSRF, map[string]any{
				"parent_domain_id": domainID,
				"username":         "flowjk",
				"password":         "Sup3r-Secret!",
				"chroot":           "jailkit",
			})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		require.Equal(t, "jailkit", rec["chroot"])

		before := len(runner.all())
		require.NoError(t, daemon.RunCycle(ctx))
		cmds := runner.all()[before:]

		// Base parks the account on /bin/false until jailkit finishes.
		homedir := filepath.Join(docroot, "home", "flowjk")
		assert.Contains(t, cmds,
			"useradd -d "+homedir+" -g client0 -o -s /bin/false -u 5001 flowjk",
			"base plugin runs first with locked shell")

		var useraddIdx, jkInitIdx, unlockIdx = -1, -1, -1
		for i, c := range cmds {
			if strings.HasPrefix(c, "useradd ") && strings.HasSuffix(c, " flowjk") {
				useraddIdx = i
			}
			if strings.HasPrefix(c, "jk_init ") && strings.Contains(c, docroot) {
				jkInitIdx = i
			}
			if c == "usermod -U flowjk" {
				unlockIdx = i
			}
		}
		require.GreaterOrEqual(t, useraddIdx, 0, "useradd missing: %v", cmds)
		require.GreaterOrEqual(t, jkInitIdx, 0, "jk_init missing: %v", cmds)
		require.GreaterOrEqual(t, unlockIdx, 0, "unlock missing: %v", cmds)
		assert.Less(t, useraddIdx, jkInitIdx, "base useradd before jailkit init")
		assert.Less(t, jkInitIdx, unlockIdx, "jail built before unlock")

		var hash string
		require.NoError(t, db.Raw("SELECT last_jailkit_hash FROM web_domain WHERE domain_id = ?", int(domainID)).
			Scan(&hash).Error)
		assert.NotEmpty(t, hash, "jailkit stamps last_jailkit_hash on the site")
	})
}
