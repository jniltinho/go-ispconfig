package clientdb

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	_ "github.com/go-sql-driver/mysql" // admin connection driver
	"gorm.io/gorm"

	"go-ispconfig/internal/engine"
)

// Denylists (design D8): never create/drop/rename/grant these, matching
// mysql_clientdb_plugin.inc.php. Checked case-insensitively.
var (
	denylistUser     = []string{"root", "debian-sys-maint", "mysql.infoschema"}
	denylistDatabase = []string{"mysql", "information_schema", "performance_schema"}
)

// deniedUser reports whether name is a protected MySQL account.
func deniedUser(name string) bool {
	return slices.Contains(denylistUser, strings.ToLower(name))
}

// deniedDatabase reports whether name is a protected system database.
func deniedDatabase(name string) bool {
	return slices.Contains(denylistDatabase, strings.ToLower(name))
}

// quoteName wraps a schema object name in backticks with embedded
// backticks doubled, for identifier positions (CREATE DATABASE `x`).
func quoteName(s string) string {
	return "`" + strings.ReplaceAll(s, "`", "``") + "`"
}

// quoteStr returns s as a single-quoted MySQL string literal for the
// account-name positions ('user'@'host') that refuse placeholders.
func quoteStr(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `'`, `\'`)
	return "'" + r.Replace(s) + "'"
}

// adminConn is one open connection to the client MariaDB/MySQL server as
// the dedicated admin user; cfg is kept for the mysqldump rename path.
type adminConn struct {
	*sql.DB
	cfg Config
}

// Plugin is the mysql_clientdb plugin (port of
// mysql_clientdb_plugin.inc.php): it consumes the database module events
// and provisions real MySQL/MariaDB databases, users and GRANTs through
// the dedicated client-DB admin connection (design D3).
type Plugin struct {
	db       *gorm.DB // panel DB: web_database / web_database_user lookups
	runner   engine.CommandRunner
	confPath string
	log      *slog.Logger

	// OpenAdmin opens the admin connection; nil means LoadConfig(confPath)
	// over TCP. Integration tests inject a DSN-based opener.
	OpenAdmin func(ctx context.Context) (*sql.DB, Config, error)
}

// NewPlugin creates the mysql_clientdb plugin. confPath empty falls back
// to DefaultConfPath; log nil means slog.Default.
func NewPlugin(db *gorm.DB, runner engine.CommandRunner, confPath string, log *slog.Logger) *Plugin {
	if log == nil {
		log = slog.Default()
	}
	if confPath == "" {
		confPath = DefaultConfPath
	}
	return &Plugin{db: db, runner: runner, confPath: confPath, log: log}
}

// Name identifies the plugin in logs and the registry.
func (*Plugin) Name() string { return "mysql_clientdb" }

// OnLoad subscribes the plugin to the database module events.
// database_user_insert is deliberately not handled (design D7, PHP
// comment: stale user accounts are useless) — users materialise on the
// first grant() via CREATE USER IF NOT EXISTS.
func (p *Plugin) OnLoad(r *engine.Registry) error {
	for event, handler := range p.handlers() {
		h := handler
		if err := r.RegisterEvent(event, func(ctx context.Context, _ string, data engine.Data) error {
			return h(ctx, data)
		}); err != nil {
			return err
		}
	}
	return nil
}

// handlers maps the five handled events to their implementations.
func (p *Plugin) handlers() map[string]func(context.Context, engine.Data) error {
	return map[string]func(context.Context, engine.Data) error{
		"database_insert":      p.dbInsert,
		"database_update":      p.dbUpdate,
		"database_delete":      p.dbDelete,
		"database_user_update": p.dbUserUpdate,
		"database_user_delete": p.dbUserDelete,
	}
}

// connect opens the admin connection. Failures are returned for the
// handlers to log-and-skip: a broken client-DB connection aborts the
// event, never the daemon (design D3).
func (p *Plugin) connect(ctx context.Context) (*adminConn, error) {
	if p.OpenAdmin != nil {
		db, cfg, err := p.OpenAdmin(ctx)
		if err != nil {
			return nil, err
		}
		return &adminConn{DB: db, cfg: cfg}, nil
	}
	cfg, err := LoadConfig(p.confPath)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("mysql", fmt.Sprintf("%s:%s@tcp(%s:%d)/", cfg.User, cfg.Password, cfg.Host, cfg.Port))
	if err != nil {
		return nil, fmt.Errorf("opening client-DB admin connection: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connecting to client-DB %s:%d as %s: %w", cfg.Host, cfg.Port, cfg.User, err)
	}
	return &adminConn{DB: db, cfg: cfg}, nil
}

// connectOr logs a failed connect and reports whether the handler should
// proceed (PHP connect() parity: log error, abort the event silently).
func (p *Plugin) connectOr(ctx context.Context) *adminConn {
	c, err := p.connect(ctx)
	if err != nil {
		p.log.Error("clientdb: unable to connect to client database server", "error", err)
		return nil
	}
	return c
}
