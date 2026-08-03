package clients

// Port of dashboard/dashlets/limits.php: the account-limits dashlet lists
// every limit_* column of the logged-in client next to its current usage.
// The count queries are the ones the create-time limit hook already uses
// (resolveRule), so the dashlet can never disagree with enforcement.

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"go-ispconfig/internal/model"
	"go-ispconfig/internal/repository"
)

// LimitUsage is one row of the account-limits dashlet.
type LimitUsage struct {
	// Field is the client column name (limit_web_domain, ...).
	Field string `json:"field" example:"limit_web_domain"`
	// Limit is the configured cap; -1 means unlimited.
	Limit int32 `json:"limit" example:"10"`
	// Usage is the current count, or the assigned MB for quota rows.
	Usage int64 `json:"usage" example:"3"`
	// Quota marks rows where limit and usage are megabytes, not counts.
	Quota bool `json:"quota,omitempty"`
}

// limitEntities are the count limits, in the order limits.php prints them.
// Each entry is a resolveRule key plus the body the rule inspects (only the
// web_domain rules branch on the type).
var limitEntities = []struct {
	entity string
	body   map[string]any
}{
	{"mail-domains", nil},
	{"mailboxes", nil},
	{"aliases", nil},
	{"alias-domains", nil},
	{"forwards", nil},
	{"catchalls", nil},
	{"web-domains", nil}, // vhost
	{"web-domains", map[string]any{"type": "subdomain"}},
	{"web-domains", map[string]any{"type": "alias"}},
	{"ftp-users", nil},
	{"shell-users", nil},
	{"zones", nil},
	{"slave-zones", nil},
	{"dns_rr", nil},
	{"databases", nil},
	{"database-users", nil},
	{"clients", nil},
}

// extraRules are the limits.php rows with no create-time hook of their own.
var extraRules = []limitRule{
	{"limit_mailmailinglist", func(c *model.Client) int32 { return c.LimitMailmailinglist }, countByGroup("mail_mailinglist", "")},
	{"limit_mailrouting", func(c *model.Client) int32 { return c.LimitMailrouting }, countByGroup("mail_transport", "")},
	{"limit_mail_wblist", func(c *model.Client) int32 { return c.LimitMailWblist }, countByGroup("mail_access", "")},
	{"limit_mailfilter", func(c *model.Client) int32 { return c.LimitMailfilter }, countByGroup("mail_user_filter", "")},
	{"limit_fetchmail", func(c *model.Client) int32 { return c.LimitFetchmail }, countByGroup("mail_get", "")},
	{"limit_spamfilter_wblist", func(c *model.Client) int32 { return c.LimitSpamfilterWblist }, countByGroup("spamfilter_wblist", "")},
	{"limit_spamfilter_user", func(c *model.Client) int32 { return c.LimitSpamfilterUser }, countByGroup("spamfilter_users", "")},
	{"limit_spamfilter_policy", func(c *model.Client) int32 { return c.LimitSpamfilterPolicy }, countByGroup("spamfilter_policy", "")},
	{"limit_cron", func(c *model.Client) int32 { return c.LimitCron }, countByGroup("cron", "")},
	{"limit_domain", func(c *model.Client) int32 { return c.LimitDomainmodule }, countByGroup("domain", "")},
}

// quotaRules are the three limits.php rows whose usage is a sum of assigned
// quota rather than a row count. Only positive quotas count; -1 on a record
// means unlimited and is skipped, exactly like the PHP db_where.
var quotaRules = []struct {
	field  string
	limit  func(*model.Client) int32
	table  string
	column string
	// divisor converts the stored unit to MB (mail_user.quota is bytes).
	divisor int64
}{
	{"limit_mailquota", func(c *model.Client) int32 { return c.LimitMailquota }, "mail_user", "quota", 1048576},
	{"limit_web_quota", func(c *model.Client) int32 { return c.LimitWebQuota }, "web_domain", "hd_quota", 1},
	{"limit_database_quota", func(c *model.Client) int32 { return c.LimitDatabaseQuota }, "web_database", "database_quota", 1},
}

// Usage returns the limits and current usage of the identity's owning
// client. It returns nil when the caller has no client row (admins and
// legacy panel users): those are unlimited and the dashlet says so.
func Usage(ctx context.Context, db *gorm.DB, id *repository.Identity) ([]LimitUsage, error) {
	if id == nil || id.IsAdmin() {
		return nil, nil
	}
	client, err := owningClient(ctx, db, id)
	if err != nil || client == nil {
		return nil, err
	}
	group, err := clientGroupID(ctx, db, client.ClientID)
	if err != nil {
		return nil, err
	}

	// limits.php hides every row whose limit is 0 (no allowance at all).
	rows := make([]LimitUsage, 0, len(limitEntities)+len(extraRules)+len(quotaRules))
	countRules := make([]limitRule, 0, len(limitEntities)+len(extraRules))
	for _, e := range limitEntities {
		rule, ok := resolveRule(e.entity, e.body)
		if !ok {
			continue
		}
		countRules = append(countRules, rule)
	}
	countRules = append(countRules, extraRules...)

	for _, rule := range countRules {
		field := strings.TrimPrefix(rule.key, "error.")
		limit := rule.limit(client)
		if limit == 0 {
			continue // no allowance: skip the query too
		}
		n, err := rule.count(ctx, db, client)
		if err != nil {
			return nil, err
		}
		rows = append(rows, LimitUsage{Field: field, Limit: limit, Usage: n})
	}

	for _, q := range quotaRules {
		limit := q.limit(client)
		if limit == 0 {
			continue
		}
		var sum int64
		err := db.WithContext(ctx).Table(q.table).
			Where("sys_groupid = ? AND "+q.column+" > 0", group).
			Select("COALESCE(SUM(" + q.column + "), 0)").Scan(&sum).Error
		if err != nil {
			return nil, fmt.Errorf("clients: summing %s.%s: %w", q.table, q.column, err)
		}
		rows = append(rows, LimitUsage{
			Field: q.field, Limit: limit, Usage: sum / q.divisor, Quota: true,
		})
	}
	return rows, nil
}
