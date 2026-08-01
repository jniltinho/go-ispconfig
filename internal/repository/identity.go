package repository

import (
	"fmt"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"go-ispconfig/internal/model"
)

// Identity is the resolved permission identity of a request: the sys_user
// id, its access level and the effective group list used by WithPerm.
type Identity struct {
	// UserID is sys_user.userid.
	UserID uint32
	// Username is sys_user.username, recorded in sys_datalog.user.
	Username string
	// Typ is sys_user.typ: "admin" bypasses record permissions.
	Typ string
	// Groups is the effective sys_group id list: the sys_user.groups CSV
	// plus, for resellers, the groups of their clients (resolved through
	// client.parent_client_id, design D4).
	Groups []uint32
}

// IsAdmin reports whether the identity bypasses record permissions.
func (id *Identity) IsAdmin() bool { return id.Typ == "admin" }

// InGroup reports whether groupID is in the identity's effective groups.
func (id *Identity) InGroup(groupID uint32) bool {
	for _, g := range id.Groups {
		if g == groupID {
			return true
		}
	}
	return false
}

// ResolveIdentity builds the Identity for a sys_user: it parses the
// sys_user.groups CSV and, for non-admin users owning clients (resellers),
// adds the sys_group of every client whose parent_client_id points at the
// user's client — the full ISPConfig reseller graph (design D4).
func ResolveIdentity(db *gorm.DB, u *model.SysUser) (*Identity, error) {
	id := &Identity{
		UserID:   u.UserID,
		Username: u.Username,
		Typ:      u.Typ,
		Groups:   parseGroupCSV(u.Groups),
	}
	if !id.IsAdmin() && u.ClientID > 0 {
		var childGroups []uint32
		err := db.Table("sys_group").
			Joins("JOIN client ON client.client_id = sys_group.client_id").
			Where("client.parent_client_id = ?", u.ClientID).
			Pluck("sys_group.groupid", &childGroups).Error
		if err != nil {
			return nil, fmt.Errorf("resolving reseller client groups: %w", err)
		}
		for _, g := range childGroups {
			if !id.InGroup(g) {
				id.Groups = append(id.Groups, g)
			}
		}
	}
	return id, nil
}

// parseGroupCSV parses the sys_user.groups comma-separated group id list,
// silently skipping empty or malformed entries (matching PHP's loose
// handling of the column).
func parseGroupCSV(csv string) []uint32 {
	var groups []uint32
	for part := range strings.SplitSeq(csv, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, err := strconv.ParseUint(part, 10, 32)
		if err != nil || n == 0 {
			continue
		}
		groups = append(groups, uint32(n))
	}
	return groups
}
