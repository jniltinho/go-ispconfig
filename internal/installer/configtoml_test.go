package installer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserStepCreatesWhenMissing(t *testing.T) {
	st, mock, _ := testState(t)
	mock.fail["id -u go-ispconfig"] = "no such user"
	require.NoError(t, userStep{}.Run(context.Background(), st))
	assert.True(t, mock.called("useradd --system"))
}

func TestUserStepSkipsWhenExists(t *testing.T) {
	st, mock, _ := testState(t)
	err := userStep{}.Run(context.Background(), st)
	require.ErrorContains(t, err, "exists")
	assert.False(t, mock.called("useradd"))
	assert.False(t, mock.called("groupadd"), "existing sshusers group is kept")
}

func TestUserStepCreatesSshusersGroupWhenMissing(t *testing.T) {
	st, mock, _ := testState(t)
	mock.fail["getent group sshusers"] = "no such group"
	err := userStep{}.Run(context.Background(), st)
	require.ErrorContains(t, err, "exists") // panel user present -> skip
	assert.True(t, mock.called("groupadd sshusers"))
}

func TestConfigTomlWriteAndRerun(t *testing.T) {
	st, mock, _ := testState(t)
	st.DBPassword = "generated1"
	ctx := context.Background()

	require.NoError(t, configTomlStep{}.Run(ctx, st))
	path := filepath.Join(st.ConfigDir, "config.toml")
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(content), "port = 8080")
	assert.Contains(t, string(content), "ispconfig:generated1@tcp(127.0.0.1:3306)/dbispconfig")
	info, _ := os.Stat(path)
	assert.Equal(t, os.FileMode(0o640), info.Mode().Perm())
	assert.True(t, mock.called("chown root:go-ispconfig "+path))

	// Same content on re-run: skip, no backup.
	err = configTomlStep{}.Run(ctx, st)
	require.ErrorContains(t, err, "unchanged")
	entries, _ := os.ReadDir(st.ConfigDir)
	assert.Len(t, entries, 1, "no backup for identical re-run")

	// Changed port: backup created, credentials preserved in new content.
	st.Answers.PanelPort = 9443
	require.NoError(t, configTomlStep{}.Run(ctx, st))
	entries, _ = os.ReadDir(st.ConfigDir)
	assert.Len(t, entries, 2, "differing content is backed up")
	content, _ = os.ReadFile(path)
	assert.Contains(t, string(content), "port = 9443")
	assert.Contains(t, string(content), "generated1")

	// The mariadb step must be able to read the password back (reuse path).
	assert.Equal(t, "generated1", existingDBPassword(path, "ispconfig"))
}
