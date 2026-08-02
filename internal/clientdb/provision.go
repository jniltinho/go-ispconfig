package clientdb

import (
	"context"
)

// createDatabase creates the client database with its optional default
// charset (port of createDatabase). Denylisted names are refused with a
// warning; failures are logged, never returned — the event keeps going
// (PHP parity).
func (p *Plugin) createDatabase(ctx context.Context, c *adminConn, r row) bool {
	name := r.str("database_name")
	if deniedDatabase(name) {
		p.log.Warn("clientdb: refuse to create database", "database", name)
		return false
	}
	query := "CREATE DATABASE " + quoteName(name)
	if charset := r.str("database_charset"); charset != "" {
		query += " DEFAULT CHARACTER SET " + quoteName(charset)
	}
	if _, err := c.ExecContext(ctx, query); err != nil {
		p.log.Warn("clientdb: unable to create database", "database", name, "error", err)
		return false
	}
	p.log.Debug("clientdb: created MySQL database", "database", name)
	return true
}

// deleteDatabase drops the client database (port of deleteDatabase).
// Denylisted names are refused with a warning.
func (p *Plugin) deleteDatabase(ctx context.Context, c *adminConn, r row) bool {
	name := r.str("database_name")
	if deniedDatabase(name) {
		p.log.Warn("clientdb: refuse to delete database", "database", name)
		return false
	}
	if _, err := c.ExecContext(ctx, "DROP DATABASE "+quoteName(name)); err != nil {
		p.log.Warn("clientdb: error while dropping database", "database", name, "error", err)
		return false
	}
	p.log.Debug("clientdb: dropped MySQL database", "database", name)
	return true
}
