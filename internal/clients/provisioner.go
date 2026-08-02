package clients

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"go-ispconfig/internal/datalog"
	"go-ispconfig/internal/model"
)

// InterfaceModules is the module set granted to new client panel logins
// (PHP $conf['interface_modules_enabled'] for this panel's scope).
// Resellers additionally get the client module.
const InterfaceModules = "dashboard,sites,dns,tools,help"

// themeRe sanitizes usertheme (path-traversal guard, PHP parity).
var themeRe = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,32}$`)

// Note on datalog: sys_group/sys_user are untracked by the foundation
// (no daemon hook consumes them, matching client_module.inc.php which
// hooks only `client`), so the Log* calls below are deliberate no-ops
// kept for the day those tables opt into tracking.

// ProvisionIdentity creates the panel identity of a freshly inserted
// client row inside the same transaction (port of
// client_edit.php::onAfterInsert): the sys_group named after the client,
// the sys_user login bound to it, parent-reseller group membership, and
// re-owning the client row under its parent reseller (admin-created) or
// keeping creator ownership. passwordHash is the already-hashed login
// password (client.password and sys_user.passwort share it, PHP parity).
func ProvisionIdentity(ctx context.Context, tx *gorm.DB, c *model.Client, passwordHash, actorUsername string) error {
	group := &model.SysGroup{Name: c.Username, Description: "", ClientID: c.ClientID}
	if err := tx.WithContext(ctx).Create(group).Error; err != nil {
		return fmt.Errorf("clients: creating sys_group: %w", err)
	}
	if err := datalog.LogInsert(tx, group, actorUsername); err != nil {
		return err
	}

	modules := InterfaceModules
	if IsReseller(c) && !hasModule(modules, "client") {
		modules += ",client"
	}
	startmodule := "client"
	if hasModule(modules, "dashboard") {
		startmodule = "dashboard"
	}
	theme := c.Usertheme
	if !themeRe.MatchString(theme) {
		theme = "default"
	}
	language := c.Language
	if language == "" {
		language = "en"
	}

	user := &model.SysUser{
		SysUserID: 1, SysGroupID: group.GroupID,
		SysPermUser: "riud", SysPermGroup: "riud",
		Username: c.Username, Passwort: passwordHash,
		Modules: modules, Startmodule: startmodule, AppTheme: theme,
		Typ: "user", Active: 1, Language: language,
		Groups: strconv.FormatUint(uint64(group.GroupID), 10), DefaultGroup: group.GroupID,
		ClientID: c.ClientID,
	}
	if c.Locked == "y" {
		user.Active = 0
	}
	if err := tx.WithContext(ctx).Create(user).Error; err != nil {
		return fmt.Errorf("clients: creating sys_user: %w", err)
	}
	if err := datalog.LogInsert(tx, user, actorUsername); err != nil {
		return err
	}

	if c.ParentClientID > 0 {
		if err := attachGroupToParent(ctx, tx, c.ParentClientID, group.GroupID); err != nil {
			return err
		}
		// Admin-created client under a reseller: the client row belongs
		// to the reseller's user/group (design D3.5) so the reseller's
		// riud scope covers it.
		parentUser, err := parentSysUser(ctx, tx, c.ParentClientID)
		if err != nil {
			return err
		}
		c.SysUserID = parentUser.UserID
		c.SysGroupID = parentUser.DefaultGroup
		err = tx.WithContext(ctx).Model(&model.Client{}).
			Where("client_id = ?", c.ClientID).
			Updates(map[string]any{"sys_userid": c.SysUserID, "sys_groupid": c.SysGroupID}).Error
		if err != nil {
			return fmt.Errorf("clients: re-owning client under reseller: %w", err)
		}
	}
	return nil
}

// SyncIdentity propagates client row changes onto the linked sys_user /
// sys_group inside the update transaction (port of onAfterUpdate):
// username rename, password change (hash already computed), language,
// locked → active, limit_client → client module token, and parent
// reassignment (group membership moves between resellers).
func SyncIdentity(ctx context.Context, tx *gorm.DB, old, updated *model.Client, newPasswordHash string) error {
	var group model.SysGroup
	err := tx.WithContext(ctx).Where("client_id = ?", updated.ClientID).Take(&group).Error
	if err != nil {
		return fmt.Errorf("clients: loading client group: %w", err)
	}
	var user model.SysUser
	err = tx.WithContext(ctx).Where("client_id = ?", updated.ClientID).Take(&user).Error
	if err != nil {
		return fmt.Errorf("clients: loading client sys_user: %w", err)
	}

	userChanges := map[string]any{}
	if old.Username != updated.Username {
		userChanges["username"] = updated.Username
		err = tx.WithContext(ctx).Model(&model.SysGroup{}).
			Where("groupid = ?", group.GroupID).Update("name", updated.Username).Error
		if err != nil {
			return fmt.Errorf("clients: renaming sys_group: %w", err)
		}
	}
	if newPasswordHash != "" {
		userChanges["passwort"] = newPasswordHash
		userChanges["last_password_change"] = time.Now()
	}
	if old.Language != updated.Language && updated.Language != "" {
		userChanges["language"] = updated.Language
	}
	if old.Locked != updated.Locked {
		if updated.Locked == "y" {
			userChanges["active"] = 0
		} else {
			userChanges["active"] = 1
		}
	}
	wasReseller, isNow := old.LimitClient != 0, updated.LimitClient != 0
	if wasReseller != isNow {
		modules := user.Modules
		if isNow && !hasModule(modules, "client") {
			modules += ",client"
		}
		if !isNow {
			modules = removeModule(modules, "client")
		}
		userChanges["modules"] = modules
	}
	if len(userChanges) > 0 {
		err = tx.WithContext(ctx).Model(&model.SysUser{}).
			Where("userid = ?", user.UserID).Updates(userChanges).Error
		if err != nil {
			return fmt.Errorf("clients: syncing sys_user: %w", err)
		}
	}

	if old.ParentClientID != updated.ParentClientID {
		if old.ParentClientID > 0 {
			if err := detachGroupFromParent(ctx, tx, old.ParentClientID, group.GroupID); err != nil {
				return err
			}
		}
		if updated.ParentClientID > 0 {
			if err := attachGroupToParent(ctx, tx, updated.ParentClientID, group.GroupID); err != nil {
				return err
			}
		}
	}
	return nil
}

// DeprovisionIdentity removes the client's panel identity inside the
// delete transaction (port of client_del.php): template assignments, the
// group's membership in every sys_user.groups CSV (parent reseller
// detach), the client's sys_user rows and its sys_group. The client row
// itself is deleted (and datalogged) by the caller.
func DeprovisionIdentity(ctx context.Context, tx *gorm.DB, c *model.Client, actorUsername string) error {
	err := tx.WithContext(ctx).
		Where("client_id = ?", c.ClientID).Delete(&model.ClientTemplateAssigned{}).Error
	if err != nil {
		return fmt.Errorf("clients: deleting template assignments: %w", err)
	}

	var group model.SysGroup
	err = tx.WithContext(ctx).Where("client_id = ?", c.ClientID).Take(&group).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		// No group (e.g. imported partial data): nothing to detach.
	case err != nil:
		return fmt.Errorf("clients: loading client group: %w", err)
	default:
		// Detach the group from every sys_user that carries it (parent
		// resellers and any admin-granted membership).
		var carriers []model.SysUser
		err = tx.WithContext(ctx).
			Where("groups LIKE ?", "%"+strconv.FormatUint(uint64(group.GroupID), 10)+"%").
			Find(&carriers).Error
		if err != nil {
			return fmt.Errorf("clients: finding group carriers: %w", err)
		}
		for i := range carriers {
			if carriers[i].ClientID == c.ClientID {
				continue // about to be deleted anyway
			}
			cleaned := removeGroupFromCSV(carriers[i].Groups, group.GroupID)
			if cleaned == carriers[i].Groups {
				continue // LIKE false positive (e.g. 12 vs 112)
			}
			err = tx.WithContext(ctx).Model(&model.SysUser{}).
				Where("userid = ?", carriers[i].UserID).Update("groups", cleaned).Error
			if err != nil {
				return fmt.Errorf("clients: detaching group from user %d: %w", carriers[i].UserID, err)
			}
		}

		var users []model.SysUser
		err = tx.WithContext(ctx).Where("client_id = ?", c.ClientID).Find(&users).Error
		if err != nil {
			return fmt.Errorf("clients: loading client sys_users: %w", err)
		}
		for i := range users {
			if err := tx.WithContext(ctx).Delete(&users[i]).Error; err != nil {
				return fmt.Errorf("clients: deleting sys_user %d: %w", users[i].UserID, err)
			}
			if err := datalog.LogDelete(tx, &users[i], actorUsername); err != nil {
				return err
			}
		}
		if err := tx.WithContext(ctx).Delete(&group).Error; err != nil {
			return fmt.Errorf("clients: deleting sys_group: %w", err)
		}
		if err := datalog.LogDelete(tx, &group, actorUsername); err != nil {
			return err
		}
	}
	return nil
}

// parentSysUser resolves the reseller panel login of a parent client id
// (the sys_user whose default_group is the parent's group, PHP query).
func parentSysUser(ctx context.Context, tx *gorm.DB, parentClientID uint32) (*model.SysUser, error) {
	var user model.SysUser
	err := tx.WithContext(ctx).
		Joins("JOIN sys_group ON sys_user.default_group = sys_group.groupid").
		Where("sys_group.client_id = ?", parentClientID).
		Take(&user).Error
	if err != nil {
		return nil, fmt.Errorf("clients: resolving parent reseller sys_user (client %d): %w", parentClientID, err)
	}
	return &user, nil
}

// attachGroupToParent appends groupID to the parent reseller's
// sys_user.groups CSV (port of auth::add_group_to_user).
func attachGroupToParent(ctx context.Context, tx *gorm.DB, parentClientID, groupID uint32) error {
	user, err := parentSysUser(ctx, tx, parentClientID)
	if err != nil {
		return err
	}
	next := addGroupToCSV(user.Groups, groupID)
	if next == user.Groups {
		return nil
	}
	err = tx.WithContext(ctx).Model(&model.SysUser{}).
		Where("userid = ?", user.UserID).Update("groups", next).Error
	if err != nil {
		return fmt.Errorf("clients: attaching group to reseller: %w", err)
	}
	return nil
}

// detachGroupFromParent removes groupID from the parent reseller's
// sys_user.groups CSV.
func detachGroupFromParent(ctx context.Context, tx *gorm.DB, parentClientID, groupID uint32) error {
	user, err := parentSysUser(ctx, tx, parentClientID)
	if err != nil {
		return err
	}
	next := removeGroupFromCSV(user.Groups, groupID)
	if next == user.Groups {
		return nil
	}
	err = tx.WithContext(ctx).Model(&model.SysUser{}).
		Where("userid = ?", user.UserID).Update("groups", next).Error
	if err != nil {
		return fmt.Errorf("clients: detaching group from reseller: %w", err)
	}
	return nil
}

// addGroupToCSV appends id to a comma-separated group list without
// duplicating it.
func addGroupToCSV(csv string, id uint32) string {
	want := strconv.FormatUint(uint64(id), 10)
	parts := splitCSV(csv)
	for _, p := range parts {
		if p == want {
			return csv
		}
	}
	parts = append(parts, want)
	return strings.Join(parts, ",")
}

// removeGroupFromCSV removes id from a comma-separated group list.
func removeGroupFromCSV(csv string, id uint32) string {
	want := strconv.FormatUint(uint64(id), 10)
	var out []string
	for _, p := range splitCSV(csv) {
		if p != want {
			out = append(out, p)
		}
	}
	return strings.Join(out, ",")
}

// splitCSV splits a groups CSV, dropping empties (PHP-loose parsing).
func splitCSV(csv string) []string {
	var out []string
	for _, p := range strings.Split(csv, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// hasModule reports whether the modules CSV contains name.
func hasModule(modules, name string) bool {
	for _, m := range strings.Split(modules, ",") {
		if strings.TrimSpace(m) == name {
			return true
		}
	}
	return false
}

// removeModule removes name from the modules CSV.
func removeModule(modules, name string) string {
	var out []string
	for _, m := range strings.Split(modules, ",") {
		m = strings.TrimSpace(m)
		if m != "" && m != name {
			out = append(out, m)
		}
	}
	return strings.Join(out, ",")
}
