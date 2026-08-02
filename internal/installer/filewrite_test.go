package installer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteFileBackupNewFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "file.conf")
	changed, restore, err := writeFileBackup(path, []byte("new"), 0o600)
	require.NoError(t, err)
	assert.True(t, changed)

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "new", string(got))
	info, _ := os.Stat(path)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	// Restore of a fresh file removes it.
	require.NoError(t, restore())
	assert.NoFileExists(t, path)
}

func TestWriteFileBackupDifferingContent(t *testing.T) {
	old := nowUnix
	nowUnix = func() int64 { return 1234 }
	t.Cleanup(func() { nowUnix = old })

	path := filepath.Join(t.TempDir(), "file.conf")
	require.NoError(t, os.WriteFile(path, []byte("old"), 0o644))

	changed, restore, err := writeFileBackup(path, []byte("new"), 0o644)
	require.NoError(t, err)
	assert.True(t, changed)

	backup, err := os.ReadFile(path + ".bak-1234")
	require.NoError(t, err, "differing file must be backed up")
	assert.Equal(t, "old", string(backup))

	// Restore brings the original content back.
	require.NoError(t, restore())
	got, _ := os.ReadFile(path)
	assert.Equal(t, "old", string(got))
}

func TestWriteFileBackupIdenticalNoop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file.conf")
	require.NoError(t, os.WriteFile(path, []byte("same"), 0o644))

	changed, restore, err := writeFileBackup(path, []byte("same"), 0o644)
	require.NoError(t, err)
	assert.False(t, changed)
	assert.Nil(t, restore)

	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	assert.Len(t, entries, 1, "no backup file for identical content")
}
