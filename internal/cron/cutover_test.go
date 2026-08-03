package cron

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/getconf"
)

func TestIsLegacyISPCronFile(t *testing.T) {
	assert.True(t, isLegacyISPCronFile("ispc_web1"))
	assert.True(t, isLegacyISPCronFile("ispc_chrooted_web1"))
	assert.True(t, isLegacyISPCronFile("ispc_web1.cron"))
	assert.True(t, isLegacyISPCronFile("ispc_chrooted_web1.cron"))
	assert.False(t, isLegacyISPCronFile("sysstat"))
	assert.False(t, isLegacyISPCronFile("php"))
	assert.False(t, isLegacyISPCronFile("notispc_web1"))
}

func TestRemoveLegacyCrontabs(t *testing.T) {
	dir := t.TempDir()
	// Create mix of legacy and unrelated files.
	for _, name := range []string{
		"ispc_web1",
		"ispc_chrooted_web2",
		"ispc_web3.cron",
		"sysstat",
		"README",
	} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("# test\n"), 0o644))
	}

	removed, err := RemoveLegacyCrontabs(dir, nil)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"ispc_web1", "ispc_chrooted_web2", "ispc_web3.cron"}, removed)

	// Legacy gone, others remain.
	_, err = os.Stat(filepath.Join(dir, "ispc_web1"))
	assert.True(t, os.IsNotExist(err))
	_, err = os.Stat(filepath.Join(dir, "sysstat"))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(dir, "README"))
	require.NoError(t, err)

	// Second pass is a no-op.
	removed, err = RemoveLegacyCrontabs(dir, nil)
	require.NoError(t, err)
	assert.Empty(t, removed)
}

func TestRemoveLegacyCrontabsMissingDir(t *testing.T) {
	removed, err := RemoveLegacyCrontabs(filepath.Join(t.TempDir(), "no-such"), nil)
	require.NoError(t, err)
	assert.Empty(t, removed)
}

func TestCrontabDir(t *testing.T) {
	assert.Equal(t, DefaultCrontabDir, CrontabDir(nil))
	assert.Equal(t, DefaultCrontabDir, CrontabDir(&getconf.ServerConfig{}))
	cfg := &getconf.ServerConfig{Raw: getconf.Sections{
		"cron": {"crontab_dir": "/tmp/custom-cron.d"},
	}}
	assert.Equal(t, "/tmp/custom-cron.d", CrontabDir(cfg))
}
