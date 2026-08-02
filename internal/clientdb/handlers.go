package clientdb

import (
	"context"

	"go-ispconfig/internal/engine"
)

// dbInsert handles database_insert (port of dbInsert): create the
// database and grant its users when active. Non-mysql types are skipped
// before any connection is made.
func (p *Plugin) dbInsert(ctx context.Context, data engine.Data) error {
	if row(data.New).str("type") != "mysql" {
		return nil
	}
	c := p.connectOr(ctx)
	if c == nil {
		return nil
	}
	defer func() { _ = c.Close() }()

	// Provisioning body lands with task 3.6.
	return nil
}

// dbUpdate handles database_update (port of dbUpdate): reconcile
// existence, rename, active flag and user/host grants.
func (p *Plugin) dbUpdate(ctx context.Context, data engine.Data) error {
	if row(data.New).str("type") != "mysql" {
		return nil
	}
	// Skip processing if database was and is inactive.
	if row(data.New).str("active") == "n" && row(data.Old).str("active") == "n" {
		return nil
	}
	c := p.connectOr(ctx)
	if c == nil {
		return nil
	}
	defer func() { _ = c.Close() }()

	// Provisioning body lands with task 3.6.
	return nil
}

// dbDelete handles database_delete (port of dbDelete): revoke users and
// drop the database.
func (p *Plugin) dbDelete(ctx context.Context, data engine.Data) error {
	if row(data.Old).str("type") != "mysql" {
		return nil
	}
	c := p.connectOr(ctx)
	if c == nil {
		return nil
	}
	defer func() { _ = c.Close() }()

	// Provisioning body lands with task 3.6.
	return nil
}

// dbUserUpdate handles database_user_update (port of dbUserUpdate):
// rename the MySQL user and/or set its password across every host of
// every database still referencing it.
func (p *Plugin) dbUserUpdate(ctx context.Context, data engine.Data) error {
	// Nothing to do when username and password are unchanged (empty new
	// password means "leave unchanged", design D6).
	if row(data.Old).str("database_user") == row(data.New).str("database_user") &&
		(row(data.Old).str("database_password") == row(data.New).str("database_password") ||
			row(data.New).str("database_password") == "") {
		return nil
	}
	c := p.connectOr(ctx)
	if c == nil {
		return nil
	}
	defer func() { _ = c.Close() }()

	// Provisioning body lands with task 3.8.
	return nil
}

// dbUserDelete handles database_user_delete (port of dbUserDelete): drop
// the user@host pairs found in mysql.user with Create_user_priv = 'N'.
func (p *Plugin) dbUserDelete(ctx context.Context, data engine.Data) error {
	if deniedUser(row(data.Old).str("database_user")) {
		p.log.Warn("clientdb: refuse to drop user", "user", row(data.Old).str("database_user"))
		return nil
	}
	c := p.connectOr(ctx)
	if c == nil {
		return nil
	}
	defer func() { _ = c.Close() }()

	// Provisioning body lands with task 3.8.
	return nil
}
