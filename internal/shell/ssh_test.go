package shell

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	keyA = "ssh-rsa AAAAB3NzaC1yc2EAAAA-a user-a@host"
	keyB = "ssh-rsa AAAAB3NzaC1yc2EAAAA-b user-b@host"
	keyC = "ssh-rsa AAAAB3NzaC1yc2EAAAA-c user-c@host"
)

// authorizedKeys returns the key lines currently written for the account.
func authorizedKeys(t *testing.T, homedir string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(homedir, ".ssh", "authorized_keys"))
	require.NoError(t, err)
	return splitKeys(string(data))
}

func TestInsertWritesSiteAndClientKeys(t *testing.T) {
	p, runner, docroot := testPlugin(t)
	p.LoadSiteSSHKeys = func(int64) ([]string, error) { return []string{keyA}, nil }
	p.LoadClientSSHKey = func(int64) (string, error) { return keyB, nil }

	require.NoError(t, insert(t, p, shellUser(docroot)))

	homedir := filepath.Join(docroot, "home", "web1user")
	assert.ElementsMatch(t, []string{keyA, keyB}, authorizedKeys(t, homedir),
		"a fresh file gets the client key on top of the site keys")

	sshDir := filepath.Join(homedir, ".ssh")
	fi, err := os.Stat(sshDir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), fi.Mode().Perm())
	fi, err = os.Stat(filepath.Join(sshDir, "authorized_keys"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
	assert.Contains(t, runner.all(), "chown -R web1:client1 "+sshDir)
}

func TestInsertSeedsTheAdminKeyOnce(t *testing.T) {
	p, _, docroot := testPlugin(t)
	adminKeys := filepath.Join(t.TempDir(), "root_authorized_keys")
	require.NoError(t, os.WriteFile(adminKeys, []byte(keyC+"\n"), 0o600))
	p.RootAuthorizedKeys = adminKeys
	p.LoadSiteSSHKeys = func(int64) ([]string, error) { return []string{keyA}, nil }

	require.NoError(t, insert(t, p, shellUser(docroot)))

	homedir := filepath.Join(docroot, "home", "web1user")
	assert.ElementsMatch(t, []string{keyA, keyC}, authorizedKeys(t, homedir),
		"the admin keeps SSH access to the account")

	// A second run must not re-add it: the file exists now.
	require.NoError(t, os.WriteFile(adminKeys, []byte(keyB+"\n"), 0o600))
	require.NoError(t, insert(t, p, shellUser(docroot)))
	assert.NotContains(t, authorizedKeys(t, homedir), keyB,
		"the admin key is seeded at creation only")
}

func TestUpdateReplacesTheOldKey(t *testing.T) {
	p, runner, docroot := testPlugin(t)
	old := shellUser(docroot)
	old["ssh_rsa"] = keyA
	p.LoadSiteSSHKeys = func(int64) ([]string, error) { return []string{keyA}, nil }
	existingAccount(t, p, runner, old)

	homedir := filepath.Join(docroot, "home", "web1user")
	require.Equal(t, []string{keyA}, authorizedKeys(t, homedir))

	u := shellUser(docroot)
	u["ssh_rsa"] = keyB
	p.LoadSiteSSHKeys = func(int64) ([]string, error) { return []string{keyB}, nil }
	require.NoError(t, update(t, p, old, u))

	assert.Equal(t, []string{keyB}, authorizedKeys(t, homedir),
		"editing a key in the panel must not leave the previous one behind")
}

func TestUpdateKeepsKeysOfTheOtherShellUsers(t *testing.T) {
	p, runner, docroot := testPlugin(t)
	old := shellUser(docroot)
	old["ssh_rsa"] = keyA
	// keyB belongs to a second shell user of the same website.
	p.LoadSiteSSHKeys = func(int64) ([]string, error) { return []string{keyA, keyB}, nil }
	existingAccount(t, p, runner, old)

	u := shellUser(docroot)
	u["ssh_rsa"] = ""
	p.LoadSiteSSHKeys = func(int64) ([]string, error) { return []string{keyB}, nil }
	require.NoError(t, update(t, p, old, u))

	assert.Equal(t, []string{keyB}, authorizedKeys(t, filepath.Join(docroot, "home", "web1user")),
		"dropping one user's key leaves the site's other keys in place")
}

func TestUpdateKeepsAManuallyAddedKey(t *testing.T) {
	p, runner, docroot := testPlugin(t)
	u := shellUser(docroot)
	p.LoadSiteSSHKeys = func(int64) ([]string, error) { return []string{keyA}, nil }
	existingAccount(t, p, runner, u)

	homedir := filepath.Join(docroot, "home", "web1user")
	keyFile := filepath.Join(homedir, ".ssh", "authorized_keys")
	require.NoError(t, os.WriteFile(keyFile, []byte(keyA+"\n"+keyC+"\n"), 0o600))

	require.NoError(t, update(t, p, u, u))

	assert.ElementsMatch(t, []string{keyA, keyC}, authorizedKeys(t, homedir),
		"a key the user added over SFTP is not managed by the panel and survives")
}

func TestSetupSSHRSAUsesTheJailedHome(t *testing.T) {
	p, runner, docroot := testPlugin(t)
	u := shellUser(docroot)
	u["chroot"] = "jailkit"
	p.LoadSiteSSHKeys = func(int64) ([]string, error) { return []string{keyA}, nil }
	existingAccount(t, p, runner, u)

	// The insert path always builds dir/home/<user>; the update path is
	// where the jailed layout takes over.
	require.NoError(t, update(t, p, u, u))

	assert.Equal(t, []string{keyA}, authorizedKeys(t, docroot),
		"a jailed account keeps its keys in the login directory itself")
}

func TestDedupeAndWithout(t *testing.T) {
	assert.Equal(t, []string{keyA, keyB}, dedupe([]string{keyA, keyB, keyA}))
	assert.Empty(t, dedupe(nil))
	assert.Equal(t, []string{keyB}, without([]string{keyA, keyB}, []string{keyA}))
	assert.Equal(t, []string{keyA, keyB}, without([]string{keyA, keyB}, nil))
}

func TestSplitKeys(t *testing.T) {
	assert.Equal(t, []string{keyA, keyB},
		splitKeys("\r\n"+keyA+"\r\n\n  "+keyB+"  \n\n"),
		"CRLF, blank lines and stray whitespace are all normalised away")
	assert.Empty(t, splitKeys("   \n\n"))
}
