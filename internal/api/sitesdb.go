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
	"slices"
	"strconv"
	"strings"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/labstack/echo/v5"
	"gorm.io/gorm"

	"go-ispconfig/internal/clientdb"
	"go-ispconfig/internal/clients"
	"go-ispconfig/internal/datalog"
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
func sitesGlobalConfig(db *gorm.DB) map[string]string {
	sections, err := getconf.GetGlobalConfig(db)
	if err != nil || sections["sites"] == nil {
		return map[string]string{}
	}
	return sections["sites"]
}

// panelDBName extracts the panel's own database name from the configured
// DSN (the PHP $conf['db_database'] blacklist input).
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

	// Admin creates without a server fall back to the configured
	// default_dbserver (database_edit.php onShowNew).
	if old == nil && bodyInt(body, "server_id") == 0 {
		if def, err := strconv.ParseInt(global["default_dbserver"], 10, 64); err == nil && def > 0 {
			body["server_id"] = float64(def)
		}
	}

	// Clients lose the remote-access controls when the panel disables
	// them (D10; admin may always set remote access).
	if global["disable_client_remote_dbserver"] == "y" && (id == nil || !id.IsAdmin()) {
		delete(body, "remote_access")
		delete(body, "remote_ips")
	}

	mergeRemoteAccess(d.DB, body, parent, old, global)

	// Quota re-check on update (the create limit hook does not fire
	// there; PHP database_edit onSubmit update path).
	if old != nil {
		if _, ok := body["database_quota"]; ok {
			err := clients.CheckDatabaseQuotaUpdate(ctx, d.DB, id,
				bodyInt(body, "database_quota"), int64(old.DatabaseID))
			if err != nil {
				return err
			}
		}
	}

	if len(fields) > 0 {
		return &ValidationError{Fields: fields}
	}
	return nil
}

// mergeRemoteAccess ports the database_edit.php web-server-IP merge
// (design D11): when the parent site lives on another server, remote
// access is force-enabled and the web server's IP plus the
// default_remote_dbserver list land in remote_ips; otherwise a
// configured default list only applies when remote access was off.
// remote_access=y with an empty list stays empty (wildcard '%', PHP
// parity). old carries the stored record on updates so the stored
// remote flags are consulted when the body omits them.
func mergeRemoteAccess(db *gorm.DB, body map[string]any, parent *model.WebDomain, old *model.WebDatabase, global map[string]string) {
	remoteAccess, hasAccess := body["remote_access"].(string)
	if !hasAccess && old != nil {
		remoteAccess = old.RemoteAccess
	}
	remoteIPs, hasIPs := body["remote_ips"].(string)
	if !hasIPs && old != nil {
		remoteIPs = old.RemoteIps
	}
	defaults := splitIPList(global["default_remote_dbserver"])

	dbServer := bodyInt(body, "server_id")
	if parent != nil && parent.ServerID != 0 && int64(parent.ServerID) != dbServer {
		webIP := ""
		if cfg, err := getconf.GetServerConfig(db, parent.ServerID); err == nil && cfg.Raw["server"] != nil {
			webIP = cfg.Raw["server"]["ip_address"]
		}
		if webIP != "" && !slices.Contains(defaults, webIP) {
			defaults = append(defaults, webIP)
		}
		if webIP != "" {
			if remoteAccess != "y" {
				body["remote_ips"] = strings.Join(defaults, ",")
				body["remote_access"] = "y"
			} else if remoteIPs != "" {
				merged := splitIPList(remoteIPs)
				for _, ip := range defaults {
					if !slices.Contains(merged, ip) {
						merged = append(merged, ip)
					}
				}
				body["remote_ips"] = strings.Join(merged, ",")
			}
		}
		return
	}
	if len(defaults) > 0 && remoteAccess != "y" {
		body["remote_ips"] = strings.Join(defaults, ",")
		body["remote_access"] = "y"
	}
}

// splitIPList splits a comma-separated list, trimming entries and
// dropping empties.
func splitIPList(s string) []string {
	var out []string
	for entry := range strings.SplitSeq(s, ",") {
		if entry = strings.TrimSpace(entry); entry != "" {
			out = append(out, entry)
		}
	}
	return out
}

// expandSitesPrefix resolves the placeholder inputs of one prefix
// template for the requesting identity (port of the tools_sites
// getClientName/getClientID resolution): non-admin identities use their
// default group; admins resolve the owning group through the submitted
// parent_domain_id (falling back to the literal placeholders when
// nothing resolves, PHP parity).
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

// --- entity hooks (task 4.6, sites_database_plugin + remote API parity) ---

// fanoutDatabaseUserJournal writes one web_database_user UPDATE datalog
// row with server_id overridden to serverID (PHP "force update of the
// used database user"): the daemon of that server re-reconciles the
// user. No row is written when the user is missing or nothing differs.
func fanoutDatabaseUserJournal(tx *gorm.DB, userID *uint32, serverID uint32, username string) error {
	if userID == nil || *userID == 0 {
		return nil
	}
	var u model.WebDatabaseUser
	if err := tx.Take(&u, *userID).Error; err != nil {
		return nil // dangling reference: nothing to journal
	}
	fanned := u
	fanned.ServerID = serverID
	return datalog.LogUpdate(tx, &u, &fanned, username)
}

// sitesDatabaseSyncParent ports sites_database_plugin
// processDatabaseUpdate: the database is owned by the parent site's
// group and inherits its backup_copies; the referenced users get a
// journal fan-out targeting the database's server.
func sitesDatabaseSyncParent(tx *gorm.DB, id *repository.Identity, rec *model.WebDatabase) error {
	if rec.ParentDomainID > 0 {
		var web model.WebDomain
		if err := tx.Where("domain_id = ?", rec.ParentDomainID).Take(&web).Error; err != nil {
			return fmt.Errorf("api: loading parent domain %d: %w", rec.ParentDomainID, err)
		}
		rec.SysGroupID = web.SysGroupID
		rec.BackupCopies = web.BackupCopies
		if err := tx.Model(rec).Select("sys_groupid", "backup_copies").Updates(rec).Error; err != nil {
			return err
		}
	}
	username := ""
	if id != nil {
		username = id.Username
	}
	if err := fanoutDatabaseUserJournal(tx, rec.DatabaseUserID, rec.ServerID, username); err != nil {
		return err
	}
	return fanoutDatabaseUserJournal(tx, rec.DatabaseROUserID, rec.ServerID, username)
}

// sitesDatabaseAfterInsert runs inside the create transaction (before
// the datalog insert row) so the inherited sys_groupid/backup_copies
// land in the journal too.
func sitesDatabaseAfterInsert(_ context.Context, tx *gorm.DB, id *repository.Identity, recAny any, _ map[string]any) error {
	return sitesDatabaseSyncParent(tx, id, recAny.(*model.WebDatabase))
}

// sitesDatabaseBeforeUpdate mirrors the insert-side parent sync on
// updates (runs before change detection: the diff carries the values).
func sitesDatabaseBeforeUpdate(_ context.Context, tx *gorm.DB, id *repository.Identity, _ map[string]any, _, recAny any) error {
	rec := recAny.(*model.WebDatabase)
	if rec.ParentDomainID > 0 {
		var web model.WebDomain
		if err := tx.Where("domain_id = ?", rec.ParentDomainID).Take(&web).Error; err != nil {
			return fmt.Errorf("api: loading parent domain %d: %w", rec.ParentDomainID, err)
		}
		rec.SysGroupID = web.SysGroupID
		rec.BackupCopies = web.BackupCopies
	}
	username := ""
	if id != nil {
		username = id.Username
	}
	if err := fanoutDatabaseUserJournal(tx, rec.DatabaseUserID, rec.ServerID, username); err != nil {
		return err
	}
	return fanoutDatabaseUserJournal(tx, rec.DatabaseROUserID, rec.ServerID, username)
}

// sitesDatabaseUserBeforeUpdate fans the user update out per distinct
// server_id of the databases still referencing it (PHP
// sites_database_user_update remote API parity).
func sitesDatabaseUserBeforeUpdate(_ context.Context, tx *gorm.DB, id *repository.Identity, _ map[string]any, oldAny, recAny any) error {
	old := oldAny.(*model.WebDatabaseUser)
	rec := recAny.(*model.WebDatabaseUser)
	var serverIDs []uint32
	err := tx.Table("web_database").Distinct("server_id").
		Where("database_user_id = ? OR database_ro_user_id = ?", old.DatabaseUserID, old.DatabaseUserID).
		Pluck("server_id", &serverIDs)
	if err.Error != nil {
		return err.Error
	}
	username := ""
	if id != nil {
		username = id.Username
	}
	for _, sid := range serverIDs {
		fanned := *rec
		fanned.ServerID = sid
		if logErr := datalog.LogUpdate(tx, old, &fanned, username); logErr != nil {
			return logErr
		}
	}
	return nil
}

// sitesDatabaseUserBeforeDelete nulls the FK references of every
// database using the deleted user, with journaled updates so the daemon
// revokes the grants (PHP sites_database_user_delete parity).
func sitesDatabaseUserBeforeDelete(_ context.Context, tx *gorm.DB, id *repository.Identity, recAny any) error {
	rec := recAny.(*model.WebDatabaseUser)
	username := ""
	if id != nil {
		username = id.Username
	}
	for _, col := range []string{"database_user_id", "database_ro_user_id"} {
		var dbs []model.WebDatabase
		if err := tx.Where(col+" = ?", rec.DatabaseUserID).Find(&dbs).Error; err != nil {
			return err
		}
		for i := range dbs {
			old := dbs[i]
			if col == "database_user_id" {
				dbs[i].DatabaseUserID = nil
			} else {
				dbs[i].DatabaseROUserID = nil
			}
			if err := tx.Model(&dbs[i]).Select(col).Updates(map[string]any{col: nil}).Error; err != nil {
				return err
			}
			if err := datalog.LogUpdate(tx, &old, &dbs[i], username); err != nil {
				return err
			}
		}
	}
	return nil
}

// databaseUserSecretColumns are redacted on every read (design D13:
// password hashes never leave the API).
var databaseUserSecretColumns = []string{
	"database_password", "database_password_sha2",
	"database_password_mongo", "database_password_postgres",
}

// sitesDatabaseUserDecorate combines the datalog state decoration with
// hash redaction.
func sitesDatabaseUserDecorate() func(ctx context.Context, db *gorm.DB, items []map[string]any) error {
	state := datalogStateDecorator("web_database_user", "database_user_id")
	return func(ctx context.Context, db *gorm.DB, items []map[string]any) error {
		for _, item := range items {
			for _, col := range databaseUserSecretColumns {
				delete(item, col)
			}
		}
		return state(ctx, db, items)
	}
}

// --- entity definitions (design D13/D14) ---

// registerSitesDatabaseEntities mounts the databases and database-users
// CRUD entities on the /sites group.
func registerSitesDatabaseEntities(g *echo.Group, d *Deps) error {
	if err := RegisterEntity[model.WebDatabase](g, d, sitesDatabaseEntity()); err != nil {
		return err
	}
	return RegisterEntity[model.WebDatabaseUser](g, d, sitesDatabaseUserEntity())
}

// sitesDatabaseEntity is the declarative definition of the web_database
// form (port of database.tform.php, MySQL only).
func sitesDatabaseEntity() *Entity {
	return &Entity{
		Name:  "databases",
		Title: "database_edit_title",
		Prepare: func(c *echo.Context, deps *Deps, id *repository.Identity, body map[string]any) error {
			return sitesDatabasePrepare(c, deps, id, body)
		},
		AfterInsert:  sitesDatabaseAfterInsert,
		BeforeUpdate: sitesDatabaseBeforeUpdate,
		Decorate:     datalogStateDecorator("web_database", "database_id"),
		Tabs: []Tab{
			{
				Name: "database", Label: "database_tab_txt",
				Fields: []Field{
					selectField("server_id", "server_id_txt", "INTEGER", nil, nil,
						validator.Rule{Type: "ISPOSITIVE", ErrKey: "no_server_error"}),
					selectField("parent_domain_id", "parent_domain_id_txt", "INTEGER", nil, nil),
					selectField("type", "database_type_txt", "VARCHAR", "mysql",
						[]Option{{Value: "mysql", Label: "MySQL"}}),
					{Name: "database_name", Label: "database_name_txt", Datatype: "VARCHAR",
						Formtype: "TEXT", Validators: databaseNameRules()},
					text("database_name_prefix", "database_name_prefix_txt"),
					intField("database_quota", "database_quota_txt", "-1",
						validator.Rule{Type: "ISINT", ErrKey: "limit_database_quota_error_notint"}),
					selectField("database_user_id", "database_user_txt", "INTEGER", nil, nil),
					selectField("database_ro_user_id", "database_ro_user_txt", "INTEGER", nil, nil),
					selectField("database_charset", "database_charset_txt", "VARCHAR", "",
						databaseCharsetOptions(),
						validator.Rule{Type: "CUSTOM", ErrKey: "database_charset_error_regex", Fn: checkDatabaseCharset}),
					selectField("backup_interval", "backup_interval_txt", "VARCHAR", "none", []Option{
						{Value: "none", Label: "no_backup_txt"},
						{Value: "daily", Label: "daily_backup_txt"},
						{Value: "weekly", Label: "weekly_backup_txt"},
						{Value: "monthly", Label: "monthly_backup_txt"},
					}),
					checkbox("remote_access", "remote_access_txt", "n"),
					{Name: "remote_ips", Label: "remote_ips_txt", Datatype: "TEXT", Formtype: "TEXT",
						Validators: []validator.Rule{
							{Type: "CUSTOM", ErrKey: "database_remote_error_ips", Fn: checkRemoteIPList},
						}},
					checkbox("active", "active_txt", "y"),
				},
			},
		},
	}
}

// sitesDatabaseUserEntity is the declarative definition of the
// web_database_user form (port of database_user.tform.php).
// database_password_sha2 is declared (not rendered) so the Prepare hook's
// dual-hash store reaches the record; reads redact every hash column.
func sitesDatabaseUserEntity() *Entity {
	return &Entity{
		Name:  "database-users",
		Title: "database_user_edit_title",
		Prepare: func(c *echo.Context, deps *Deps, id *repository.Identity, body map[string]any) error {
			return sitesDatabaseUserPrepare(c, deps, id, body)
		},
		BeforeUpdate: sitesDatabaseUserBeforeUpdate,
		BeforeDelete: sitesDatabaseUserBeforeDelete,
		Decorate:     sitesDatabaseUserDecorate(),
		Tabs: []Tab{
			{
				Name: "database_user", Label: "database_user_tab_txt",
				Fields: []Field{
					selectField("server_id", "server_id_txt", "INTEGER", nil, nil),
					{Name: "database_user", Label: "database_user_txt", Datatype: "VARCHAR",
						Formtype: "TEXT", Validators: databaseUserRules()},
					text("database_user_prefix", "database_user_prefix_txt"),
					{Name: "database_password", Label: "database_password_txt",
						Datatype: "VARCHAR", Formtype: "PASSWORD"},
					{Name: "database_password_sha2", Label: "", Datatype: "VARCHAR",
						Formtype: "PASSWORD"},
				},
			},
		},
	}
}
