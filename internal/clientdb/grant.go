package clientdb

import (
	"context"
)

// grantPrivileges maps the design-D5 access modes to their GRANT lists:
// rw = full user on an active DB, rd = quota-exceeded full user,
// r = read-only user.
var grantPrivileges = map[string]string{
	"rw": "ALL PRIVILEGES",
	"rd": "SELECT, DELETE, ALTER, DROP",
	"r":  "SELECT",
}

// grant creates user@host if needed, applies its stored password hash
// and grants the mode's privileges on the database (port of grant).
// Restrictive modes first REVOKE ALL so leftover wider grants never
// survive (PHP parity); caller flushes privileges after the batch.
func (p *Plugin) grant(ctx context.Context, c *adminConn, databaseName string, user row, host, mode string) bool {
	name := user.str("database_user")
	if deniedUser(name) {
		p.log.Warn("clientdb: refuse to grant user", "user", name)
		return false
	}
	account := quoteStr(name) + "@" + quoteStr(host)
	target := quoteName(databaseName) + ".*"

	if mode == "r" || mode == "rd" {
		if _, err := c.ExecContext(ctx, "REVOKE ALL PRIVILEGES ON "+target+" FROM "+account); err != nil {
			// Ignored: the account may simply hold no grants yet.
			p.log.Debug("clientdb: pre-grant revoke skipped", "user", name, "host", host, "error", err)
		}
	}

	// Ignore errors: the account might already exist.
	if _, err := c.ExecContext(ctx, "CREATE USER IF NOT EXISTS "+account); err != nil {
		p.log.Debug("clientdb: create user skipped", "user", name, "host", host, "error", err)
	}

	if !p.setPassword(ctx, c, user, host) {
		return false
	}

	grants, ok := grantPrivileges[mode]
	if !ok {
		p.log.Warn("clientdb: unknown grant mode", "mode", mode, "user", name)
		return false
	}
	if _, err := c.ExecContext(ctx, "GRANT "+grants+" ON "+target+" TO "+account); err != nil {
		p.log.Warn("clientdb: error granting privileges", "grants", grants,
			"database", databaseName, "user", name, "host", host, "error", err)
		return false
	}
	p.log.Debug("clientdb: granted privileges", "grants", grants,
		"database", databaseName, "user", name, "host", host)
	return true
}

// revokeAndDrop revokes all privileges on the database from user@host
// and, when drop is set (no other active database needs the account),
// drops it (port of revokeAndDrop).
func (p *Plugin) revokeAndDrop(ctx context.Context, c *adminConn, databaseName, userName, host string, drop bool) bool {
	if deniedUser(userName) {
		p.log.Warn("clientdb: refuse to revoke/drop user", "user", userName)
		return false
	}
	account := quoteStr(userName) + "@" + quoteStr(host)
	_, err := c.ExecContext(ctx, "REVOKE ALL PRIVILEGES ON "+quoteName(databaseName)+".* FROM "+account)
	p.log.Debug("clientdb: revoked privileges", "database", databaseName,
		"user", userName, "host", host, "ok", err == nil)
	if drop {
		_, err = c.ExecContext(ctx, "DROP USER "+account)
		p.log.Debug("clientdb: dropped user", "user", userName, "host", host, "ok", err == nil)
	}
	return err == nil
}
