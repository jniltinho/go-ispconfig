// Package config defines the application configuration structures and the
// Viper-based loading logic (config.toml + GOISP_* environment variables).
//
// Precedence: environment variables (GOISP_ prefix, e.g. GOISP_SERVER_PORT)
// override file values, which override the built-in defaults.
// File search order: explicit path → ./config.toml → /etc/go-ispconfig/config.toml.
package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// Config is the root configuration object passed throughout the application.
type Config struct {
	Server   ServerConfig   `toml:"server" mapstructure:"server"`
	Database DatabaseConfig `toml:"database" mapstructure:"database"`
	Daemon   DaemonConfig   `toml:"daemon" mapstructure:"daemon"`
	Auth     AuthConfig     `toml:"auth" mapstructure:"auth"`
	Queue    QueueConfig    `toml:"queue" mapstructure:"queue"`
}

// QueueConfig holds the Redis/Valkey connection for the asynq task queue
// (design D12). The queue is always enabled; when Redis is unreachable the
// daemon keeps working through its datalog tick polling and producers
// degrade to warnings — a lost Redis never loses configuration.
type QueueConfig struct {
	// Addr is the Redis/Valkey host:port.
	Addr string `toml:"addr" mapstructure:"addr"`
	// DB is the Redis logical database number.
	DB int `toml:"db" mapstructure:"db"`
	// Password is the Redis AUTH password, empty for none.
	Password string `toml:"password" mapstructure:"password"`
}

// ServerConfig holds HTTP server settings for the panel (serve command).
type ServerConfig struct {
	Host    string `toml:"host" mapstructure:"host"`
	Port    int    `toml:"port" mapstructure:"port"`
	TLSCert string `toml:"tls_cert" mapstructure:"tls_cert"`
	TLSKey  string `toml:"tls_key" mapstructure:"tls_key"`
}

// DatabaseConfig holds the MariaDB/MySQL connection string.
type DatabaseConfig struct {
	DSN string `toml:"dsn" mapstructure:"dsn"`
}

// DaemonConfig controls the config-apply daemon (sys_datalog consumer).
type DaemonConfig struct {
	// TickSeconds is the interval between sys_datalog processing cycles.
	TickSeconds int `toml:"tick_seconds" mapstructure:"tick_seconds"`
	// DatalogRetentionDays is how long processed sys_datalog rows are kept
	// before the daily pruning job removes them.
	DatalogRetentionDays int `toml:"datalog_retention_days" mapstructure:"datalog_retention_days"`
}

// AuthConfig holds authentication behavior toggles.
type AuthConfig struct {
	// RehashLegacy re-hashes legacy ISPConfig crypt password hashes ($6$/$1$)
	// to bcrypt on successful login. Default false: PHP ISPConfig cannot verify
	// bcrypt, so eager rehash would break the migration rollback path (D10).
	RehashLegacy bool `toml:"rehash_legacy" mapstructure:"rehash_legacy"`
}

// setDefaults registers the built-in default for every known key so that
// environment overrides are visible to viper.Unmarshal.
func setDefaults() {
	viper.SetDefault("server.host", "0.0.0.0")
	viper.SetDefault("server.port", 8080)
	viper.SetDefault("server.tls_cert", "")
	viper.SetDefault("server.tls_key", "")
	viper.SetDefault("database.dsn", "root:@tcp(127.0.0.1:3306)/dbispconfig?charset=utf8mb4&parseTime=True&loc=Local")
	viper.SetDefault("daemon.tick_seconds", 10)
	viper.SetDefault("daemon.datalog_retention_days", 30)
	viper.SetDefault("auth.rehash_legacy", false)
	viper.SetDefault("queue.addr", "localhost:6379")
	viper.SetDefault("queue.db", 0)
	viper.SetDefault("queue.password", "")
}

// Init configures the global Viper instance: defaults, GOISP_ environment
// binding, and the config file search path. cfgFile, when non-empty, is used
// verbatim (a missing explicit file is an error); otherwise ./config.toml and
// /etc/go-ispconfig/config.toml are tried and a missing file is not an error.
func Init(cfgFile string) error {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		viper.SetConfigName("config")
		viper.SetConfigType("toml")
		viper.AddConfigPath(".")
		viper.AddConfigPath("/etc/go-ispconfig/")
	}

	viper.SetEnvPrefix("GOISP")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()
	setDefaults()

	if err := viper.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if cfgFile != "" || !errors.As(err, &notFound) {
			return fmt.Errorf("reading config: %w", err)
		}
	}
	return nil
}

// Load unmarshals the current Viper state into a Config.
// Call it after Init (e.g. inside a cobra RunE function).
func Load() (*Config, error) {
	cfg := &Config{}
	if err := viper.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("unmarshaling config: %w", err)
	}
	return cfg, nil
}
