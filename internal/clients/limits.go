package clients

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"go-ispconfig/internal/model"
	"go-ispconfig/internal/repository"
)

// LimitError is a limit veto; the API error handler maps any error with
// a LimitKey() method to HTTP 403 with the i18n key.
type LimitError struct{ Key string }

func (e *LimitError) Error() string { return "clients: limit exceeded: " + e.Key }

// LimitKey satisfies the API error handler's limit-veto interface.
func (e *LimitError) LimitKey() string { return e.Key }

// LimitHook returns the enforcement function: it resolves the owning
// client of the requesting identity and vetoes creates that would exceed
// the matching limit_* column. Semantics: limit < 0 allow, == 0 veto,
// > 0 veto when the existing count is already >= limit. Admin identities
// bypass all count limits; unknown entity names allow (no-op) so
// unrelated entities keep working. The returned func matches
// api.LimitHook; wire it with api.RegisterLimitHook at startup.
func LimitHook(db *gorm.DB) func(context.Context, string, *repository.Identity, map[string]any) error {
	return func(ctx context.Context, entity string, id *repository.Identity, body map[string]any) error {
		if id == nil || id.IsAdmin() {
			return nil
		}
		rule, ok := resolveRule(entity, body)
		if !ok {
			return nil
		}
		client, err := owningClient(ctx, db, id)
		if err != nil {
			return err
		}
		if client == nil {
			return nil // panel user without a client row (legacy admins)
		}
		if entity == "databases" {
			if err := databaseCreateChecks(ctx, db, client, body); err != nil {
				return err
			}
		}
		if err := checkResellerLimit(ctx, db, client, entity, rule); err != nil {
			return err
		}
		limit := rule.limit(client)
		if limit < 0 {
			return nil
		}
		if limit == 0 {
			return &LimitError{Key: rule.key}
		}
		count, err := rule.count(ctx, db, client)
		if err != nil {
			return err
		}
		if count >= int64(limit) {
			return &LimitError{Key: rule.key}
		}
		return nil
	}
}

// limitRule pairs the limit column accessor, the count query and the
// i18n veto key of one create entity.
type limitRule struct {
	key   string
	limit func(*model.Client) int32
	count func(context.Context, *gorm.DB, *model.Client) (int64, error)
}

// countByGroup counts rows of table owned by the client's own group.
func countByGroup(table, extraWhere string, extraArgs ...any) func(context.Context, *gorm.DB, *model.Client) (int64, error) {
	return func(ctx context.Context, db *gorm.DB, c *model.Client) (int64, error) {
		group, err := clientGroupID(ctx, db, c.ClientID)
		if err != nil {
			return 0, err
		}
		q := db.WithContext(ctx).Table(table).Where("sys_groupid = ?", group)
		if extraWhere != "" {
			q = q.Where(extraWhere, extraArgs...)
		}
		var n int64
		if err := q.Count(&n).Error; err != nil {
			return 0, fmt.Errorf("clients: counting %s: %w", table, err)
		}
		return n, nil
	}
}

// resolveRule maps a create entity name (and body, for per-type web
// limits) to its limit rule.
func resolveRule(entity string, body map[string]any) (limitRule, bool) {
	switch entity {
	case "web-domains":
		typ, _ := body["type"].(string)
		switch typ {
		case "vhostsubdomain", "subdomain":
			return limitRule{
				key:   "error.limit_web_subdomain",
				limit: func(c *model.Client) int32 { return c.LimitWebSubdomain },
				count: countByGroup("web_domain", "type IN ?", []string{"vhostsubdomain", "subdomain"}),
			}, true
		case "vhostalias", "alias", "aliasdomain":
			return limitRule{
				key:   "error.limit_web_aliasdomain",
				limit: func(c *model.Client) int32 { return c.LimitWebAliasdomain },
				count: countByGroup("web_domain", "type IN ?", []string{"vhostalias", "alias", "aliasdomain"}),
			}, true
		default: // vhost (entity default)
			return limitRule{
				key:   "error.limit_web_domain",
				limit: func(c *model.Client) int32 { return c.LimitWebDomain },
				count: countByGroup("web_domain", "type = ?", "vhost"),
			}, true
		}
	case "zones":
		return limitRule{
			key:   "error.limit_dns_zone",
			limit: func(c *model.Client) int32 { return c.LimitDNSZone },
			count: countByGroup("dns_soa", ""),
		}, true
	case "slave-zones":
		return limitRule{
			key:   "error.limit_dns_slave_zone",
			limit: func(c *model.Client) int32 { return c.LimitDNSSlaveZone },
			count: countByGroup("dns_slave", ""),
		}, true
	case "dns_rr":
		return limitRule{
			key:   "error.limit_dns_record",
			limit: func(c *model.Client) int32 { return c.LimitDNSRecord },
			count: countByGroup("dns_rr", ""),
		}, true
	case "mail-domains":
		return limitRule{
			key:   "error.limit_maildomain",
			limit: func(c *model.Client) int32 { return c.LimitMaildomain },
			count: countByGroup("mail_domain", ""),
		}, true
	case "mailboxes":
		return limitRule{
			key:   "error.limit_mailbox",
			limit: func(c *model.Client) int32 { return c.LimitMailbox },
			count: countByGroup("mail_user", ""),
		}, true
	case "aliases":
		return limitRule{
			key:   "error.limit_mailalias",
			limit: func(c *model.Client) int32 { return c.LimitMailalias },
			count: countByGroup("mail_forwarding", "type = ?", "alias"),
		}, true
	case "alias-domains":
		return limitRule{
			key:   "error.limit_mailaliasdomain",
			limit: func(c *model.Client) int32 { return c.LimitMailaliasdomain },
			count: countByGroup("mail_forwarding", "type = ?", "aliasdomain"),
		}, true
	case "forwards":
		return limitRule{
			key:   "error.limit_mailforward",
			limit: func(c *model.Client) int32 { return c.LimitMailforward },
			count: countByGroup("mail_forwarding", "type = ?", "forward"),
		}, true
	case "fetchmail":
		return limitRule{
			key:   "error.limit_fetchmail",
			limit: func(c *model.Client) int32 { return c.LimitFetchmail },
			count: countByGroup("mail_get", ""),
		}, true
	case "catchalls":
		return limitRule{
			key:   "error.limit_mailcatchall",
			limit: func(c *model.Client) int32 { return c.LimitMailcatchall },
			count: countByGroup("mail_forwarding", "type = ?", "catchall"),
		}, true
	case "clients", "resellers":
		// PHP counts child clients by the reseller's group ownership
		// (client rows under a reseller are re-owned to its group).
		return limitRule{
			key:   "error.limit_client",
			limit: func(c *model.Client) int32 { return c.LimitClient },
			count: countByGroup("client", ""),
		}, true
	case "databases":
		return limitRule{
			key:   "error.limit_database",
			limit: func(c *model.Client) int32 { return c.LimitDatabase },
			count: countByGroup("web_database", "type = ?", "mysql"),
		}, true
	case "database-users":
		return limitRule{
			key:   "error.limit_database_user",
			limit: func(c *model.Client) int32 { return c.LimitDatabaseUser },
			count: countByGroup("web_database_user", ""),
		}, true
	case "ftp-users":
		return limitRule{
			key:   "error.limit_ftp_user",
			limit: func(c *model.Client) int32 { return c.LimitFTPUser },
			count: countByGroup("ftp_user", ""),
		}, true
	case "shell-users":
		return limitRule{
			key:   "error.limit_shell_user",
			limit: func(c *model.Client) int32 { return c.LimitShellUser },
			count: countByGroup("shell_user", ""),
		}, true
	default:
		// Reserved for future modules (cron, …): unknown entities are
		// never vetoed.
		return limitRule{}, false
	}
}

// databaseLimitEntities are the entities whose count limits also apply
// at the reseller level (PHP checkResellerLimit is only wired for the
// database module here).
var databaseLimitEntities = map[string]struct{}{"databases": {}, "database-users": {}}

// checkResellerLimit ports tform checkResellerLimit for the database
// entities: when the client belongs to a reseller, the same limit column
// of the reseller row caps the usage summed across every client of that
// reseller (the reseller's own group included).
func checkResellerLimit(ctx context.Context, db *gorm.DB, client *model.Client, entity string, rule limitRule) error {
	if _, ok := databaseLimitEntities[entity]; !ok || client.ParentClientID == 0 {
		return nil
	}
	var reseller model.Client
	err := db.WithContext(ctx).Where("client_id = ?", client.ParentClientID).Take(&reseller).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("clients: loading reseller %d: %w", client.ParentClientID, err)
	}
	limit := rule.limit(&reseller)
	if limit < 0 {
		return nil
	}
	if limit == 0 {
		return &LimitError{Key: rule.key}
	}
	table, typeFilter := "web_database", "AND type = 'mysql'"
	if entity == "database-users" {
		table, typeFilter = "web_database_user", ""
	}
	var count int64
	err = db.WithContext(ctx).Table(table+" t").
		Joins("JOIN sys_group g ON g.groupid = t.sys_groupid").
		Joins("JOIN client c ON c.client_id = g.client_id").
		Where("(c.parent_client_id = ? OR c.client_id = ?) "+typeFilter,
			reseller.ClientID, reseller.ClientID).
		Count(&count).Error
	if err != nil {
		return fmt.Errorf("clients: counting reseller %s usage: %w", table, err)
	}
	if count >= int64(limit) {
		return &LimitError{Key: rule.key}
	}
	return nil
}

// databaseCreateChecks ports the database_edit.php create-path client
// checks that go beyond a plain count limit: the db_servers allow-list
// and the limit_database_quota sum (client and reseller). The quota
// semantics follow PHP: -1 on the DB means unlimited and is rejected
// under a finite client quota, 0 is rejected when the client has a
// positive quota limit, and the sum of quotas may never exceed the
// limit.
func databaseCreateChecks(ctx context.Context, db *gorm.DB, client *model.Client, body map[string]any) error {
	// Client may only place databases on its allow-listed servers.
	if serverID := bodyNum(body, "server_id"); serverID > 0 && client.DBServers != "" {
		allowed := false
		for s := range strings.SplitSeq(client.DBServers, ",") {
			if strings.TrimSpace(s) == strconv.FormatInt(serverID, 10) {
				allowed = true
				break
			}
		}
		if !allowed {
			return &LimitError{Key: "error.not_allowed_server_id"}
		}
	}

	return checkDatabaseQuota(ctx, db, client, bodyNum(body, "database_quota"), 0)
}

// CheckDatabaseQuotaUpdate re-runs the limit_database_quota enforcement
// for an update request (the create limit hook does not fire there):
// excludeID keeps the record's own current quota out of the sums (PHP
// database_edit onSubmit update path). Admin identities bypass.
func CheckDatabaseQuotaUpdate(ctx context.Context, db *gorm.DB, id *repository.Identity, newQuota, excludeID int64) error {
	if id == nil || id.IsAdmin() {
		return nil
	}
	client, err := owningClient(ctx, db, id)
	if err != nil || client == nil {
		return err
	}
	return checkDatabaseQuota(ctx, db, client, newQuota, excludeID)
}

// checkDatabaseQuota enforces limit_database_quota at client and
// reseller level: -1 on the DB is rejected under a finite limit, 0 under
// a positive limit, and the summed quotas may never exceed the limit.
func checkDatabaseQuota(ctx context.Context, db *gorm.DB, client *model.Client, newQuota, excludeID int64) error {
	if client.LimitDatabaseQuota < 0 {
		return nil
	}
	if newQuota < 0 || (client.LimitDatabaseQuota > 0 && newQuota == 0) {
		return &LimitError{Key: "error.limit_database_quota"}
	}
	group, err := clientGroupID(ctx, db, client.ClientID)
	if err != nil {
		return err
	}
	var used int64
	err = db.WithContext(ctx).Table("web_database").
		Where("sys_groupid = ? AND database_id != ?", group, excludeID).
		Select("COALESCE(SUM(database_quota), 0)").Scan(&used).Error
	if err != nil {
		return fmt.Errorf("clients: summing database quota: %w", err)
	}
	if used+newQuota > int64(client.LimitDatabaseQuota) {
		return &LimitError{Key: "error.limit_database_quota"}
	}

	// Reseller quota cap across all its clients.
	if client.ParentClientID == 0 {
		return nil
	}
	var reseller model.Client
	err = db.WithContext(ctx).Where("client_id = ?", client.ParentClientID).Take(&reseller).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("clients: loading reseller %d: %w", client.ParentClientID, err)
	}
	if reseller.LimitDatabaseQuota < 0 {
		return nil
	}
	var resellerUsed int64
	err = db.WithContext(ctx).Table("web_database t").
		Joins("JOIN sys_group g ON g.groupid = t.sys_groupid").
		Joins("JOIN client c ON c.client_id = g.client_id").
		Where("(c.parent_client_id = ? OR c.client_id = ?) AND t.database_id != ?",
			reseller.ClientID, reseller.ClientID, excludeID).
		Select("COALESCE(SUM(t.database_quota), 0)").Scan(&resellerUsed).Error
	if err != nil {
		return fmt.Errorf("clients: summing reseller database quota: %w", err)
	}
	if resellerUsed+newQuota > int64(reseller.LimitDatabaseQuota) {
		return &LimitError{Key: "error.limit_database_quota"}
	}
	return nil
}

// bodyNum reads a numeric JSON body value (float64 or numeric string).
func bodyNum(body map[string]any, key string) int64 {
	switch v := body[key].(type) {
	case float64:
		return int64(v)
	case int:
		return int64(v)
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return n
	default:
		return 0
	}
}

// owningClient resolves the client row of the requesting identity via
// sys_user.client_id; nil when the user has no client (admin-like).
func owningClient(ctx context.Context, db *gorm.DB, id *repository.Identity) (*model.Client, error) {
	var user model.SysUser
	err := db.WithContext(ctx).Select("client_id").
		Where("userid = ?", id.UserID).Take(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && user.ClientID == 0) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("clients: resolving identity client: %w", err)
	}
	var client model.Client
	err = db.WithContext(ctx).Where("client_id = ?", user.ClientID).Take(&client).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("clients: loading owning client: %w", err)
	}
	return &client, nil
}

// clientGroupID resolves the sys_group of a client id.
func clientGroupID(ctx context.Context, db *gorm.DB, clientID uint32) (uint32, error) {
	var group model.SysGroup
	err := db.WithContext(ctx).Select("groupid").
		Where("client_id = ?", clientID).Take(&group).Error
	if err != nil {
		return 0, fmt.Errorf("clients: resolving client group: %w", err)
	}
	return group.GroupID, nil
}
