package clientdb

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// invalidPasswordPlaceholder is set when no native hash is available so
// the account can never authenticate (PHP parity).
const invalidPasswordPlaceholder = "*THISISNOTAVALIDPASSWORDTHATCANBEUSEDHERE"

// serverInfo probes the client-DB server flavour and bare version via
// SELECT VERSION() (port of getDatabaseType/getDatabaseVersion; the
// handshake string is unreliable on MariaDB compatibility builds).
func (c *adminConn) serverInfo(ctx context.Context) (dbType, version string) {
	var raw string
	if err := c.QueryRowContext(ctx, "SELECT VERSION()").Scan(&raw); err != nil || raw == "" {
		return "mysql", "0.0.0-unknown"
	}
	dbType = "mysql"
	if strings.Contains(strings.ToLower(raw), "mariadb") {
		dbType = "mariadb"
	}
	version, _, _ = strings.Cut(raw, "-")
	if version == "" {
		version = "0.0.0-unknown"
	}
	return dbType, version
}

// versionLess reports whether dotted version a sorts before b, comparing
// numeric parts (PHP version_compare subset sufficient for 5.7/8.0
// thresholds).
func versionLess(a, b string) bool {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		var an, bn int
		if i < len(as) {
			an, _ = strconv.Atoi(strings.TrimFunc(as[i], func(r rune) bool { return r < '0' || r > '9' }))
		}
		if i < len(bs) {
			bn, _ = strconv.Atoi(strings.TrimFunc(bs[i], func(r rune) bool { return r < '0' || r > '9' }))
		}
		if an != bn {
			return an < bn
		}
	}
	return false
}

// passwordStatement picks the auth plugin and builds the password SQL for
// one user@host from server flavour/version and the stored hashes
// (design D6 decision tree): MariaDB or MySQL < 5.7 use SET PASSWORD
// with the native hash; MySQL ≥ 8.0 with a sha2 hash uses
// caching_sha2_password; otherwise ALTER USER with the native hash.
func passwordStatement(dbType, version, user, host, nativeHash, sha2Hash string) (query, authPlugin string) {
	if nativeHash == "" {
		nativeHash = invalidPasswordPlaceholder
	}
	account := quoteStr(user) + "@" + quoteStr(host)
	if dbType == "mariadb" || versionLess(version, "5.7") {
		return fmt.Sprintf("SET PASSWORD FOR %s = %s", account, quoteStr(nativeHash)),
			"mysql_native_password"
	}
	authPlugin, hash := "mysql_native_password", nativeHash
	if sha2Hash != "" && !versionLess(version, "8.0") {
		authPlugin, hash = "caching_sha2_password", sha2Hash
	}
	return fmt.Sprintf("ALTER USER IF EXISTS %s IDENTIFIED WITH %s AS %s", account, authPlugin, quoteStr(hash)),
		authPlugin
}

// hasPasswordValidation reports whether a strict password validation
// plugin is active — setPassword must refuse then, since stored hashes
// bypass validation (PHP unwanted-plugins guard).
func (p *Plugin) hasPasswordValidation(ctx context.Context, c *adminConn) bool {
	var name string
	err := c.QueryRowContext(ctx,
		"SELECT plugin_name FROM information_schema.plugins WHERE plugin_status='ACTIVE' AND plugin_name IN ('validate_password')").
		Scan(&name)
	if err == nil && name != "" {
		p.log.Error("clientdb: MySQL plugin enabled, cannot set passwords", "plugin", name)
		return true
	}
	return false
}

// setPassword applies the stored hash for user@host with the best
// available auth plugin (port of setPassword). Denylisted users and
// active password validation refuse; failures log and return false.
func (p *Plugin) setPassword(ctx context.Context, c *adminConn, user row, host string) bool {
	if p.hasPasswordValidation(ctx, c) {
		return false
	}
	name := user.str("database_user")
	if deniedUser(name) {
		p.log.Warn("clientdb: refuse to set password", "user", name)
		return false
	}
	dbType, version := c.serverInfo(ctx)
	query, authPlugin := passwordStatement(dbType, version, name, host,
		user.str("database_password"), user.str("database_password_sha2"))
	if _, err := c.ExecContext(ctx, query); err != nil {
		p.log.Warn("clientdb: error setting password", "user", name, "host", host,
			"auth_plugin", authPlugin, "error", err)
		return false
	}
	p.log.Debug("clientdb: password set", "user", name, "host", host, "auth_plugin", authPlugin)
	return true
}
