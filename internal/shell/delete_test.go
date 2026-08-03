package shell

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/getconf"
	"go-ispconfig/internal/system"
)

func deleteUser(t *testing.T, p *Plugin, old map[string]any) error {
	t.Helper()
	return p.shellUserDelete(context.Background(), "shell_user_delete", engine.Data{Old: old})
}

// deletablePlugin builds a plugin whose account exists in /etc/passwd with
// the UID of the test process, so the ownership checks of the cleanup see
// the files the test just wrote as owned by the departing user.
func deletablePlugin(t *testing.T) (*Plugin, *fakeRunner, string) {
	t.Helper()
	p, runner, docroot := testPlugin(t)
	require.Greater(t, os.Getuid(), minUID, "the test user needs a non-system uid")
	p.LookupUID = func(name string) (int, bool) {
		if name == "web1user" || name == "web1" {
			return os.Getuid(), true
		}
		return 0, false
	}
	p.DirInUse = func(string) (bool, error) { return false, nil }
	return p, runner, docroot
}

// populatedHome lays out a home as the insert path would have left it.
func populatedHome(t *testing.T, docroot string) string {
	t.Helper()
	homedir := filepath.Join(docroot, "home", "web1user")
	require.NoError(t, os.MkdirAll(filepath.Join(homedir, ".ssh"), 0o700))
	require.NoError(t, os.MkdirAll(filepath.Join(homedir, ".cache"), 0o700))
	for _, name := range []string{".bash_logout", ".bash_history", ".bashrc", ".profile"} {
		require.NoError(t, os.WriteFile(filepath.Join(homedir, name), []byte("x"), 0o644))
	}
	require.NoError(t, os.WriteFile(filepath.Join(homedir, ".ssh", "authorized_keys"), nil, 0o600))
	return homedir
}

func TestDeleteRemovesAccountAndOwnedDotfiles(t *testing.T) {
	p, runner, docroot := deletablePlugin(t)
	homedir := populatedHome(t, docroot)

	require.NoError(t, deleteUser(t, p, shellUser(docroot)))

	for _, name := range append(ownedDotfiles, ownedDotdirs...) {
		assert.NoFileExists(t, filepath.Join(homedir, name), name)
	}
	assert.NoDirExists(t, homedir, "an emptied home is removed")
	assert.Equal(t, []string{
		"chattr -i " + docroot,
		"chattr +i " + docroot,
		"killall -u web1user",
		"userdel -f web1user",
	}, runner.all(), "no php-fpm dance for a site that does not run php-fpm")
}

func TestDeleteKeepsFilesTheUserDoesNotOwn(t *testing.T) {
	p, _, docroot := deletablePlugin(t)
	homedir := populatedHome(t, docroot)
	// The departing user is not the owner of anything in this home.
	p.LookupUID = func(string) (int, bool) { return os.Getuid() + 1, true }

	require.NoError(t, deleteUser(t, p, shellUser(docroot)))

	assert.FileExists(t, filepath.Join(homedir, ".bashrc"),
		"a home shared with the site user keeps the files that are not ours")
	assert.DirExists(t, filepath.Join(homedir, ".ssh"))
	assert.DirExists(t, homedir)
}

func TestDeleteKeepsAHomeThatStillHasContent(t *testing.T) {
	p, _, docroot := deletablePlugin(t)
	homedir := populatedHome(t, docroot)
	require.NoError(t, os.WriteFile(filepath.Join(homedir, "project.tar"), []byte("data"), 0o644))

	require.NoError(t, deleteUser(t, p, shellUser(docroot)))

	assert.DirExists(t, homedir, "the user's own files are never deleted")
	assert.FileExists(t, filepath.Join(homedir, "project.tar"))
	assert.NoFileExists(t, filepath.Join(homedir, ".bashrc"))
}

func TestDeleteSkipsCleanupWhileTheDirIsShared(t *testing.T) {
	p, runner, docroot := deletablePlugin(t)
	homedir := populatedHome(t, docroot)
	p.DirInUse = func(string) (bool, error) { return true, nil }

	require.NoError(t, deleteUser(t, p, shellUser(docroot)))

	assert.FileExists(t, filepath.Join(homedir, ".bashrc"),
		"another account still logs in here")
	assert.NotContains(t, runner.all(), "chattr -i "+docroot)
	assert.Contains(t, runner.all(), "userdel -f web1user",
		"the account itself still goes away")
}

func TestDeleteStopsAndStartsPHPFPMAroundUserdel(t *testing.T) {
	p, runner, docroot := deletablePlugin(t)
	populatedHome(t, docroot)
	p.LoadWeb = func(int64) (system.Row, error) {
		return system.Row{
			"document_root": docroot, "server_id": int64(1),
			"php": "php-fpm", "server_php_id": int64(0),
		}, nil
	}
	p.LoadWebConfig = func(uint32) (*getconf.WebConfig, error) {
		return &getconf.WebConfig{PHPFPMInitScript: "php8.3-fpm"}, nil
	}

	require.NoError(t, deleteUser(t, p, shellUser(docroot)))

	cmds := runner.all()
	assert.Equal(t, []string{
		"systemctl stop php8.3-fpm",
		"killall -u web1user",
		"userdel -f web1user",
		"systemctl start php8.3-fpm",
	}, cmds[len(cmds)-4:],
		"the pool runs under the same uid: killall would take it down and userdel would then refuse")
}

func TestDeleteUsesThePinnedPHPVersionUnit(t *testing.T) {
	p, runner, docroot := deletablePlugin(t)
	populatedHome(t, docroot)
	p.LoadWeb = func(int64) (system.Row, error) {
		return system.Row{
			"document_root": docroot, "server_id": int64(1),
			"php": "php-fpm", "server_php_id": int64(7),
		}, nil
	}
	p.LoadServerPHPUnit = func(id int64) (string, error) {
		require.Equal(t, int64(7), id)
		return "php7.4-fpm", nil
	}

	require.NoError(t, deleteUser(t, p, shellUser(docroot)))

	assert.Contains(t, runner.all(), "systemctl stop php7.4-fpm")
	assert.Contains(t, runner.all(), "systemctl start php7.4-fpm")
}

func TestDeleteStartsPHPFPMAgainWhenUserdelFails(t *testing.T) {
	p, runner, docroot := deletablePlugin(t)
	populatedHome(t, docroot)
	p.LoadWeb = func(int64) (system.Row, error) {
		return system.Row{
			"document_root": docroot, "server_id": int64(1),
			"php": "php-fpm", "server_php_id": int64(0),
		}, nil
	}
	p.LoadWebConfig = func(uint32) (*getconf.WebConfig, error) {
		return &getconf.WebConfig{PHPFPMInitScript: "php8.3-fpm"}, nil
	}
	runner.fail = "userdel"

	err := deleteUser(t, p, shellUser(docroot))

	require.ErrorContains(t, err, "userdel web1user")
	cmds := runner.all()
	assert.Equal(t, "systemctl start php8.3-fpm", cmds[len(cmds)-1],
		"the site must not be left with a stopped pool")
}

func TestDeleteOfAJailkitUserLeavesUserdelToTheJailkitPlugin(t *testing.T) {
	p, runner, docroot := deletablePlugin(t)
	u := shellUser(docroot)
	u["chroot"] = "jailkit"
	// A jailed account is chrooted to its login dir, so its dotfiles sit
	// there rather than under home/<user>.
	for _, name := range ownedDotfiles {
		require.NoError(t, os.WriteFile(filepath.Join(docroot, name), []byte("x"), 0o644))
	}

	require.NoError(t, deleteUser(t, p, u))

	assert.NoFileExists(t, filepath.Join(docroot, ".bashrc"), "the jailed home is cleaned up here")
	assert.NotContains(t, runner.all(), "userdel -f web1user",
		"the account has to leave the jail's passwd file too, which the jailkit plugin owns")
}

func TestDeleteOfAMissingAccountIsSkipped(t *testing.T) {
	p, runner, docroot := deletablePlugin(t)
	homedir := populatedHome(t, docroot)
	p.LookupUID = func(string) (int, bool) { return 0, false }

	require.NoError(t, deleteUser(t, p, shellUser(docroot)))

	assert.Empty(t, runner.all())
	assert.FileExists(t, filepath.Join(homedir, ".bashrc"))
}

func TestDeleteRefusesASystemAccount(t *testing.T) {
	p, runner, docroot := deletablePlugin(t)
	populatedHome(t, docroot)
	p.LookupUID = func(string) (int, bool) { return minUID, true }

	err := deleteUser(t, p, shellUser(docroot))

	require.ErrorContains(t, err, "system account")
	assert.Empty(t, runner.all())
}

func TestDeleteDisabledBySecuritySetting(t *testing.T) {
	p, runner, docroot := deletablePlugin(t)
	homedir := populatedHome(t, docroot)
	p.AllowShellUser = func() (bool, error) { return false, nil }

	require.NoError(t, deleteUser(t, p, shellUser(docroot)))

	assert.Empty(t, runner.all())
	assert.FileExists(t, filepath.Join(homedir, ".bashrc"))
}

func TestDeleteRefusesADirOutsideTheAllowedPaths(t *testing.T) {
	p, runner, _ := deletablePlugin(t)

	require.NoError(t, deleteUser(t, p, shellUser("/etc/ssh")))

	assert.Empty(t, runner.all())
}
