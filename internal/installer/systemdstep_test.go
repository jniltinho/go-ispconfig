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

func TestSystemdStepInstallsUnitsAndBinary(t *testing.T) {
	st, mock, _ := testState(t)
	self := filepath.Join(t.TempDir(), "go-ispconfig-built")
	require.NoError(t, os.WriteFile(self, []byte("BINARY"), 0o755))
	st.SelfExe = self
	ctx := context.Background()

	require.NoError(t, systemdStep{}.Run(ctx, st))

	bin, err := os.ReadFile(st.BinPath)
	require.NoError(t, err)
	assert.Equal(t, "BINARY", string(bin), "binary copied to units' ExecStart path")

	serve, err := os.ReadFile(filepath.Join(st.SystemdDir, ServeUnitName))
	require.NoError(t, err)
	assert.Contains(t, string(serve), "User=go-ispconfig")
	assert.FileExists(t, filepath.Join(st.SystemdDir, DaemonUnitName))

	assert.True(t, mock.called("systemctl daemon-reload"))
	assert.True(t, mock.called("systemctl enable --now go-ispconfig-serve.service go-ispconfig-daemon.service"))
	assert.NotContains(t, strings.Join(mock.calls, "|"), "crontab", "the installer never touches any crontab")

	// Re-run: identical units, no daemon-reload, enable stays (idempotent).
	mock.calls = nil
	require.NoError(t, systemdStep{}.Run(ctx, st))
	assert.False(t, mock.called("systemctl daemon-reload"), "no reload when units unchanged")
	assert.True(t, mock.called("systemctl enable --now"))
	assert.NotContains(t, strings.Join(mock.calls, "|"), "crontab")
}

func TestSystemdStepUpdateRestarts(t *testing.T) {
	st, mock, _ := testState(t)
	st.Update = true
	require.NoError(t, systemdStep{}.Run(context.Background(), st))
	assert.True(t, mock.called("systemctl restart go-ispconfig-serve.service go-ispconfig-daemon.service"))
	assert.False(t, mock.called("systemctl enable"), "update mode restarts, never re-enables")
}
