package clients

import (
	"context"
	"errors"
	"fmt"

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
	default:
		// Reserved for future modules (mail/ftp/shell/db/cron): unknown
		// entities are never vetoed.
		return limitRule{}, false
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
