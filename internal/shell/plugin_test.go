package shell

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

// fakeRunner records commands without executing anything: the plugin runs
// useradd/chown/chattr as root, which no test may do. It also implements
// engine.StdinRunner so the chpasswd pipe can be asserted.
type fakeRunner struct {
	mu    sync.Mutex
	runs  [][]string
	stdin map[string]string
	fail  string // command name whose invocation returns an error
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

func (f *fakeRunner) RunWithStdin(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
	out, err := f.Run(ctx, name, args...)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.stdin == nil {
		f.stdin = map[string]string{}
	}
	f.stdin[name] = string(stdin)
	return out, err
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

// siteRoot creates a document root for the fake website. It deliberately
// avoids t.TempDir(): system::is_allowed_path refuses everything under /tmp,
// which is exactly where the Go temp dir lives.
func siteRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	require.NoError(t, err)
	base, err := os.MkdirTemp(cwd, "site-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	base, err = filepath.EvalSymlinks(base)
	require.NoError(t, err)

	docroot := filepath.Join(base, "web1")
	require.NoError(t, os.MkdirAll(docroot, 0o755))
	return docroot
}

// testPlugin builds a plugin whose parent site is docroot, owned by
// web1:client1 (uid/gid 5001), with web_folder_protection enabled and the
// security kill-switch on.
func testPlugin(t *testing.T) (*Plugin, *fakeRunner, string) {
	t.Helper()
	docroot := siteRoot(t)
	runner := &fakeRunner{}
	p := NewPlugin(nil, runner, nil)
	p.LoadWeb = func(int64) (system.Row, error) {
		return system.Row{
			"domain_id": int64(1), "document_root": docroot, "server_id": int64(1),
			"system_user": "web1", "system_group": "client1",
		}, nil
	}
	p.LoadWebConfig = func(uint32) (*getconf.WebConfig, error) {
		return &getconf.WebConfig{WebFolderProtection: "y"}, nil
	}
	p.AllowShellUser = func() (bool, error) { return true, nil }
	// Never read the host's real /root/.ssh/authorized_keys in a test.
	p.RootAuthorizedKeys = ""
	p.LookupUID = func(name string) (int, bool) {
		if name == "web1" {
			return 5001, true
		}
		return 0, false
	}
	p.LookupGID = func(name string) (int, bool) {
		if name == "client1" {
			return 5001, true
		}
		return 0, false
	}
	return p, runner, docroot
}

func shellUser(dir string) map[string]any {
	return map[string]any{
		"shell_user_id": float64(3), "username": "web1user", "dir": dir,
		"parent_domain_id": float64(1), "puser": "web1", "pgroup": "client1",
		"shell": "/bin/bash", "active": "y", "chroot": "",
		"password": "$6$salt$hash",
	}
}

func insert(t *testing.T, p *Plugin, u map[string]any) error {
	t.Helper()
	return p.shellUserInsert(context.Background(), "shell_user_insert", engine.Data{New: u})
}

func TestInsertCreatesAccountAndHomeLayout(t *testing.T) {
	p, runner, docroot := testPlugin(t)
	require.NoError(t, insert(t, p, shellUser(docroot)))

	homeBase := filepath.Join(docroot, "home")
	homedir := filepath.Join(homeBase, "web1user")

	fi, err := os.Stat(homeBase)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), fi.Mode().Perm())
	fi, err = os.Stat(homedir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o750), fi.Mode().Perm())

	assert.Equal(t, []string{
		"chattr -i " + docroot,
		"chown root:root " + homeBase,
		"chown web1:client1 " + homedir,
		"useradd -d " + homedir + " -g client1 -o -s /bin/bash -u 5001 web1user",
		"chpasswd -e",
		"chown root:root " + docroot,
		"chown -R web1:client1 " + filepath.Join(homedir, ".ssh"),
		"chown web1user:client1 " + filepath.Join(homedir, ".bash_history"),
		"chown web1user:client1 " + filepath.Join(homedir, ".profile"),
		"chown web1user:client1 " + filepath.Join(homedir, ".bashrc.d"),
		"chown web1user:client1 " + filepath.Join(homedir, ".local"),
		"chown web1user:client1 " + filepath.Join(homedir, ".local", "bin"),
		"chattr +i " + docroot,
	}, runner.all())

	assert.Equal(t, "web1user:$6$salt$hash\n", runner.stdin["chpasswd"],
		"the hash goes through the pipe, never through argv")

	profile, err := os.ReadFile(filepath.Join(homedir, ".profile"))
	require.NoError(t, err)
	assert.Equal(t, profileContent, string(profile))
	for name, mode := range map[string]os.FileMode{".bash_history": 0o750, ".profile": 0o644} {
		fi, err := os.Stat(filepath.Join(homedir, name))
		require.NoError(t, err, name)
		assert.Equal(t, mode, fi.Mode().Perm(), name)
	}
	for _, dir := range []string{".bashrc.d", ".local/bin"} {
		fi, err := os.Stat(filepath.Join(homedir, dir))
		require.NoError(t, err, dir)
		assert.True(t, fi.IsDir(), dir)
	}
	for _, name := range []string{"web", "log", "private"} {
		target, err := os.Readlink(filepath.Join(homedir, name))
		require.NoError(t, err, name)
		assert.Equal(t, "../../"+name, target, "relative so it also resolves inside a jail")
	}
}

func TestInsertJailkitUserIsParkedWithoutShell(t *testing.T) {
	p, runner, docroot := testPlugin(t)
	u := shellUser(docroot)
	u["chroot"] = "jailkit"

	require.NoError(t, insert(t, p, u))

	cmds := runner.all()
	assert.Contains(t, cmds,
		"useradd -d "+filepath.Join(docroot, "home", "web1user")+" -g client1 -o -s /bin/false -u 5001 web1user",
		"a jailkit user gets no shell until its chroot exists")
	assert.Contains(t, cmds, "usermod -s /bin/false -L web1user",
		"and is locked until the jailkit plugin unlocks it")
}

func TestInsertInactiveUserGetsFalseShell(t *testing.T) {
	p, runner, docroot := testPlugin(t)
	u := shellUser(docroot)
	u["active"] = "n"

	require.NoError(t, insert(t, p, u))

	assert.Contains(t, runner.all(),
		"useradd -d "+filepath.Join(docroot, "home", "web1user")+" -g client1 -o -s /bin/false -u 5001 web1user")
	assert.NotContains(t, runner.all(), "usermod -s /bin/false -L web1user",
		"only jailkit users are locked")
}

func TestInsertWithoutPasswordSkipsChpasswd(t *testing.T) {
	p, runner, docroot := testPlugin(t)
	u := shellUser(docroot)
	u["password"] = ""

	require.NoError(t, insert(t, p, u))

	assert.NotContains(t, runner.all(), "chpasswd -e")
}

func TestInsertSurvivesChpasswdFailure(t *testing.T) {
	p, runner, docroot := testPlugin(t)
	runner.fail = "chpasswd"

	require.NoError(t, insert(t, p, shellUser(docroot)),
		"a rejected hash must not undo the account that was already created")
	assert.Contains(t, runner.all(), "chattr +i "+docroot)
}

func TestInsertDisabledBySecuritySetting(t *testing.T) {
	p, runner, docroot := testPlugin(t)
	p.AllowShellUser = func() (bool, error) { return false, nil }

	require.NoError(t, insert(t, p, shellUser(docroot)))

	assert.Empty(t, runner.all(), "the kill-switch stops the plugin before any command")
	assert.NoDirExists(t, filepath.Join(docroot, "home"))
}

func TestInsertRefusedGuards(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(u map[string]any, docroot string)
	}{
		{"dir outside the docroot", func(u map[string]any, docroot string) {
			u["dir"] = filepath.Dir(docroot)
		}},
		{"dir in a sibling site sharing the prefix", func(u map[string]any, docroot string) {
			u["dir"] = docroot + "2"
		}},
		{"dir traversing out of the site", func(u map[string]any, docroot string) {
			u["dir"] = docroot + "/../web2"
		}},
		{"username root", func(u map[string]any, _ string) { u["username"] = "root" }},
		{"username with a shell metacharacter", func(u map[string]any, _ string) {
			u["username"] = "web1user;id"
		}},
		{"username longer than 32 chars", func(u map[string]any, _ string) {
			u["username"] = strings.Repeat("a", 33)
		}},
		{"parent user that is not a site user", func(u map[string]any, _ string) {
			u["puser"] = "ispconfig"
		}},
		{"parent user that does not exist", func(u map[string]any, _ string) {
			u["puser"] = "web9"
		}},
		{"parent group that is not a client group", func(u map[string]any, _ string) {
			u["pgroup"] = "root"
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, runner, docroot := testPlugin(t)
			u := shellUser(docroot)
			tt.mutate(u, docroot)

			require.NoError(t, insert(t, p, u), "a refused payload is logged, not retried forever")
			assert.Empty(t, runner.all(), "no command runs for a payload that fails a guard")
		})
	}
}

func TestInsertRefusesSystemDir(t *testing.T) {
	p, runner, _ := testPlugin(t)
	// A docroot the operator pointed at /etc: the containment check passes,
	// is_allowed_path is what stops it.
	p.LoadWeb = func(int64) (system.Row, error) {
		return system.Row{"document_root": "/etc", "server_id": int64(1)}, nil
	}

	require.NoError(t, insert(t, p, shellUser("/etc/ssh")))

	assert.Empty(t, runner.all())
}

func TestInsertRefusesSymlinkedDir(t *testing.T) {
	p, runner, docroot := testPlugin(t)
	link := filepath.Join(docroot, "link")
	require.NoError(t, os.Symlink(filepath.Join(docroot, "real"), link))

	require.NoError(t, insert(t, p, shellUser(link)))

	assert.Empty(t, runner.all(), "a symlinked login dir could redirect the root chowns")
}

func TestInsertRefusedWhenParentUIDIsASystemUser(t *testing.T) {
	p, runner, docroot := testPlugin(t)
	// A site user whose UID landed below the system floor: giving the shell
	// account that UID would hand it a system identity.
	p.LookupUID = func(string) (int, bool) { return systemMinUID - 1, true }

	require.NoError(t, insert(t, p, shellUser(docroot)))

	assert.Empty(t, runner.all())
	assert.NoDirExists(t, filepath.Join(docroot, "home"))
}

func TestInsertRelocksDocrootWhenUseraddFails(t *testing.T) {
	p, runner, docroot := testPlugin(t)
	runner.fail = "useradd"

	err := insert(t, p, shellUser(docroot))

	require.ErrorContains(t, err, "useradd web1user")
	cmds := runner.all()
	assert.Equal(t, "chattr +i "+docroot, cmds[len(cmds)-1],
		"the docroot is relocked even when the account was not created")
}

func TestInsertIsIdempotentOnExistingHome(t *testing.T) {
	p, runner, docroot := testPlugin(t)
	require.NoError(t, insert(t, p, shellUser(docroot)))
	first := len(runner.all())

	require.NoError(t, insert(t, p, shellUser(docroot)),
		"a replayed datalog row must not fail on the layout it already created")
	assert.Greater(t, len(runner.all()), first)
	assert.NotContains(t, runner.all()[first:],
		"chown web1user:client1 "+filepath.Join(docroot, "home", "web1user", ".bashrc.d"),
		"directories that are already there are not recreated")
}

func TestShellOf(t *testing.T) {
	tests := []struct {
		active, chroot, shell, want string
	}{
		{"y", "", "/bin/bash", "/bin/bash"},
		{"y", "none", "/bin/sh", "/bin/sh"},
		{"n", "", "/bin/bash", "/bin/false"},
		{"y", "jailkit", "/bin/bash", "/bin/false"},
		{"n", "jailkit", "/bin/bash", "/bin/false"},
	}
	for _, tt := range tests {
		got := shellOf(system.Row{"active": tt.active, "chroot": tt.chroot, "shell": tt.shell})
		assert.Equal(t, tt.want, got, tt.active+"/"+tt.chroot)
	}
}
