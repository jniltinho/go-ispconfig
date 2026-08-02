// Package clients implements the client-module domain logic (openspec
// change add-client-module): reseller/client rules, identity
// provisioning (sys_user/sys_group lifecycle), limit-template
// materialization, limit enforcement and messaging. It ports
// interface/web/client/* and client_templates.inc.php on the immutable
// ISPConfig3 schema.
package clients

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"gorm.io/gorm"

	"go-ispconfig/internal/model"
)

// IsReseller reports whether a client record is a reseller: the PHP rule
// used everywhere is limit_client != 0 (-1 unlimited or a positive
// sub-client quota).
func IsReseller(c *model.Client) bool { return c.LimitClient != 0 }

// usernameRe is the tform username rule (client.tform.php REGEX
// /^[\w\.\-]{0,64}$/) with NOTEMPTY folded in.
var usernameRe = regexp.MustCompile(`^[\w.\-]{1,64}$`)

// ValidUsername reports whether username satisfies the tform pattern.
func ValidUsername(username string) bool { return usernameRe.MatchString(username) }

// Parent validation errors (client-management spec: one reseller level).
var (
	// ErrParentNotReseller: parent_client_id points at a plain client.
	ErrParentNotReseller = errors.New("clients: parent is not a reseller")
	// ErrNestedReseller: a reseller cannot hang under another reseller.
	ErrNestedReseller = errors.New("clients: reseller cannot have a reseller parent")
)

// CheckParent validates a parent assignment: nil parent (admin-owned) is
// always fine; a parent must itself be a reseller; and a reseller child
// under any parent is rejected (single nesting level).
func CheckParent(parent *model.Client, childIsReseller bool) error {
	if parent == nil {
		return nil
	}
	if !IsReseller(parent) {
		return ErrParentNotReseller
	}
	if childIsReseller {
		return ErrNestedReseller
	}
	return nil
}

// LoadParent fetches the parent client row for validation; parentID 0
// returns nil (admin-owned).
func LoadParent(ctx context.Context, db *gorm.DB, parentID uint32) (*model.Client, error) {
	if parentID == 0 {
		return nil, nil
	}
	var parent model.Client
	err := db.WithContext(ctx).Where("client_id = ?", parentID).Take(&parent).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("clients: parent client %d does not exist", parentID)
	}
	if err != nil {
		return nil, err
	}
	return &parent, nil
}

// UsernameTaken reports whether username is already used by another
// client row or any sys_user not belonging to excludeClientID (port of
// the client_edit duplicate check across both tables).
func UsernameTaken(ctx context.Context, db *gorm.DB, username string, excludeClientID uint32) (bool, error) {
	var n int64
	err := db.WithContext(ctx).Model(&model.Client{}).
		Where("username = ? AND client_id <> ?", username, excludeClientID).
		Count(&n).Error
	if err != nil {
		return false, fmt.Errorf("clients: checking client username: %w", err)
	}
	if n > 0 {
		return true, nil
	}
	err = db.WithContext(ctx).Model(&model.SysUser{}).
		Where("username = ? AND client_id <> ?", username, excludeClientID).
		Count(&n).Error
	if err != nil {
		return false, fmt.Errorf("clients: checking sys_user username: %w", err)
	}
	return n > 0, nil
}
