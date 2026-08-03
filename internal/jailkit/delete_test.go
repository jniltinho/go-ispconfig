package jailkit

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

func TestDeleteRemovesAccountAndPasswdLine(t *testing.T) {
	p, runner, docroot := testPlugin(t)
	// Pre-seed a jail passwd with the user and a sibling.
	require.NoError(t, os.MkdirAll(filepath.Join(docroot, "etc"), 0o755))
	passwd := filepath.Join(docroot, "etc", "passwd")
	require.NoError(t, os.WriteFile(passwd,
		[]byte("root:x:0:0::/root:/bin/bash\nweb1user:x:5001:5001::/home/web1user:/bin/bash\n"),
		0o644))
	require.NoError(t, os.WriteFile(filepath.Join(docroot, "etc", "shadow"),
		[]byte("web1user:!:1:0:99999\nother:!:1:0:99999\n"), 0o640))

	home := filepath.Join(docroot, "home", "web1user")
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".ssh"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".profile"), []byte("x"), 0o644))

	// Ownership checks use the test process uid.
	p.LookupUID = func(name string) (int, bool) {
		if name == "web1user" || name == "web1" {
			return os.Getuid(), true
		}
		return 0, false
	}

	old := shellUser(docroot)
	require.NoError(t, p.shellUserDelete(context.Background(), "shell_user_delete",
		engine.Data{Old: old}))

	assert.True(t, runner.contains("killall -u web1user"), runner.all())
	assert.True(t, runner.contains("userdel -f web1user"), runner.all())

	data, err := os.ReadFile(passwd)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "web1user:")
	assert.Contains(t, string(data), "root:")

	shadow, err := os.ReadFile(filepath.Join(docroot, "etc", "shadow"))
	require.NoError(t, err)
	assert.NotContains(t, string(shadow), "web1user:")
	assert.Contains(t, string(shadow), "other:")
}

func TestDeleteKeepsJailWhenDeleteUnusedIsN(t *testing.T) {
	p, runner, docroot := testPlugin(t)
	require.NoError(t, os.MkdirAll(filepath.Join(docroot, "etc", "jailkit"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(docroot, "bin"), 0o755))
	p.LoadWeb = func(int64) (system.Row, error) {
		return system.Row{
			"domain_id": int64(1), "document_root": docroot, "server_id": int64(1),
			"delete_unused_jailkit": "n",
		}, nil
	}
	cleared := false
	p.ClearHash = func(string) error { cleared = true; return nil }

	require.NoError(t, p.shellUserDelete(context.Background(), "shell_user_delete",
		engine.Data{Old: shellUser(docroot)}))

	assert.DirExists(t, filepath.Join(docroot, "etc", "jailkit"))
	assert.DirExists(t, filepath.Join(docroot, "bin"))
	assert.False(t, cleared)
	assert.True(t, runner.contains("userdel -f web1user"))
}

func TestDeleteTearsDownUnusedJail(t *testing.T) {
	p, _, docroot := testPlugin(t)
	require.NoError(t, os.MkdirAll(filepath.Join(docroot, "etc", "jailkit"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(docroot, "bin"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(docroot, "usr"), 0o755))
	// A web_folder that must survive.
	require.NoError(t, os.MkdirAll(filepath.Join(docroot, "sub"), 0o755))

	p.LoadWeb = func(int64) (system.Row, error) {
		return system.Row{
			"domain_id": int64(1), "document_root": docroot, "server_id": int64(1),
			"delete_unused_jailkit": "y",
		}, nil
	}
	p.ListWebFolders = func(int64, string, uint32) ([]string, error) {
		return []string{"sub"}, nil
	}
	p.JailkitInUse = func(int64) (bool, error) { return false, nil }
	cleared := false
	p.ClearHash = func(d string) error {
		assert.Equal(t, docroot, d)
		cleared = true
		return nil
	}

	require.NoError(t, p.shellUserDelete(context.Background(), "shell_user_delete",
		engine.Data{Old: shellUser(docroot)}))

	assert.NoDirExists(t, filepath.Join(docroot, "etc"))
	assert.NoDirExists(t, filepath.Join(docroot, "bin"))
	assert.NoDirExists(t, filepath.Join(docroot, "usr"))
	assert.DirExists(t, filepath.Join(docroot, "sub"), "web_folder skip survives")
	assert.True(t, cleared)
}

func TestDeleteNonJailkitIsNoop(t *testing.T) {
	p, runner, docroot := testPlugin(t)
	old := shellUser(docroot)
	old["chroot"] = ""
	require.NoError(t, p.shellUserDelete(context.Background(), "shell_user_delete",
		engine.Data{Old: old}))
	assert.Empty(t, runner.all())
}

func TestRemoveLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "passwd")
	require.NoError(t, os.WriteFile(path, []byte("a:1\nb:2\nc:3\n"), 0o644))
	require.NoError(t, removeLine(path, "b:"))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "a:1\nc:3\n", string(data))
}

func TestJkInitArgs(t *testing.T) {
	args := jkInitArgs("/var/www/web1", []string{"coreutils", "ssh"}, []string{"hardlink", "force"})
	assert.Equal(t, []string{
		"-k", "-f", "-c", "/etc/jailkit/jk_init.ini", "-j", "/var/www/web1",
		"coreutils", "ssh",
	}, args)
}

func TestLoadWebConfigDefault(t *testing.T) {
	p := NewPlugin(nil, &fakeRunner{}, nil)
	cfg, err := p.LoadWebConfig(1)
	require.NoError(t, err)
	assert.NotNil(t, cfg)
	jk, err := p.LoadJailkitCfg(1)
	require.NoError(t, err)
	assert.Equal(t, getconf.DefaultJailkitConfig().ChrootHome, jk.ChrootHome)
}
