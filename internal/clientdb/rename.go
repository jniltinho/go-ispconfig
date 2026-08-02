package clientdb

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"go-ispconfig/internal/engine"
)

// tableNames lists the tables of a schema by TABLE_TYPE ("BASE TABLE" or
// "VIEW") — port of getTableList over information_schema.
func (c *adminConn) tableNames(ctx context.Context, schema, tableType string) ([]string, error) {
	return c.stringColumn(ctx,
		"SELECT TABLE_NAME FROM information_schema.tables WHERE table_schema = ? AND TABLE_TYPE = ?",
		schema, tableType)
}

// triggerNames lists the trigger names of a schema (port of
// getTriggerList).
func (c *adminConn) triggerNames(ctx context.Context, schema string) ([]string, error) {
	return c.stringColumn(ctx,
		"SELECT TRIGGER_NAME FROM information_schema.triggers WHERE trigger_schema = ?", schema)
}

// stringColumn runs a query returning one string column.
func (c *adminConn) stringColumn(ctx context.Context, query string, args ...any) ([]string, error) {
	rows, err := c.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// schemaExists checks information_schema.SCHEMATA for the schema.
func (c *adminConn) schemaExists(ctx context.Context, name string) bool {
	var found string
	err := c.QueryRowContext(ctx,
		"SELECT SCHEMA_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME = ?", name).Scan(&found)
	return err == nil
}

// clientArgs builds the mysqldump/mysql connection arguments for the
// admin credentials. The password rides on argv exactly like the PHP
// exec_safe port; the dump files themselves are mode 0600.
func (c *adminConn) clientArgs() []string {
	return []string{
		"-h", c.cfg.Host,
		"-P", strconv.Itoa(c.cfg.Port),
		"-u", c.cfg.User,
		"--password=" + c.cfg.Password,
	}
}

// dumpToFile runs mysqldump with extra args writing to a fresh mode-0600
// temp file (via --result-file, keeping warnings out of the dump) and
// returns its path.
func (p *Plugin) dumpToFile(ctx context.Context, c *adminConn, pattern string, extra ...string) (string, error) {
	f, err := os.CreateTemp(p.tempDir, pattern)
	if err != nil {
		return "", err
	}
	_ = f.Close()
	args := append(c.clientArgs(), "--result-file="+f.Name())
	args = append(args, extra...)
	if out, err := p.runner.Run(ctx, "mysqldump", args...); err != nil {
		_ = os.Remove(f.Name())
		return "", fmt.Errorf("mysqldump: %w: %s", err, out)
	}
	return f.Name(), nil
}

// importFile replays a dump file into schema with the stock mysql client
// (source avoids shell redirection).
func (p *Plugin) importFile(ctx context.Context, c *adminConn, schema, path string) error {
	args := append(c.clientArgs(), schema, "-e", "source "+path)
	if out, err := p.runner.Run(ctx, "mysql", args...); err != nil {
		return fmt.Errorf("mysql import: %w: %s", err, out)
	}
	return nil
}

// renameDatabase renames a client database (port of renameDatabase,
// design D9). An empty database is re-created and dropped; otherwise
// triggers/routines/events and views are dumped to mode-0600 temp files,
// base tables move via RENAME TABLE and the dumps are replayed into the
// new schema. Grants are re-applied by the surrounding dbUpdate loop.
func (p *Plugin) renameDatabase(ctx context.Context, c *adminConn, data engine.Data) bool {
	oldName := row(data.Old).str("database_name")
	newName := row(data.New).str("database_name")
	if deniedDatabase(oldName) || deniedDatabase(newName) || strings.EqualFold(oldName, newName) {
		p.log.Warn("clientdb: refuse to rename database", "from", oldName, "to", newName)
		return false
	}

	tables, tErr := c.tableNames(ctx, oldName, "BASE TABLE")
	views, vErr := c.tableNames(ctx, oldName, "VIEW")
	triggers, gErr := c.triggerNames(ctx, oldName)
	if tErr != nil || vErr != nil || gErr != nil {
		p.log.Error("clientdb: unable to rename database", "from", oldName, "to", newName,
			"tables_err", tErr, "views_err", vErr, "triggers_err", gErr)
		return false
	}

	// Empty database: create new + drop old.
	if len(tables) == 0 && len(views) == 0 && len(triggers) == 0 {
		return p.createDatabase(ctx, c, row(data.New)) && p.deleteDatabase(ctx, c, row(data.Old))
	}

	// Save triggers, routines and events.
	var triggerDump string
	if len(triggers) > 0 {
		var err error
		triggerDump, err = p.dumpToFile(ctx, c, "clientdb-*.triggers", oldName, "-d", "-t", "-R", "-E")
		if err != nil {
			triggers = nil
			p.log.Error("clientdb: unable to dump triggers", "database", oldName, "error", err)
		}
	}

	// Save views.
	var viewDump string
	if len(views) > 0 {
		var err error
		viewDump, err = p.dumpToFile(ctx, c, "clientdb-*.views", append([]string{oldName}, views...)...)
		if err != nil {
			views = nil
			p.log.Error("clientdb: unable to dump views", "database", oldName, "error", err)
		}
	}
	removeDumps := func() {
		if triggerDump != "" {
			_ = os.Remove(triggerDump)
		}
		if viewDump != "" {
			_ = os.Remove(viewDump)
		}
	}

	p.createDatabase(ctx, c, row(data.New))
	if !c.schemaExists(ctx, newName) {
		p.log.Error("clientdb: new database missing after create, rename aborted",
			"from", oldName, "to", newName)
		removeDumps()
		return false
	}

	// Drop old triggers before RENAME TABLE (they block the move).
	for _, trigger := range triggers {
		if _, err := c.ExecContext(ctx, "DROP TRIGGER "+quoteName(oldName)+"."+quoteName(trigger)); err != nil {
			p.log.Warn("clientdb: dropping trigger failed", "trigger", trigger, "error", err)
		}
	}

	// Move base tables. A partial move must never drop the old database:
	// the remaining tables would be lost (stricter than PHP, which
	// dropped regardless).
	renameFailed := false
	for _, table := range tables {
		query := "RENAME TABLE " + quoteName(oldName) + "." + quoteName(table) +
			" TO " + quoteName(newName) + "." + quoteName(table)
		if _, err := c.ExecContext(ctx, query); err != nil {
			p.log.Error("clientdb: rename table failed", "table", table, "error", err)
			renameFailed = true
		}
	}
	if renameFailed {
		p.log.Error("clientdb: rename aborted after partial table move, keeping old database",
			"from", oldName, "to", newName)
		removeDumps()
		return false
	}

	// Replay triggers/routines/events and views into the new schema.
	if len(triggers) > 0 {
		if err := p.importFile(ctx, c, newName, triggerDump); err != nil {
			p.log.Error("clientdb: unable to import triggers", "database", newName, "error", err)
		} else {
			_ = os.Remove(triggerDump)
			triggerDump = ""
		}
	}
	if len(views) > 0 {
		if err := p.importFile(ctx, c, newName, viewDump); err != nil {
			p.log.Error("clientdb: unable to import views", "database", newName, "error", err)
		} else {
			_ = os.Remove(viewDump)
			viewDump = ""
		}
	}

	p.deleteDatabase(ctx, c, row(data.Old))
	return true
}
