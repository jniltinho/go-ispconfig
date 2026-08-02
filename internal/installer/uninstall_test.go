package installer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeInstalled lays down the artifacts an install leaves behind.
func fakeInstalled(t *testing.T, st *State) (unitFile, include, configFile string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(st.SystemdDir, 0o755))
	unitFile = filepath.Join(st.SystemdDir, ServeUnitName)
	require.NoError(t, os.WriteFile(unitFile, []byte("[Unit]"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(st.SystemdDir, DaemonUnitName), []byte("[Unit]"), 0o644))

	include = filepath.Join(st.Profile.NginxConfigDir, "conf.d", "go-ispconfig-sites.conf")
	require.NoError(t, os.MkdirAll(filepath.Dir(include), 0o755))
	require.NoError(t, os.WriteFile(include, []byte("include ...;"), 0o644))

	require.NoError(t, os.MkdirAll(st.ConfigDir, 0o755))
	configFile = filepath.Join(st.ConfigDir, "config.toml")
	require.NoError(t, os.WriteFile(configFile, []byte("[server]"), 0o640))
	return unitFile, include, configFile
}

func TestUninstallRemovesUnitsConfigsAndConfigDir(t *testing.T) {
	st, mock, _ := testState(t)
	unitFile, include, _ := fakeInstalled(t, st)

	require.NoError(t, Uninstall(context.Background(), st, UninstallOptions{}))
	assert.NoFileExists(t, unitFile)
	assert.NoFileExists(t, include)
	assert.NoDirExists(t, st.ConfigDir)
	assert.True(t, mock.called("systemctl disable --now go-ispconfig-serve.service go-ispconfig-daemon.service"))
	assert.True(t, mock.called("systemctl daemon-reload"))
	assert.True(t, mock.called("systemctl reload nginx"))

	joined := strings.Join(mock.calls, "|")
	assert.NotContains(t, joined, "apt-get", "packages are never removed")
	assert.NotContains(t, joined, "remove", "no package removal commands")

	// Re-run tolerates already-removed artifacts.
	require.NoError(t, Uninstall(context.Background(), st, UninstallOptions{}))
}

func TestUninstallKeepConfig(t *testing.T) {
	st, _, _ := testState(t)
	_, _, configFile := fakeInstalled(t, st)

	require.NoError(t, Uninstall(context.Background(), st, UninstallOptions{KeepConfig: true}))
	assert.FileExists(t, configFile, "--keep-config preserves /etc/go-ispconfig")
}
