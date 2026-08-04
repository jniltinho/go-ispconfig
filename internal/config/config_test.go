package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

// writeConfig writes a temp config.toml and returns its path.
func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	return path
}

func TestDefaults(t *testing.T) {
	t.Cleanup(viper.Reset)
	viper.Reset()

	// Empty file: every value must come from the built-in defaults.
	require.NoError(t, Init(writeConfig(t, "")))
	cfg, err := Load()
	require.NoError(t, err)

	require.Equal(t, "0.0.0.0", cfg.Server.Host)
	require.Equal(t, 8080, cfg.Server.Port)
	require.Equal(t, "", cfg.Server.TLSCert)
	require.Equal(t, 10, cfg.Daemon.TickSeconds)
	require.False(t, cfg.Auth.RehashLegacy)
	require.Contains(t, cfg.Database.DSN, "dbispconfig")
}

func TestFileOverridesDefaults(t *testing.T) {
	t.Cleanup(viper.Reset)
	viper.Reset()

	path := writeConfig(t, `
[server]
host = "127.0.0.1"
port = 9090

[daemon]
tick_seconds = 30
`)
	require.NoError(t, Init(path))
	cfg, err := Load()
	require.NoError(t, err)

	require.Equal(t, "127.0.0.1", cfg.Server.Host)
	require.Equal(t, 9090, cfg.Server.Port)
	require.Equal(t, 30, cfg.Daemon.TickSeconds)
	// Untouched keys keep their defaults.
	require.False(t, cfg.Auth.RehashLegacy)
}

func TestEnvOverridesFile(t *testing.T) {
	t.Cleanup(viper.Reset)
	viper.Reset()

	path := writeConfig(t, `
[server]
port = 9090

[auth]
rehash_legacy = false
`)
	t.Setenv("GOISP_SERVER_PORT", "7070")
	t.Setenv("GOISP_AUTH_REHASH_LEGACY", "true")
	t.Setenv("GOISP_DATABASE_DSN", "user:pw@tcp(db:3306)/dbispconfig")

	require.NoError(t, Init(path))
	cfg, err := Load()
	require.NoError(t, err)

	require.Equal(t, 7070, cfg.Server.Port)
	require.True(t, cfg.Auth.RehashLegacy)
	require.Equal(t, "user:pw@tcp(db:3306)/dbispconfig", cfg.Database.DSN)
}

func TestExplicitMissingFileFails(t *testing.T) {
	t.Cleanup(viper.Reset)
	viper.Reset()

	require.Error(t, Init(filepath.Join(t.TempDir(), "nope.toml")))
}

func TestPowerDNSConfigDefaultsAndOverride(t *testing.T) {
	t.Cleanup(viper.Reset)
	viper.Reset()

	// Empty file: PowerDNS DSN override is empty (derive from database.dsn).
	require.NoError(t, Init(writeConfig(t, "")))
	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, "", cfg.PowerDNS.DSN)

	t.Cleanup(viper.Reset)
	viper.Reset()
	path := writeConfig(t, `
[powerdns]
dsn = "ispconfig:secret@tcp(127.0.0.1:3306)/powerdns?parseTime=true"
`)
	require.NoError(t, Init(path))
	cfg, err = Load()
	require.NoError(t, err)
	require.Equal(t, "ispconfig:secret@tcp(127.0.0.1:3306)/powerdns?parseTime=true", cfg.PowerDNS.DSN)

	// Env override.
	t.Setenv("GOISP_POWERDNS_DSN", "root:root@tcp(db:3306)/powerdns")
	require.NoError(t, Init(path))
	cfg, err = Load()
	require.NoError(t, err)
	require.Equal(t, "root:root@tcp(db:3306)/powerdns", cfg.PowerDNS.DSN)
}

func TestLogLevel(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
		{"panic", slog.LevelError}, // slog has no panic level
		{"", slog.LevelInfo},
		{"nonsense", slog.LevelInfo},
	} {
		require.Equal(t, tc.want, LogConfig{Level: tc.in}.Slog(), "level %q", tc.in)
	}
}

func TestLogLevelFromFileAndEnv(t *testing.T) {
	t.Cleanup(viper.Reset)
	viper.Reset()

	require.NoError(t, Init(writeConfig(t, "[log]\nlevel = \"warn\"\n")))
	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, slog.LevelWarn, cfg.Log.Slog())

	// Bare LOG_LEVEL wins over the file.
	viper.Reset()
	t.Setenv("LOG_LEVEL", "debug")
	require.NoError(t, Init(writeConfig(t, "[log]\nlevel = \"warn\"\n")))
	cfg, err = Load()
	require.NoError(t, err)
	require.Equal(t, slog.LevelDebug, cfg.Log.Slog())
}
