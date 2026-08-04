package installer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExistingDBPassword(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(cfg, []byte(
		"[database]\ndsn = \"ispconfig:s3cret@tcp(127.0.0.1:3306)/dbispconfig?charset=utf8mb4\"\n"), 0o600))

	assert.Equal(t, "s3cret", existingDBPassword(cfg, "ispconfig"))
	assert.Empty(t, existingDBPassword(cfg, "otheruser"), "user mismatch is not reused")
	assert.Empty(t, existingDBPassword(filepath.Join(dir, "missing.toml"), "ispconfig"))

	require.NoError(t, os.WriteFile(cfg, []byte("not toml at all ["), 0o600))
	assert.Empty(t, existingDBPassword(cfg, "ispconfig"), "unparsable config is ignored")
}

// TestWriteClientDBConfCreatesConfigDir pins the fix for a bug a fresh-machine
// install found and no test did: the mariadb step runs before configTomlStep,
// so /etc/go-ispconfig does not exist yet when the client-DB credentials are
// written. A bare os.WriteFile there aborted the whole install.
func TestWriteClientDBConfCreatesConfigDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "go-ispconfig", "mysql_clientdb.conf")

	require.NoError(t, writeClientDBConf(path, "goisp_clientdb", "s3cret"))
	assert.Equal(t, "s3cret", existingClientDBPassword(path))

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "credentials must not be world-readable")

	// Re-running the installer must keep the password it wrote.
	require.NoError(t, writeClientDBConf(path, "goisp_clientdb", "s3cret"))
	assert.Equal(t, "s3cret", existingClientDBPassword(path))
}

func TestIspconfigDSN(t *testing.T) {
	st, _, _ := testState(t)
	st.DBAddr = "127.0.0.1:3306"
	st.DBPassword = "pw123"
	assert.Equal(t,
		"ispconfig:pw123@tcp(127.0.0.1:3306)/dbispconfig?charset=utf8mb4&parseTime=True&loc=Local",
		st.ispconfigDSN())
}
