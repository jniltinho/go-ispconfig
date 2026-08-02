package clientdb

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeConf(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mysql_clientdb.conf")
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))
	return path
}

func TestLoadConfig(t *testing.T) {
	cfg, err := LoadConfig(writeConf(t, `
clientdb_host = "db.local"
clientdb_port = 3307
clientdb_user = "goisp_clientdb"
clientdb_password = "s3cret"
`))
	require.NoError(t, err)
	require.Equal(t, Config{Host: "db.local", Port: 3307, User: "goisp_clientdb", Password: "s3cret"}, cfg)
}

func TestLoadConfigDefaults(t *testing.T) {
	cfg, err := LoadConfig(writeConf(t, `
clientdb_user = "goisp_clientdb"
clientdb_password = "x"
`))
	require.NoError(t, err)
	require.Equal(t, "localhost", cfg.Host)
	require.Equal(t, 3306, cfg.Port)
}

func TestLoadConfigMissingFile(t *testing.T) {
	_, err := LoadConfig(filepath.Join(t.TempDir(), "nope.conf"))
	require.Error(t, err)
}

func TestLoadConfigMissingUser(t *testing.T) {
	_, err := LoadConfig(writeConf(t, `clientdb_password = "x"`))
	require.ErrorContains(t, err, "clientdb_user is required")
}

func TestLoadConfigInvalidPort(t *testing.T) {
	_, err := LoadConfig(writeConf(t, `
clientdb_user = "u"
clientdb_port = 70000
`))
	require.ErrorContains(t, err, "invalid clientdb_port")
}
