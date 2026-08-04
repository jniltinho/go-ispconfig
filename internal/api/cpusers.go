package api

// CP Users CRUD entity (System → CP Users): the panel login accounts in
// sys_user. Port of interface/web/admin/form/users.tform.php plus the rules
// that live in users_edit.php rather than the form definition:
//
//	insert          — only admins are created here; a client login is
//	                  created by the Client module, which also builds its
//	                  sys_group and client row (no_user_insert).
//	typ = admin     — gated by the admin_allow_new_admin security policy on
//	                  both create and update.
//	typ transitions — a login that belongs to a client (client_id > 0) can
//	                  never become admin, and a login that does not belong
//	                  to one can never become a plain user: either way the
//	                  account would end up without a coherent permission
//	                  scope.
//	rename/repassword — propagated to the client row and the client's
//	                  sys_group name, inside the same transaction.
//	delete          — gated by admin_allow_del_cpuser; the seeded admin
//	                  (userid 1) and client-owned logins are refused.
//
// Fields with no consumer in this panel are deliberately not rendered:
// startmodule (the SPA always lands on /dashboard), app_theme (the theme is
// a client-side toggle), otp_type (no OTP implementation) and
// lost_password_function (the login page's "password lost" only shows a
// hint). See docs/cp-users-module.md.

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v5"
	"gorm.io/gorm"

	"go-ispconfig/internal/auth"
	"go-ispconfig/internal/datalog"
	"go-ispconfig/internal/model"
	"go-ispconfig/internal/repository"
	"go-ispconfig/internal/validator"
)

// cpUserMinPasswordLen is the floor applied to a submitted CP user password.
// Same value as the client change-password endpoint, so the two ways to set
// a panel password agree.
const cpUserMinPasswordLen = 8

// cpUserPanelModules are the module ids of this panel (frontend/src/modules.ts).
// sys_user.modules only gates non-admin logins — AppShell shows every module
// to typ=admin — but an admin trimming a client's modules is exactly what the
// legacy form is for.
var cpUserPanelModules = []string{
	"dashboard", "client", "sites", "mail", "dns", "monitor", "help", "tools", "system",
}

// cpUserEntity is the /api/cp-users CRUD surface.
func cpUserEntity() *Entity {
	modules := make([]Option, 0, len(cpUserPanelModules))
	for _, m := range cpUserPanelModules {
		modules = append(modules, Option{Value: m, Label: "module." + m})
	}
	return &Entity{
		Name:         "cp-users",
		Title:        "cpuser_edit_title",
		AdminOnly:    true,
		Decorate:     redactCPUserSecrets,
		Prepare:      cpUserPrepare,
		AfterInsert:  cpUserAfterInsert,
		BeforeUpdate: cpUserBeforeUpdate,
		BeforeDelete: cpUserBeforeDelete,
		Tabs: []Tab{{
			Name:  "users",
			Label: "cpuser_tab_title",
			Fields: []Field{
				{
					Name: "username", Label: "username_txt",
					Datatype: "VARCHAR", Formtype: "TEXT",
					Validators: []validator.Rule{
						{Type: "NOTEMPTY", ErrKey: "username_empty"},
						{Type: "UNIQUE", ErrKey: "username_unique"},
						{Type: "REGEX", Regex: `^[\w.\-]{1,64}$`, ErrKey: "username_err"},
					},
				},
				{
					// Hashed in Prepare and redacted from every response;
					// empty on update means "keep the stored hash".
					Name: "passwort", Label: "password_txt",
					Datatype: "VARCHAR", Formtype: "PASSWORD",
				},
				{
					Name: "typ", Label: "typ_txt",
					Datatype: "VARCHAR", Formtype: "SELECT",
					Default: "admin",
					Options: []Option{{Value: "user", Label: "cpuser.typ_user"}, {Value: "admin", Label: "cpuser.typ_admin"}},
				},
				{
					Name: "active", Label: "active_txt",
					Datatype: "INTEGER", Formtype: "CHECKBOX",
					Default: 1,
					Options: []Option{{Value: "0", Label: "no_txt"}, {Value: "1", Label: "yes_txt"}},
				},
				{
					Name: "modules", Label: "modules_txt",
					Datatype: "VARCHAR", Formtype: "CHECKBOXARRAY",
					Options: modules,
				},
				{
					Name: "language", Label: "language_txt",
					Datatype: "VARCHAR", Formtype: "SELECT",
					Default: "en",
					Options: []Option{{Value: "en", Label: "English"}, {Value: "pt-BR", Label: "Português (BR)"}},
				},
			},
		}},
	}
}

// redactCPUserSecrets keeps hashes and key material out of every CP user
// response: the form re-sends an empty password to mean "unchanged", so the
// stored hash never has to travel to the browser.
func redactCPUserSecrets(_ context.Context, _ *gorm.DB, items []map[string]any) error {
	for _, it := range items {
		delete(it, "passwort")
		delete(it, "id_rsa")
		delete(it, "ssh_rsa")
		delete(it, "otp_data")
		delete(it, "otp_recovery")
		delete(it, "lost_password_hash")
	}
	return nil
}

// cpUserModulesCSV normalizes the submitted modules value (CSV string or JSON
// array) to the stored CSV, dropping ids this panel does not have so a stale
// legacy value cannot be written back verbatim.
func cpUserModulesCSV(v any) (string, bool) {
	var parts []string
	switch t := v.(type) {
	case string:
		parts = strings.Split(t, ",")
	case []any:
		for _, e := range t {
			s, ok := e.(string)
			if !ok {
				return "", false
			}
			parts = append(parts, s)
		}
	default:
		return "", false
	}
	known := map[string]bool{}
	for _, m := range cpUserPanelModules {
		known[m] = true
	}
	out := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] || !known[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return strings.Join(out, ","), true
}

// cpUserPrepare hashes a submitted password, normalizes modules and applies
// the create-time rules: only admins are created here, and creating one is
// gated by admin_allow_new_admin.
func cpUserPrepare(c *echo.Context, d *Deps, _ *repository.Identity, body map[string]any) error {
	isUpdate := c.Request().Method == http.MethodPut

	pw, _ := body["passwort"].(string)
	switch {
	case pw == "" && isUpdate:
		delete(body, "passwort") // unchanged
	case pw == "":
		return &ValidationError{Fields: map[string][]string{"passwort": {"password_error_empty"}}}
	case len(pw) < cpUserMinPasswordLen:
		return &ValidationError{Fields: map[string][]string{"passwort": {"password_error_length"}}}
	default:
		hash, err := auth.HashPassword(pw)
		if err != nil {
			return err
		}
		body["passwort"] = hash
	}

	if v, ok := body["modules"]; ok {
		csv, ok := cpUserModulesCSV(v)
		if !ok {
			return &ValidationError{Fields: map[string][]string{"modules": {"modules_error_invalid"}}}
		}
		body["modules"] = csv
	}

	typ, hasTyp := body["typ"].(string)
	if !isUpdate {
		// Port of users_edit.php::onBeforeInsert "Do not add users here":
		// a client login is created by the Client module together with its
		// sys_group and client row; one created here would have neither.
		if hasTyp && typ != "admin" {
			return &ValidationError{Fields: map[string][]string{"typ": {"cpuser_error_no_user_insert"}}}
		}
		body["typ"] = "admin"
	}
	if typ == "admin" || !isUpdate {
		if err := requireNewAdminPolicy(c, d); err != nil {
			return err
		}
	}
	return nil
}

// requireNewAdminPolicy enforces admin_allow_new_admin for the session.
func requireNewAdminPolicy(c *echo.Context, d *Deps) error {
	sess := auth.FromContext(c)
	if sess == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}
	ok, err := auth.CheckPolicy(d.DB.WithContext(c.Request().Context()), "admin_allow_new_admin", sess.UserID)
	if err != nil {
		return err
	}
	if !ok {
		return echo.NewHTTPError(http.StatusForbidden, "blocked by security policy admin_allow_new_admin")
	}
	return nil
}

// cpUserAfterInsert stamps the admin identity columns on a freshly created
// CP user. They are not form fields — an admin login always belongs to the
// admin group and owns itself, exactly like the seeded userid 1 row — but
// leaving them at zero would give the account no permission scope at all.
func cpUserAfterInsert(_ context.Context, tx *gorm.DB, _ *repository.Identity, rec any, _ map[string]any) error {
	user, ok := rec.(*model.SysUser)
	if !ok {
		return nil
	}
	user.SysUserID = user.UserID
	user.SysGroupID = adminGroupID
	user.SysPermUser = "riud"
	user.SysPermGroup = "riud"
	user.Groups = strconv.FormatUint(uint64(adminGroupID), 10)
	user.DefaultGroup = adminGroupID
	return tx.Model(&model.SysUser{}).Where("userid = ?", user.UserID).Updates(map[string]any{
		"sys_userid":     user.SysUserID,
		"sys_groupid":    user.SysGroupID,
		"sys_perm_user":  user.SysPermUser,
		"sys_perm_group": user.SysPermGroup,
		"groups":         user.Groups,
		"default_group":  user.DefaultGroup,
	}).Error
}

// adminGroupID is sys_group 1, the admin group seeded with the schema.
const adminGroupID uint32 = 1

// cpUserBeforeUpdate enforces the typ transition rules and propagates a
// rename or a new password to the client row and the client's group name,
// inside the update transaction (port of users_edit.php::onBeforeUpdate and
// ::onAfterUpdate).
func cpUserBeforeUpdate(ctx context.Context, tx *gorm.DB, _ *repository.Identity, body map[string]any, old, rec any) error {
	stored, ok := old.(*model.SysUser)
	if !ok {
		return nil
	}
	updated, _ := rec.(*model.SysUser)

	if typ, ok := body["typ"].(string); ok && typ != stored.Typ {
		switch {
		case typ == "admin" && stored.ClientID > 0:
			return &ValidationError{Fields: map[string][]string{"typ": {"cpuser_error_client_not_admin"}}}
		case typ == "user" && stored.ClientID == 0:
			return &ValidationError{Fields: map[string][]string{"typ": {"cpuser_error_no_user_insert"}}}
		}
	}

	if updated == nil || stored.ClientID == 0 {
		return nil
	}
	// The login belongs to a client: keep client.username / client.password
	// and the group name in step, or the client would no longer be able to
	// log in under the name the admin just gave it.
	changes := map[string]any{}
	if updated.Username != stored.Username {
		changes["username"] = updated.Username
	}
	if updated.Passwort != stored.Passwort {
		changes["password"] = updated.Passwort
	}
	if len(changes) == 0 {
		return nil
	}
	err := tx.WithContext(ctx).Model(&model.Client{}).
		Where("client_id = ?", stored.ClientID).Updates(changes).Error
	if err != nil {
		return err
	}
	if _, renamed := changes["username"]; !renamed {
		return nil
	}
	var group model.SysGroup
	err = tx.WithContext(ctx).Where("client_id = ?", stored.ClientID).Take(&group).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	group.Name = updated.Username
	if err := tx.WithContext(ctx).Model(&model.SysGroup{}).
		Where("groupid = ?", group.GroupID).Update("name", group.Name).Error; err != nil {
		return err
	}
	return datalog.LogUpdate(tx, &model.SysGroup{GroupID: group.GroupID, Name: stored.Username}, &group, updated.Username)
}

// cpUserBeforeDelete refuses the deletes that would break the panel: the
// seeded admin (nobody could log in afterwards) and a client-owned login,
// which the Client module removes together with its group and client row.
// Gated by admin_allow_del_cpuser.
func cpUserBeforeDelete(ctx context.Context, tx *gorm.DB, id *repository.Identity, rec any) error {
	user, ok := rec.(*model.SysUser)
	if !ok {
		return nil
	}
	allowed, err := auth.CheckPolicy(tx.WithContext(ctx), "admin_allow_del_cpuser", id.UserID)
	if err != nil {
		return err
	}
	if !allowed {
		return echo.NewHTTPError(http.StatusForbidden, "blocked by security policy admin_allow_del_cpuser")
	}
	if user.UserID == auth.SuperadminUserID {
		return &ValidationError{Fields: map[string][]string{"userid": {"cpuser_error_delete_admin"}}}
	}
	if user.ClientID > 0 {
		return &ValidationError{Fields: map[string][]string{"userid": {"cpuser_error_delete_client"}}}
	}
	return nil
}
