package api

// This file holds the sites database module domain logic (openspec change
// add-database-module, lote D): the Go port of tools_sites prefix
// handling, database_edit.php / database_user_edit.php validation and the
// sites_database_plugin hooks, feeding the /api/sites/databases and
// /api/sites/database-users entities.

import (
	"context"
	"fmt"
	"net/netip"
	"regexp"
	"strconv"
	"strings"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/labstack/echo/v5"
	"gorm.io/gorm"

	"go-ispconfig/internal/clientdb"
	"go-ispconfig/internal/getconf"
	"go-ispconfig/internal/model"
	"go-ispconfig/internal/repository"
	"go-ispconfig/internal/validator"
)

// mysqlDatabaseNameMax and mysqlUserNameMax are the crop limits of
// database_edit.php / database_user_edit.php (mysql: db 64, user 32).
const (
	mysqlDatabaseNameMax = 64
	mysqlUserNameMax     = 32
)

// expandPrefixPlaceholders ports tools_sites::replacePrefix — the
// [CLIENTNAME], [CLIENTID] and [DOMAINID] placeholders of the getconf
// sites dbname_prefix/dbuser_prefix templates. Unresolvable values keep
// their literal placeholder (PHP parity): pass "[CLIENTNAME]"/
// "[CLIENTID]" through, and domainID <= 0 keeps [DOMAINID].
func expandPrefixPlaceholders(tpl, clientName, clientID string, domainID int64) string {
	if tpl == "" {
		return ""
	}
	domain := "[DOMAINID]"
	if domainID > 0 {
		domain = strconv.FormatInt(domainID, 10)
	}
	return strings.NewReplacer(
		"[CLIENTNAME]", clientName,
		"[CLIENTID]", clientID,
		"[DOMAINID]", domain,
	).Replace(tpl)
}

// keepStoredPrefix ports tools_sites::getPrefix for the API flow: an
// existing record keeps the prefix it was created with; '#' (legacy
// marker for "no prefix recorded yet") falls back to the expanded
// template value.
func keepStoredPrefix(stored, expanded string) string {
	if stored != "#" {
		return stored
	}
	return expanded
}

// cropName ports the database_edit/database_user_edit substr crop:
// prefix+name truncated to the MySQL identifier limit.
func cropName(s string, maxLen int) string {
	if len(s) > maxLen {
		return s[:maxLen]
	}
	return s
}

// sitesGlobalConfig loads the getconf global [sites] section (nil-safe:
// missing sys_ini or section yields an empty map, PHP parity where every
// key reads as "").
//
//nolint:unused // wired into the databases entity by task 4.6
func sitesGlobalConfig(db *gorm.DB) map[string]string {
	sections, err := getconf.GetGlobalConfig(db)
	if err != nil || sections["sites"] == nil {
		return map[string]string{}
	}
	return sections["sites"]
}

// panelDBName extracts the panel's own database name from the configured
// DSN (the PHP $conf['db_database'] blacklist input).
//
//nolint:unused // wired into the databases entity by task 4.6
func panelDBName(d *Deps) string {
	if d.Config == nil {
		return ""
	}
	cfg, err := mysqldriver.ParseDSN(d.Config.Database.DSN)
	if err != nil {
		return ""
	}
	return cfg.DBName
}

// --- database validators (task 4.3, port of database.tform.php rules and
// the database_edit.php onBeforeInsert/onBeforeUpdate checks) ---

// databaseNameRules are the declarative rules of the full database name
// (prefix already applied by the Prepare hook).
func databaseNameRules() []validator.Rule {
	return []validator.Rule{
		{Type: "NOTEMPTY", ErrKey: "database_name_error_empty"},
		{Type: "REGEX", Regex: `^[a-zA-Z0-9_]{2,64}$`, ErrKey: "database_name_error_regex"},
	}
}

// databaseCharsetOptions is the charset SELECT of database.tform.php.
//
//nolint:unused // wired into the databases entity by task 4.6
func databaseCharsetOptions() []Option {
	return []Option{
		{Value: "", Label: "DB-Default"},
		{Value: "latin1", Label: "Latin 1"},
		{Value: "utf8", Label: "UTF-8"},
		{Value: "utf8mb4", Label: "UTF8MB4"},
	}
}

// checkDatabaseCharset restricts database_charset to the tform SELECT set
// (the engine renders options but does not enforce them).
func checkDatabaseCharset(_ *validator.Context, value string) string {
	switch value {
	case "", "latin1", "utf8", "utf8mb4":
		return ""
	default:
		return "database_charset_error_regex"
	}
}

// hostnameRe is the FILTER_VALIDATE_DOMAIN/HOSTNAME approximation used by
// the remote_ips validator (PHP accepts hostnames besides IPs).
var hostnameRe = regexp.MustCompile(`^(?i)([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)*[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// checkRemoteIPList ports validate_database::valid_ip_list: an empty list
// is fine, otherwise every comma-separated value must be an IP address or
// a hostname. (The daemon-side host list keeps only real IPs; hostnames
// are accepted for parity but ignored by grants.)
func checkRemoteIPList(_ *validator.Context, value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	for entry := range strings.SplitSeq(value, ",") {
		entry = strings.TrimSpace(entry)
		if _, err := netip.ParseAddr(entry); err == nil {
			continue
		}
		if entry == "" || !hostnameRe.MatchString(entry) {
			return "database_remote_error_ips"
		}
	}
	return ""
}

// databaseNameBlacklisted ports the database_edit.php blacklist: the
// panel's own database and 'mysql' are never valid client DB names.
func databaseNameBlacklisted(name, panelDBName string) bool {
	return name == "mysql" || (panelDBName != "" && name == panelDBName)
}

// --- database-user validators (task 4.4, port of
// database_user.tform.php and database_user_edit.php) ---

// databaseUserRules are the declarative rules of the full database user
// name (prefix already applied by the Prepare hook). MySQL's effective
// user length is capped by the 32-char crop; the tform regex allows up
// to 64.
//
//nolint:unused // wired into the database-users entity by task 4.6
func databaseUserRules() []validator.Rule {
	return []validator.Rule{
		{Type: "NOTEMPTY", ErrKey: "database_user_error_empty"},
		{Type: "UNIQUE", ErrKey: "database_user_error_unique"},
		{Type: "REGEX", Regex: `^[a-zA-Z0-9_]{2,64}$`, ErrKey: "database_user_error_regex"},
	}
}

// passwordStrength ports validate_password::_get_password_strength: a
// 1–5 score from length and character-class diversity.
func passwordStrength(password string) int {
	length := len(password)
	if length < 5 {
		return 1
	}
	points, different := 0, 0
	if regexp.MustCompile(`[a-z]`).MatchString(password) {
		different++
	}
	if regexp.MustCompile(`[A-Z]`).MatchString(password) {
		points++
		different++
	}
	if regexp.MustCompile(`[0-9]`).MatchString(password) {
		points++
		different++
	}
	if regexp.MustCompile("[`~!@#$%^&*()_+|\\\\=\\-\\[\\]}{';:/?.>,<\" ]").MatchString(password) {
		points++
		different++
	}
	switch {
	case points == 0 || different < 3:
		switch {
		case length <= 6:
			return 1
		case length <= 8:
			return 2
		default:
			return 3
		}
	case points == 1:
		switch {
		case length <= 6:
			return 2
		case length <= 10:
			return 3
		default:
			return 4
		}
	case points == 2:
		switch {
		case length <= 8:
			return 3
		case length <= 10:
			return 4
		default:
			return 5
		}
	case points == 3:
		switch {
		case length <= 6:
			return 3
		case length <= 8:
			return 4
		default:
			return 5
		}
	default: // points >= 4
		if length <= 6 {
			return 4
		}
		return 5
	}
}

// checkPasswordPolicy ports validate_password::password_check against
// the getconf global [misc] policy (min_password_length default 8,
// min_password_strength default 0). Empty passwords pass — emptiness is
// the create path's job (update means "unchanged").
func checkPasswordPolicy(db *gorm.DB, password string) string {
	if password == "" {
		return ""
	}
	minLength, minStrength := 8, 0
	if db != nil {
		if sections, err := getconf.GetGlobalConfig(db); err == nil {
			if v, err := strconv.Atoi(sections["misc"]["min_password_length"]); err == nil {
				minLength = v
			}
			if v, err := strconv.Atoi(sections["misc"]["min_password_strength"]); err == nil {
				minStrength = v
			}
		}
	}
	if len(password) < minLength || passwordStrength(password) < minStrength {
		return "weak_password_txt"
	}
	return ""
}

// sitesDatabaseUserPrepare is the Prepare hook of the database-users
// entity (port of database_user_edit.php): prefix application with the
// 32-char crop, blacklist (root/mysql/panel DB user), password policy on
// the submitted plaintext and the dual-hash store (design D6) — native +
// caching_sha2; an empty password on update leaves the hashes untouched.
//
//nolint:unused // wired into the database-users entity by task 4.6
func sitesDatabaseUserPrepare(c *echo.Context, d *Deps, id *repository.Identity, body map[string]any) error {
	ctx := c.Request().Context()
	fields := map[string][]string{}
	create := c.Param("id") == ""

	global := sitesGlobalConfig(d.DB)
	expandedPrefix := expandSitesPrefix(ctx, d.DB, id, global["dbuser_prefix"], body)
	prefix := expandedPrefix
	if !create {
		var old model.WebDatabaseUser
		if err := d.DB.WithContext(ctx).Take(&old, c.Param("id")).Error; err != nil {
			return err
		}
		prefix = keepStoredPrefix(old.DatabaseUserPrefix, expandedPrefix)
	}

	if suffix, ok := body["database_user"].(string); ok {
		full := cropName(prefix+suffix, mysqlUserNameMax)
		body["database_user"] = full
		body["database_user_prefix"] = prefix
		if databaseUserBlacklisted(full, panelDBUser(d)) {
			fields["database_user"] = append(fields["database_user"], "database_user_error_blacklist")
		}
	}

	// server_id 0: a database user exists on every server until a
	// database binds it (PHP: "we need this on all servers").
	if create {
		body["server_id"] = float64(0)
	}

	pw, _ := body["database_password"].(string)
	switch {
	case pw == "" && create:
		fields["database_password"] = append(fields["database_password"], "database_password_error_empty")
	case pw == "":
		// Empty on update: leave the stored hashes untouched.
		delete(body, "database_password")
		delete(body, "database_password_sha2")
	default:
		if key := checkPasswordPolicy(d.DB, pw); key != "" {
			fields["database_password"] = append(fields["database_password"], key)
		} else {
			sha2, err := clientdb.Sha2PasswordHash(pw)
			if err != nil {
				return err
			}
			body["database_password"] = clientdb.NativePasswordHash(pw)
			body["database_password_sha2"] = sha2
		}
	}

	if len(fields) > 0 {
		return &ValidationError{Fields: fields}
	}
	return nil
}

// databaseUserBlacklisted ports the database_user_edit.php blacklist:
// root, mysql and the panel's own DB user are never valid client users.
func databaseUserBlacklisted(name, panelUser string) bool {
	return name == "root" || name == "mysql" || (panelUser != "" && name == panelUser)
}

// panelDBUser extracts the panel's own database user from the configured
// DSN (the PHP $conf['db_user'] blacklist input).
//
//nolint:unused // wired into the database-users entity by task 4.6
func panelDBUser(d *Deps) string {
	if d.Config == nil {
		return ""
	}
	cfg, err := mysqldriver.ParseDSN(d.Config.Database.DSN)
	if err != nil {
		return ""
	}
	return cfg.User
}

// sitesDatabasePrepare is the Prepare hook of the databases entity: it
// ports database_edit.php onSubmit/onBeforeInsert/onBeforeUpdate —
// parent-site ownership and vhost check, prefix application with the
// 64-char crop, blacklist, per-server uniqueness and the update
// immutability guards (server_id, charset, non-admin rename).
//
//nolint:unused // wired into the databases entity by task 4.6
func sitesDatabasePrepare(c *echo.Context, d *Deps, id *repository.Identity, body map[string]any) error {
	ctx := c.Request().Context()
	fields := map[string][]string{}

	// Only MySQL databases are accepted (design non-goal: postgres).
	if t, ok := body["type"].(string); ok && t != "mysql" {
		fields["type"] = append(fields["type"], "database_type_error")
	}

	// Parent site is required, must be readable by the caller and a vhost.
	pid := bodyInt(body, "parent_domain_id")
	var parent *model.WebDomain
	if pid <= 0 {
		fields["parent_domain_id"] = append(fields["parent_domain_id"], "database_site_error_empty")
	} else {
		var err error
		parent, err = loadOwned[model.WebDomain](c, d, id, pid)
		if err != nil {
			return err
		}
		if parent.Type != "vhost" {
			fields["parent_domain_id"] = append(fields["parent_domain_id"], "parent_domain_id_error_invalid")
		}
	}

	global := sitesGlobalConfig(d.DB)
	expandedPrefix := expandSitesPrefix(ctx, d.DB, id, global["dbname_prefix"], body)

	var old *model.WebDatabase
	if idParam := c.Param("id"); idParam != "" {
		old = &model.WebDatabase{}
		if err := d.DB.WithContext(ctx).Take(old, idParam).Error; err != nil {
			return err
		}
		// server_id and charset are immutable once created (PHP restores
		// the server and errors on charset change).
		if v := bodyInt(body, "server_id"); v != 0 && uint32(v) != old.ServerID {
			fields["server_id"] = append(fields["server_id"], "database_server_change_error")
		}
		body["server_id"] = float64(old.ServerID)
		if v, ok := body["database_charset"].(string); ok && v != old.DatabaseCharset {
			fields["database_charset"] = append(fields["database_charset"], "database_charset_change_error")
		}
		body["database_charset"] = old.DatabaseCharset
	}

	// Apply the name prefix and crop (create: expanded template; update:
	// the prefix the record was created with).
	prefix := expandedPrefix
	if old != nil {
		prefix = keepStoredPrefix(old.DatabaseNamePrefix, expandedPrefix)
	}
	if suffix, ok := body["database_name"].(string); ok {
		full := cropName(prefix+suffix, mysqlDatabaseNameMax)
		body["database_name"] = full
		body["database_name_prefix"] = prefix

		// Only admin may rename an existing database.
		if old != nil && old.DatabaseName != full && (id == nil || !id.IsAdmin()) {
			fields["database_name"] = append(fields["database_name"], "database_name_change_error")
		}
		if databaseNameBlacklisted(full, panelDBName(d)) {
			fields["database_name"] = append(fields["database_name"], "database_name_error_blacklist")
		}
		// Unique per server (the plain UNIQUE rule cannot scope on
		// server_id).
		q := d.DB.WithContext(ctx).Table("web_database").
			Where("database_name = ? AND server_id = ?", full, bodyInt(body, "server_id"))
		if old != nil {
			q = q.Where("database_id != ?", old.DatabaseID)
		}
		var n int64
		if err := q.Count(&n).Error; err != nil {
			return fmt.Errorf("api: database name uniqueness check: %w", err)
		}
		if n > 0 {
			fields["database_name"] = append(fields["database_name"], "database_name_error_unique")
		}
	}

	if len(fields) > 0 {
		return &ValidationError{Fields: fields}
	}
	return nil
}

// expandSitesPrefix resolves the placeholder inputs of one prefix
// template for the requesting identity (port of the tools_sites
// getClientName/getClientID resolution): non-admin identities use their
// default group; admins resolve the owning group through the submitted
// parent_domain_id (falling back to the literal placeholders when
// nothing resolves, PHP parity).
//
//nolint:unused // wired into the entity Prepare hooks by task 4.6
func expandSitesPrefix(ctx context.Context, db *gorm.DB, id *repository.Identity, tpl string, body map[string]any) string {
	if tpl == "" {
		return ""
	}
	domainID := bodyInt(body, "parent_domain_id")

	var groupID uint32
	if id != nil && !id.IsAdmin() {
		groupID = id.DefaultGroup
	} else if domainID > 0 {
		var web model.WebDomain
		if err := db.WithContext(ctx).Select("sys_groupid").
			Where("domain_id = ?", domainID).Take(&web).Error; err == nil {
			groupID = web.SysGroupID
		}
	}

	clientName, clientID := "[CLIENTNAME]", "[CLIENTID]"
	if groupID != 0 {
		var group model.SysGroup
		if err := db.WithContext(ctx).Select("name, client_id").
			Where("groupid = ?", groupID).Take(&group).Error; err == nil {
			clientName = group.Name
			clientID = strconv.FormatUint(uint64(group.ClientID), 10)
		}
	}
	return expandPrefixPlaceholders(tpl, clientName, clientID, domainID)
}
