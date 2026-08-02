package clientdb

import (
	"context"
	"slices"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/model"
)

// lookupUser loads one web_database_user panel row as a payload-shaped
// row; nil when id is 0 or the row is missing.
func (p *Plugin) lookupUser(ctx context.Context, id int64) row {
	if id == 0 {
		return nil
	}
	var u model.WebDatabaseUser
	if err := p.db.WithContext(ctx).Take(&u, id).Error; err != nil {
		return nil
	}
	return row{
		"database_user_id":       int64(u.DatabaseUserID),
		"database_user":          u.DatabaseUser,
		"database_password":      u.DatabasePassword,
		"database_password_sha2": u.DatabasePasswordSha2,
	}
}

// flushPrivileges applies a batch of grant/revoke changes.
func (p *Plugin) flushPrivileges(ctx context.Context, c *adminConn) {
	if _, err := c.ExecContext(ctx, "FLUSH PRIVILEGES"); err != nil {
		p.log.Warn("clientdb: flush privileges failed", "error", err)
	}
}

// rwMode returns the full-user grant mode of a database row: rd when the
// quota is exceeded, rw otherwise (design D5).
func rwMode(r row) string {
	if r.str("quota_exceeded") == "y" {
		return "rd"
	}
	return "rw"
}

// dbInsert handles database_insert (port of dbInsert): create the
// database and grant its users when active. Non-mysql types are skipped
// before any connection is made.
func (p *Plugin) dbInsert(ctx context.Context, data engine.Data) error {
	newRow := row(data.New)
	if newRow.str("type") != "mysql" {
		return nil
	}
	c := p.connectOr(ctx)
	if c == nil {
		return nil
	}
	defer func() { _ = c.Close() }()

	p.createDatabase(ctx, c, newRow)

	if newRow.str("active") != "y" {
		return nil
	}
	dbUser := p.lookupUser(ctx, newRow.num("database_user_id"))
	roUser := p.lookupUser(ctx, newRow.num("database_ro_user_id"))
	did := false
	for _, host := range hostList(newRow) {
		if dbUser != nil {
			p.grant(ctx, c, newRow.str("database_name"), dbUser, host, rwMode(newRow))
			did = true
		}
		if roUser != nil && newRow.num("database_user_id") != newRow.num("database_ro_user_id") {
			p.grant(ctx, c, newRow.str("database_name"), roUser, host, "r")
			did = true
		}
	}
	if did {
		p.flushPrivileges(ctx, c)
	}
	return nil
}

// dbUpdate handles database_update (port of dbUpdate): reconcile
// existence, rename, the active flag and the user/host grant matrix.
func (p *Plugin) dbUpdate(ctx context.Context, data engine.Data) error {
	newRow, oldRow := row(data.New), row(data.Old)
	if newRow.str("type") != "mysql" {
		return nil
	}
	// Skip processing if database was and is inactive.
	if newRow.str("active") == "n" && oldRow.str("active") == "n" {
		return nil
	}
	c := p.connectOr(ctx)
	if c == nil {
		return nil
	}
	defer func() { _ = c.Close() }()

	// Recreate a vanished database when the name is unchanged.
	if newRow.str("database_name") == oldRow.str("database_name") &&
		!c.schemaExists(ctx, newRow.str("database_name")) {
		p.createDatabase(ctx, c, newRow)
	}

	// Users referenced before and after the change.
	dbUser := p.lookupUser(ctx, newRow.num("database_user_id"))
	oldDBUser := p.lookupUser(ctx, oldRow.num("database_user_id"))
	roUser := p.lookupUser(ctx, newRow.num("database_ro_user_id"))
	oldROUser := p.lookupUser(ctx, oldRow.num("database_ro_user_id"))

	// All host lists needed upfront.
	newHosts := hostList(newRow)
	oldHosts := hostList(oldRow)
	combined := newHosts
	for _, h := range oldHosts {
		if !slices.Contains(combined, h) {
			combined = append(combined, h)
		}
	}
	otherHosts := map[int64][]string{}
	for _, userID := range []int64{
		oldRow.num("database_user_id"), oldRow.num("database_ro_user_id"),
		newRow.num("database_user_id"), newRow.num("database_ro_user_id"),
	} {
		if userID != 0 && otherHosts[userID] == nil {
			hosts, err := p.otherHostList(ctx, newRow.num("database_id"), userID)
			if err != nil {
				p.log.Warn("clientdb: other-host lookup failed", "user_id", userID, "error", err)
				hosts = []string{}
			}
			otherHosts[userID] = hosts
		}
	}

	if newRow.str("database_name") != oldRow.str("database_name") {
		p.renameDatabase(ctx, c, data)
	}

	// Database just became inactive: revoke everything and stop.
	if newRow.str("active") == "n" {
		did := false
		for _, host := range oldHosts {
			if oldDBUser != nil {
				p.revokeAndDrop(ctx, c, oldRow.str("database_name"), oldDBUser.str("database_user"),
					host, !slices.Contains(otherHosts[oldDBUser.num("database_user_id")], host))
				did = true
			}
			if oldROUser != nil && oldRow.num("database_user_id") != oldRow.num("database_ro_user_id") {
				p.revokeAndDrop(ctx, c, oldRow.str("database_name"), oldROUser.str("database_user"),
					host, !slices.Contains(otherHosts[oldROUser.num("database_user_id")], host))
				did = true
			}
		}
		if did {
			p.flushPrivileges(ctx, c)
		}
		return nil
	}

	// Reconcile the grant matrix per host (GRANT new users, REVOKE
	// replaced users and removed hosts).
	did := false
	for _, host := range combined {
		inOld := slices.Contains(oldHosts, host)
		inNew := slices.Contains(newHosts, host)
		var revoked []int64
		if inNew && dbUser != nil {
			p.grant(ctx, c, newRow.str("database_name"), dbUser, host, rwMode(newRow))
			did = true
		}
		if inNew && roUser != nil && newRow.num("database_user_id") != newRow.num("database_ro_user_id") {
			p.grant(ctx, c, newRow.str("database_name"), roUser, host, "r")
			did = true
		}
		// User changed: revoke the old one (unless it became the RO user).
		if inOld && newRow.num("database_user_id") != oldRow.num("database_user_id") &&
			oldDBUser != nil && oldRow.num("database_user_id") != newRow.num("database_ro_user_id") {
			p.revokeAndDrop(ctx, c, oldRow.str("database_name"), oldDBUser.str("database_user"),
				host, !slices.Contains(otherHosts[oldDBUser.num("database_user_id")], host))
			revoked = append(revoked, oldDBUser.num("database_user_id"))
			did = true
		}
		// RO user changed: revoke the old one (unless it became the rw user).
		if inOld && newRow.num("database_ro_user_id") != oldRow.num("database_ro_user_id") &&
			oldROUser != nil && oldRow.num("database_ro_user_id") != newRow.num("database_user_id") {
			p.revokeAndDrop(ctx, c, oldRow.str("database_name"), oldROUser.str("database_user"),
				host, !slices.Contains(otherHosts[oldROUser.num("database_user_id")], host))
			revoked = append(revoked, oldROUser.num("database_user_id"))
			did = true
		}
		// Host removed: revoke both users for it.
		if inOld && !inNew {
			if oldDBUser != nil && !slices.Contains(revoked, oldDBUser.num("database_user_id")) {
				p.revokeAndDrop(ctx, c, oldRow.str("database_name"), oldDBUser.str("database_user"),
					host, !slices.Contains(otherHosts[oldDBUser.num("database_user_id")], host))
				did = true
			}
			if oldROUser != nil && !slices.Contains(revoked, oldROUser.num("database_user_id")) {
				p.revokeAndDrop(ctx, c, oldRow.str("database_name"), oldROUser.str("database_user"),
					host, !slices.Contains(otherHosts[oldROUser.num("database_user_id")], host))
				did = true
			}
		}
	}
	if did {
		p.flushPrivileges(ctx, c)
	}
	return nil
}

// dbDelete handles database_delete (port of dbDelete): revoke both users
// across the old host list (dropping accounts no other active database
// needs) and drop the database.
func (p *Plugin) dbDelete(ctx context.Context, data engine.Data) error {
	oldRow := row(data.Old)
	if oldRow.str("type") != "mysql" {
		return nil
	}
	c := p.connectOr(ctx)
	if c == nil {
		return nil
	}
	defer func() { _ = c.Close() }()

	oldHosts := hostList(oldRow)
	did := false
	revoke := func(userID int64) {
		user := p.lookupUser(ctx, userID)
		if user == nil {
			return
		}
		otherHosts, err := p.otherHostList(ctx, oldRow.num("database_id"), userID)
		if err != nil {
			p.log.Warn("clientdb: other-host lookup failed", "user_id", userID, "error", err)
			otherHosts = []string{}
		}
		for _, host := range oldHosts {
			p.revokeAndDrop(ctx, c, oldRow.str("database_name"), user.str("database_user"),
				host, !slices.Contains(otherHosts, host))
			did = true
		}
	}
	if id := oldRow.num("database_user_id"); id != 0 {
		revoke(id)
	}
	if id := oldRow.num("database_ro_user_id"); id != 0 && id != oldRow.num("database_user_id") {
		revoke(id)
	}
	if did {
		p.flushPrivileges(ctx, c)
	}

	p.deleteDatabase(ctx, c, oldRow)
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
