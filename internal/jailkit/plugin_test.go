package jailkit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/getconf"
	"go-ispconfig/internal/system"
)

// fakeRunner records commands without executing anything.
type fakeRunner struct {
	mu   sync.Mutex
	runs [][]string
	fail string
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runs = append(f.runs, append([]string{name}, args...))
	if name == f.fail {
		return []byte(name + ": boom"), errors.New("exit status 1")
	}
	return nil, nil
}

func (f *fakeRunner) all() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.runs))
	for _, r := range f.runs {
		out = append(out, strings.Join(r, " "))
	}
	return out
}

func (f *fakeRunner) contains(substr string) bool {
	for _, c := range f.all() {
		if strings.Contains(c, substr) {
			return true
		}
	}
	return false
}

// siteRoot creates a document root outside /tmp (is_allowed_path).
func siteRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	require.NoError(t, err)
	base, err := os.MkdirTemp(cwd, "jk-site-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	base, err = filepath.EvalSymlinks(base)
	require.NoError(t, err)
	docroot := filepath.Join(base, "web1")
	require.NoError(t, os.MkdirAll(docroot, 0o755))
	return docroot
}

func testPlugin(t *testing.T) (*Plugin, *fakeRunner, string) {
	t.Helper()
	docroot := siteRoot(t)
	runner := &fakeRunner{}
	p := NewPlugin(nil, runner, nil)
	p.RootAuthorizedKeys = ""
	p.LoadWeb = func(int64) (system.Row, error) {
		return system.Row{
			"domain_id": int64(1), "document_root": docroot, "server_id": int64(1),
			"domain": "e2e.example", "system_user": "web1", "system_group": "client1",
			"last_jailkit_hash": "",
		}, nil
	}
	p.LoadJailkitCfg = func(uint32) (getconf.JailkitConfig, error) {
		return getconf.DefaultJailkitConfig(), nil
	}
	p.LoadWebConfig = func(uint32) (*getconf.WebConfig, error) {
		return &getconf.WebConfig{WebFolderProtection: "y", SecurityLevel: "10"}, nil
	}
	p.AllowShellUser = func() (bool, error) { return true, nil }
	p.LookupUID = func(name string) (int, bool) {
		switch name {
		case "web1", "web1user", "web1dev":
			return 5001, true
		}
		return 0, false
	}
	p.LookupGID = func(name string) (int, bool) {
		return 5001, name == "client1"
	}
	p.StampHash = func(string, string) error { return nil }
	p.ClearHash = func(string) error { return nil }
	p.ListWebFolders = func(int64, string, uint32) ([]string, error) { return nil, nil }
	p.JailkitInUse = func(int64) (bool, error) { return false, nil }
	return p, runner, docroot
}

func shellUser(docroot string) map[string]any {
	return map[string]any{
		"username": "web1user", "puser": "web1", "pgroup": "client1",
		"dir": docroot, "chroot": "jailkit", "shell": "/bin/bash",
		"active": "y", "parent_domain_id": int64(1), "ssh_rsa": "",
		"password": "$6$salt$hash",
	}
}

func TestNonJailkitInsertIsNoop(t *testing.T) {
	p, runner, docroot := testPlugin(t)
	u := shellUser(docroot)
	u["chroot"] = ""
	require.NoError(t, p.shellUserInsert(context.Background(), "shell_user_insert",
		engine.Data{New: u}))
	assert.Empty(t, runner.all())
}

func TestPolicyDisablesJailkit(t *testing.T) {
	p, runner, docroot := testPlugin(t)
	p.AllowShellUser = func() (bool, error) { return false, nil }
	require.NoError(t, p.shellUserInsert(context.Background(), "shell_user_insert",
		engine.Data{New: shellUser(docroot)}))
	assert.Empty(t, runner.all())
}

func TestRootParentAborts(t *testing.T) {
	p, runner, docroot := testPlugin(t)
	u := shellUser(docroot)
	u["puser"] = "root"
	require.NoError(t, p.shellUserInsert(context.Background(), "shell_user_insert",
		engine.Data{New: u}))
	assert.Empty(t, runner.all(), "root parent must not touch the jail")
}

func TestInsertBuildsChrootAndJailsUser(t *testing.T) {
	p, runner, docroot := testPlugin(t)
	var stamped string
	p.StampHash = func(dir, hash string) error {
		stamped = hash
		assert.Equal(t, docroot, dir)
		return nil
	}

	require.NoError(t, p.shellUserInsert(context.Background(), "shell_user_insert",
		engine.Data{New: shellUser(docroot)}))

	cmds := runner.all()
	assert.True(t, runner.contains("jk_init"), "first jailkit user builds the chroot: %v", cmds)
	assert.True(t, runner.contains("-j "+docroot), cmds)
	assert.True(t, runner.contains("jk_jailuser -n -s /bin/bash -j "+docroot+" web1user"), cmds)
	assert.True(t, runner.contains("usermod -s "+jkChrootShell+" web1user"),
		"insert unlocks and hands over jk_chrootsh: %v", cmds)
	assert.True(t, runner.contains("usermod -U web1user"), cmds)

	// Jail layout markers.
	assert.DirExists(t, filepath.Join(docroot, "etc"))
	assert.DirExists(t, filepath.Join(docroot, "home", "web1user"))
	assert.FileExists(t, filepath.Join(docroot, "var", "run", "motd"))
	assert.NotEmpty(t, stamped)

	// Fake jk_init does not create etc/jailkit; simulate for the hash-skip test.
	require.NoError(t, os.MkdirAll(jailEtcPath(docroot), 0o755))
}

func TestUnchangedHashSkipsRebuild(t *testing.T) {
	p, runner, docroot := testPlugin(t)
	require.NoError(t, os.MkdirAll(jailEtcPath(docroot), 0o755))

	cfg := MergeConfig(getconf.DefaultJailkitConfig(), nil)
	hash := Hash(cfg)
	p.LoadWeb = func(int64) (system.Row, error) {
		return system.Row{
			"domain_id": int64(1), "document_root": docroot, "server_id": int64(1),
			"domain": "e2e.example", "last_jailkit_hash": hash,
		}, nil
	}
	stamped := false
	p.StampHash = func(string, string) error { stamped = true; return nil }

	require.NoError(t, p.shellUserInsert(context.Background(), "shell_user_insert",
		engine.Data{New: shellUser(docroot)}))

	assert.False(t, runner.contains("jk_init"), "unchanged hash must not rebuild: %v", runner.all())
	assert.False(t, stamped, "hash stamp is skipped with the rebuild")
	assert.True(t, runner.contains("jk_jailuser"), "the user is still added")
}

func TestHashChangeForceUpdates(t *testing.T) {
	p, runner, docroot := testPlugin(t)
	require.NoError(t, os.MkdirAll(jailEtcPath(docroot), 0o755))
	p.LoadWeb = func(int64) (system.Row, error) {
		return system.Row{
			"domain_id": int64(1), "document_root": docroot, "server_id": int64(1),
			"domain": "e2e.example", "last_jailkit_hash": "deadbeef",
			"php_cli_binary": "/usr/bin/php8.3",
		}, nil
	}
	p.ListWebFolders = func(int64, string, uint32) ([]string, error) {
		return []string{"sub"}, nil
	}
	var stamped string
	p.StampHash = func(_, hash string) error { stamped = hash; return nil }

	require.NoError(t, p.shellUserUpdate(context.Background(), "shell_user_update",
		engine.Data{New: shellUser(docroot), Old: shellUser(docroot)}))

	assert.True(t, runner.contains("jk_update --jail="+docroot), runner.all())
	assert.True(t, runner.contains("--skip=/sub"), "web_folder of subdomains is skipped")
	assert.True(t, runner.contains("jk_init"), "force re-init after update")
	assert.NotEmpty(t, stamped)
	assert.NotEqual(t, "deadbeef", stamped)
}

func TestSiteSectionsReachJkInit(t *testing.T) {
	p, runner, docroot := testPlugin(t)
	p.LoadWeb = func(int64) (system.Row, error) {
		return system.Row{
			"domain_id": int64(1), "document_root": docroot, "server_id": int64(1),
			"domain":                      "e2e.example",
			"jailkit_chroot_app_sections": "basicshell git",
			"php_jk_section":              "php8_3",
		}, nil
	}
	require.NoError(t, p.shellUserInsert(context.Background(), "shell_user_insert",
		engine.Data{New: shellUser(docroot)}))

	var initCmd string
	for _, c := range runner.all() {
		if strings.HasPrefix(c, "jk_init ") {
			initCmd = c
			break
		}
	}
	require.NotEmpty(t, initCmd)
	assert.Contains(t, initCmd, "basicshell")
	assert.Contains(t, initCmd, "git")
	assert.Contains(t, initCmd, "php8_3")
	assert.NotContains(t, initCmd, "coreutils",
		"site sections replace the server defaults")
}
