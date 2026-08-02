// Package clientdb ports ISPConfig3's database module stack: the daemon
// database_module (table hooks → named events) and the mysql_clientdb
// plugin that provisions real MySQL/MariaDB client databases, users and
// GRANTs (openspec change add-database-module).
package clientdb

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/viper"
)

// DefaultConfPath is where the installer writes the client-DB admin
// credentials file (config.toml database.clientdb_conf default).
const DefaultConfPath = "/etc/go-ispconfig/mysql_clientdb.conf"

// Config holds the dedicated client-DB admin credentials (design D3), the
// Go equivalent of ISPConfig3's server/lib/mysql_clientdb.conf. The file
// is TOML and must be mode 0600, owned by root — it contains a privileged
// MySQL password:
//
//	clientdb_host = "localhost"
//	clientdb_port = 3306          # optional, default 3306
//	clientdb_user = "goisp_clientdb"
//	clientdb_password = "secret"
//
// The user needs only client-DB administration privileges (CREATE/DROP
// DATABASE, CREATE USER, GRANT OPTION, read of mysql.user) — never root.
type Config struct {
	Host     string
	Port     int
	User     string
	Password string
}

// LoadConfig reads and validates the credentials file. The password is
// never logged by this package; callers must not log the returned Config
// verbatim.
func LoadConfig(path string) (Config, error) {
	if info, err := os.Stat(path); err == nil && info.Mode().Perm()&0o077 != 0 {
		slog.Warn("clientdb: credentials file should be mode 0600",
			"path", path, "mode", info.Mode().Perm().String())
	}
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("toml")
	v.SetDefault("clientdb_host", "localhost")
	v.SetDefault("clientdb_port", 3306)
	if err := v.ReadInConfig(); err != nil {
		return Config{}, fmt.Errorf("reading client-DB admin config %s: %w", path, err)
	}
	cfg := Config{
		Host:     v.GetString("clientdb_host"),
		Port:     v.GetInt("clientdb_port"),
		User:     v.GetString("clientdb_user"),
		Password: v.GetString("clientdb_password"),
	}
	if cfg.User == "" {
		return Config{}, fmt.Errorf("client-DB admin config %s: clientdb_user is required", path)
	}
	if cfg.Port <= 0 || cfg.Port > 65535 {
		return Config{}, fmt.Errorf("client-DB admin config %s: invalid clientdb_port %d", path, cfg.Port)
	}
	return cfg, nil
}
