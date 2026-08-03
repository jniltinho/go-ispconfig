package shell

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/system"
)

func update(t *testing.T, p *Plugin, old, u map[string]any) error {
	t.Helper()
	return p.shellUserUpdate(context.Background(), "shell_user_update",
		engine.Data{Old: old, New: u})
}

// existingAccount runs the insert path first, so the update tests start from
// a real home layout, and returns a runner that only holds the update.
func existingAccount(t *testing.T, p *Plugin, runner *fakeRunner, u map[string]any) {
	t.Helper()
	require.NoError(t, insert(t, p, u))
	knownUsers := p.LookupUID
	p.LookupUID = func(name string) (int, bool) {
		if name == u["username"] {
			return 5001, true
		}
		return knownUsers(name)
	}
	runner.mu.Lock()
	runner.runs = nil
	runner.mu.Unlock()
}

func TestUpdateRenamesLoginAndMovesHome(t *testing.T) {
	p, runner, docroot := testPlugin(t)
	old := shellUser(docroot)
	existingAccount(t, p, runner, old)

	oldHome := filepath.Join(docroot, "home", "web1user")
	require.NoError(t, os.WriteFile(filepath.Join(oldHome, "notes.txt"), []byte("keep me"), 0o644))

	u := shellUser(docroot)
	u["username"] = "web1dev"
	require.NoError(t, update(t, p, old, u))

	newHome := filepath.Join(docroot, "home", "web1dev")
	assert.NoDirExists(t, oldHome, "the home moved rather than being recreated")
	content, err := os.ReadFile(filepath.Join(newHome, "notes.txt"))
	require.NoError(t, err)
	assert.Equal(t, "keep me", string(content), "the user's files travel with the home")

	assert.Contains(t, runner.all(),
		"usermod -d "+newHome+" -g client1 -s /bin/bash -l web1dev web1user",
		"the login is renamed in the same usermod that records the new home")
	assert.Equal(t, "web1dev:$6$salt$hash\n", runner.stdin["chpasswd"],
		"the hash is set for the new login name")
}

func TestUpdateKeepsLoginWhenUsernameIsUnchanged(t *testing.T) {
	p, runner, docroot := testPlugin(t)
	old := shellUser(docroot)
	existingAccount(t, p, runner, old)

	u := shellUser(docroot)
	u["shell"] = "/bin/sh"
	require.NoError(t, update(t, p, old, u))

	assert.Contains(t, runner.all(),
		"usermod -d "+filepath.Join(docroot, "home", "web1user")+" -g client1 -s /bin/sh web1user")
	for _, cmd := range runner.all() {
		assert.NotContains(t, cmd, " -l ", "no rename without a new username")
	}
}

func TestUpdateInactiveUserLosesItsShell(t *testing.T) {
	p, runner, docroot := testPlugin(t)
	old := shellUser(docroot)
	existingAccount(t, p, runner, old)

	u := shellUser(docroot)
	u["active"] = "n"
	require.NoError(t, update(t, p, old, u))

	assert.Contains(t, runner.all(),
		"usermod -d "+filepath.Join(docroot, "home", "web1user")+" -g client1 -s /bin/false web1user")
}

func TestUpdateJailkitUserKeepsItsShell(t *testing.T) {
	p, runner, docroot := testPlugin(t)
	old := shellUser(docroot)
	old["chroot"] = "jailkit"
	existingAccount(t, p, runner, old)

	u := shellUser(docroot)
	u["chroot"] = "jailkit"
	u["shell"] = "/usr/sbin/jk_chrootsh"
	require.NoError(t, update(t, p, old, u))

	assert.Contains(t, runner.all(),
		"usermod -d "+docroot+" -g client1 -s /usr/sbin/jk_chrootsh web1user",
		"by update time the jailkit plugin owns the shell, and a jailed home is the login dir itself")
}

func TestUpdateParksAnExistingDirAtTheTarget(t *testing.T) {
	p, runner, docroot := testPlugin(t)
	old := shellUser(docroot)
	existingAccount(t, p, runner, old)

	// Something already occupies the new home: a leftover from an account
	// that used the same name before.
	newHome := filepath.Join(docroot, "home", "web1dev")
	require.NoError(t, os.MkdirAll(newHome, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(newHome, "leftover"), nil, 0o644))

	u := shellUser(docroot)
	u["username"] = "web1dev"
	require.NoError(t, update(t, p, old, u))

	assert.FileExists(t, filepath.Join(newHome+"_bak", "leftover"),
		"the previous occupant is parked, never overwritten")
	assert.DirExists(t, newHome)
}

func TestUpdateRecreatesAMissingHome(t *testing.T) {
	p, runner, docroot := testPlugin(t)
	u := shellUser(docroot)
	existingAccount(t, p, runner, u)

	homedir := filepath.Join(docroot, "home", "web1user")
	require.NoError(t, os.RemoveAll(homedir))

	require.NoError(t, update(t, p, u, u))

	assert.DirExists(t, homedir)
	assert.Contains(t, runner.all(), "chown web1:client1 "+homedir)
	assert.FileExists(t, filepath.Join(homedir, ".profile"))
}

func TestUpdateKeepsTheBashHistory(t *testing.T) {
	p, runner, docroot := testPlugin(t)
	u := shellUser(docroot)
	existingAccount(t, p, runner, u)

	history := filepath.Join(docroot, "home", "web1user", ".bash_history")
	require.NoError(t, os.WriteFile(history, []byte("ls -la\n"), 0o750))

	require.NoError(t, update(t, p, u, u))

	content, err := os.ReadFile(history)
	require.NoError(t, err)
	assert.Equal(t, "ls -la\n", string(content), "an update must not wipe the user's history")
}

func TestUpdateOfAMissingAccountFallsThroughToInsert(t *testing.T) {
	p, runner, docroot := testPlugin(t)
	u := shellUser(docroot)

	// LookupUID knows web1 but not web1user: the account was never created,
	// for instance because the plugin was disabled at insert time.
	require.NoError(t, update(t, p, u, u))

	assert.Contains(t, runner.all(),
		"useradd -d "+filepath.Join(docroot, "home", "web1user")+" -g client1 -o -s /bin/bash -u 5001 web1user")
	for _, cmd := range runner.all() {
		assert.NotContains(t, cmd, "usermod -d", "nothing to modify, the account is created instead")
	}
}

func TestUpdateDisabledBySecuritySetting(t *testing.T) {
	p, runner, docroot := testPlugin(t)
	u := shellUser(docroot)
	existingAccount(t, p, runner, u)
	p.AllowShellUser = func() (bool, error) { return false, nil }

	require.NoError(t, update(t, p, u, u))

	assert.Empty(t, runner.all(), "the kill-switch stops the plugin before any command")
}

func TestUpdateRefusesADirOutsideTheDocroot(t *testing.T) {
	p, runner, docroot := testPlugin(t)
	old := shellUser(docroot)
	existingAccount(t, p, runner, old)

	u := shellUser(filepath.Dir(docroot))
	require.NoError(t, update(t, p, old, u))

	assert.Empty(t, runner.all(), "a payload that fails a guard runs no command")
}

func TestUpdateRelocksDocrootWhenUsermodFails(t *testing.T) {
	p, runner, docroot := testPlugin(t)
	u := shellUser(docroot)
	existingAccount(t, p, runner, u)
	runner.fail = "usermod"

	err := update(t, p, u, u)

	require.ErrorContains(t, err, "usermod web1user")
	cmds := runner.all()
	assert.Equal(t, "chattr +i "+docroot, cmds[len(cmds)-1])
}

func TestHomeOf(t *testing.T) {
	assert.Equal(t, "/var/www/web1/home/web1user",
		homeOf(system.Row{"dir": "/var/www/web1", "username": "web1user", "chroot": ""}))
	assert.Equal(t, "/var/www/web1",
		homeOf(system.Row{"dir": "/var/www/web1", "username": "web1user", "chroot": "jailkit"}),
		"a jailed account is chrooted to its login directory")
}
