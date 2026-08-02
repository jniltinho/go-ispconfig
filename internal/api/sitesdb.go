package api

// This file holds the sites database module domain logic (openspec change
// add-database-module, lote D): the Go port of tools_sites prefix
// handling, database_edit.php / database_user_edit.php validation and the
// sites_database_plugin hooks, feeding the /api/sites/databases and
// /api/sites/database-users entities.

import (
	"context"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"go-ispconfig/internal/model"
	"go-ispconfig/internal/repository"
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
