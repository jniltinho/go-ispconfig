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
	"log/slog"
	"strings"

	"github.com/spf13/viper"
)

// Config is the root configuration object passed throughout the application.
type Config struct {
	Server    ServerConfig    `toml:"server" mapstructure:"server"`
	Database  DatabaseConfig  `toml:"database" mapstructure:"database"`
	Daemon    DaemonConfig    `toml:"daemon" mapstructure:"daemon"`
	Auth      AuthConfig      `toml:"auth" mapstructure:"auth"`
	Queue     QueueConfig     `toml:"queue" mapstructure:"queue"`
	Log       LogConfig       `toml:"log" mapstructure:"log"`
	Templates TemplatesConfig `toml:"templates" mapstructure:"templates"`
	Mail      MailConfig      `toml:"mail" mapstructure:"mail"`
	// PowerDNS holds optional overrides for the PowerDNS gmysql connection
	// when dns_backend=powerdns (default: same host as [database] with
	// database name "powerdns").
	PowerDNS PowerDNSConfig `toml:"powerdns" mapstructure:"powerdns"`
	// ServerID is this node's row in the server table (ISPConfig
	// conf['server_id'] parity). Required on a multi-server installation;
	// 0 means auto-detect by hostname, then by "the single active row".
	ServerID uint32 `toml:"server_id" mapstructure:"server_id"`
}

// PowerDNSConfig is the optional [powerdns] section of config.toml. An empty
// DSN means the daemon builds one from the main MariaDB credentials +
// database "powerdns" (design D3).
type PowerDNSConfig struct {
	// DSN overrides the PowerDNS MariaDB connection (full Go MySQL DSN).
	// Empty: derive from [database].dsn with database name powerdns.
	DSN string `toml:"dsn" mapstructure:"dsn"`
}

// MailConfig is the optional SMTP transport used by the client messaging
// endpoints and welcome emails; an empty SMTPHost disables sending.
type MailConfig struct {
	SMTPHost string `toml:"smtp_host" mapstructure:"smtp_host"`
	SMTPPort int    `toml:"smtp_port" mapstructure:"smtp_port"`
	SMTPUser string `toml:"smtp_user" mapstructure:"smtp_user"`
	SMTPPass string `toml:"smtp_pass" mapstructure:"smtp_pass"`
	// From is the envelope/header sender of panel emails.
	From string `toml:"from" mapstructure:"from"`
}

// TemplatesConfig controls the ".master" template override directory
// (design D6b, conf-custom parity): a file in CustomDir with the same name
// as an embedded template overrides it.
type TemplatesConfig struct {
	// CustomDir is the directory checked before the embedded template set.
	CustomDir string `toml:"custom_dir" mapstructure:"custom_dir"`
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
	Host string `toml:"host" mapstructure:"host"`
	Port int    `toml:"port" mapstructure:"port"`
	// HTTPS enables TLS termination (design D13, default true). When no
	// tls_cert/tls_key pair is configured, serve auto-generates a 10-year
	// self-signed certificate next to the config file. Set to false for
	// plain HTTP (explicit opt-in, no certificate is generated).
	HTTPS   bool   `toml:"https" mapstructure:"https"`
	TLSCert string `toml:"tls_cert" mapstructure:"tls_cert"`
	TLSKey  string `toml:"tls_key" mapstructure:"tls_key"`
	// SwaggerPublic serves /swagger without authentication (dev
	// convenience). Default false: the Swagger UI and spec require an
	// admin session.
	SwaggerPublic bool `toml:"swagger_public" mapstructure:"swagger_public"`
	// TrustedProxies lists CIDRs of reverse proxies allowed to set
	// X-Forwarded-For/X-Forwarded-Proto. Requests arriving from these
	// addresses use the forwarded client IP for login lockout and the
	// forwarded proto for the Secure cookie flag. Empty (default) means
	// headers are ignored and the TCP peer address is used.
	TrustedProxies []string `toml:"trusted_proxies" mapstructure:"trusted_proxies"`
}

// DatabaseConfig holds the MariaDB/MySQL connection string.
type DatabaseConfig struct {
	DSN string `toml:"dsn" mapstructure:"dsn"`
	// ClientDBConf is the path of the client-DB admin credentials file
	// (TOML, mode 0600, ISPConfig mysql_clientdb.conf equivalent) used by
	// the daemon database module to provision client databases.
	ClientDBConf string `toml:"clientdb_conf" mapstructure:"clientdb_conf"`
}

// DaemonConfig controls the config-apply daemon (sys_datalog consumer).
type DaemonConfig struct {
	// TickSeconds is the interval between sys_datalog processing cycles.
	TickSeconds int `toml:"tick_seconds" mapstructure:"tick_seconds"`
	// DatalogRetentionDays is how long processed sys_datalog rows are kept
	// before the daily pruning job removes them.
	DatalogRetentionDays int `toml:"datalog_retention_days" mapstructure:"datalog_retention_days"`
	// DisableMailModule turns off the daemon mail module and plugins on
	// a mail server (spec mail-module-events: config.toml enablement).
	DisableMailModule bool `toml:"disable_mail_module" mapstructure:"disable_mail_module"`
	// DisableFirewallModule turns off the daemon firewall module and
	// UFW plugin even when server.firewall_server = 1 (spec
	// firewall-module-events / design D3: config.toml enablement).
	DisableFirewallModule bool `toml:"disable_firewall_module" mapstructure:"disable_firewall_module"`
	// DisableDatabaseModule turns off the daemon database module and
	// mysql_clientdb plugin even when server.db_server = 1 (spec
	// database-module-events / design D15: config.toml enablement).
	DisableDatabaseModule bool `toml:"disable_database_module" mapstructure:"disable_database_module"`
	// DisableCronModule turns off the daemon cron module and client-job
	// runner even when server.web_server = 1 (spec cron-module-events /
	// design D1: config.toml enablement).
	DisableCronModule bool `toml:"disable_cron_module" mapstructure:"disable_cron_module"`
	// DisableClientEvents turns off the daemon client module (emergency
	// rollback only; client_delete teardown stops firing while set).
	DisableClientEvents bool `toml:"disable_client_events" mapstructure:"disable_client_events"`
}

// LogConfig holds the global logging verbosity.
type LogConfig struct {
	// Level is the slog level: panic|error|warn|info|debug (case-insensitive).
	// Overridable at runtime with LOG_LEVEL (or GOISP_LOG_LEVEL).
	Level string `toml:"level" mapstructure:"level"`
}

// Slog resolves Level into a slog.Level, falling back to info on an unknown
// value. "panic" maps to error: slog has no panic level.
func (l LogConfig) Slog() slog.Level {
	if strings.EqualFold(l.Level, "panic") {
		return slog.LevelError
	}
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(l.Level)); err != nil {
		return slog.LevelInfo
	}
	return lvl
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
	viper.SetDefault("server_id", 0)
	viper.SetDefault("server.host", "0.0.0.0")
	viper.SetDefault("server.port", 8080)
	viper.SetDefault("server.https", true)
	viper.SetDefault("server.tls_cert", "")
	viper.SetDefault("server.tls_key", "")
	viper.SetDefault("server.swagger_public", false)
	viper.SetDefault("server.trusted_proxies", []string{})
	viper.SetDefault("database.dsn", "root:@tcp(127.0.0.1:3306)/dbispconfig?charset=utf8mb4&parseTime=True&loc=Local")
	viper.SetDefault("database.clientdb_conf", "/etc/go-ispconfig/mysql_clientdb.conf")
	viper.SetDefault("daemon.tick_seconds", 10)
	viper.SetDefault("daemon.datalog_retention_days", 30)
	viper.SetDefault("daemon.disable_client_events", false)
	viper.SetDefault("daemon.disable_mail_module", false)
	viper.SetDefault("daemon.disable_firewall_module", false)
	viper.SetDefault("daemon.disable_database_module", false)
	viper.SetDefault("daemon.disable_cron_module", false)
	viper.SetDefault("auth.rehash_legacy", false)
	viper.SetDefault("queue.addr", "localhost:6379")
	viper.SetDefault("queue.db", 0)
	viper.SetDefault("queue.password", "")
	viper.SetDefault("templates.custom_dir", "/etc/go-ispconfig/templates-custom")
	viper.SetDefault("mail.smtp_host", "")
	viper.SetDefault("mail.smtp_port", 25)
	viper.SetDefault("mail.smtp_user", "")
	viper.SetDefault("mail.smtp_pass", "")
	viper.SetDefault("mail.from", "")
	viper.SetDefault("powerdns.dsn", "")
	viper.SetDefault("log.level", "info")
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
	// Bare LOG_LEVEL as well as GOISP_LOG_LEVEL: turning on debug logging is
	// the one knob reached for mid-incident, so it keeps the conventional name.
	if err := viper.BindEnv("log.level", "GOISP_LOG_LEVEL", "LOG_LEVEL"); err != nil {
		return fmt.Errorf("binding LOG_LEVEL: %w", err)
	}
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
