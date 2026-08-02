package api

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strconv"

	"github.com/labstack/echo/v5"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"

	"go-ispconfig/internal/auth"
	"go-ispconfig/internal/clients"
	"go-ispconfig/internal/datalog"
	"go-ispconfig/internal/model"
	"go-ispconfig/internal/repository"
	"go-ispconfig/internal/validator"
)

// registerClientEntities mounts the client and reseller CRUD entities and
// the extra client routes (port of interface/web/client + remote.d/
// client.inc.php). Both entities share the client table; the role
// discriminator is limit_client (0 = client, != 0 = reseller, design D2).
func registerClientEntities(g *echo.Group, d *Deps) error {
	if err := RegisterEntity[model.Client](g, d, clientEntity()); err != nil {
		return err
	}
	if err := RegisterEntity[model.Client](g, d, resellerEntity()); err != nil {
		return err
	}
	registerClientExtraRoutes(g, d)
	return nil
}

// clientEntity is the /api/clients CRUD surface (client.tform.php).
func clientEntity() *Entity {
	return &Entity{
		Name:  "clients",
		Title: "client_edit_title",
		Tabs:  clientTabs(false),
		ListScope: func(tx *gorm.DB) *gorm.DB {
			return tx.Where("limit_client = 0")
		},
		Prepare:      clientPrepare(false),
		AfterInsert:  clientAfterInsert,
		BeforeUpdate: clientBeforeUpdate,
		BeforeDelete: clientBeforeDelete,
		Decorate:     redactClientSecrets,
	}
}

// resellerEntity is the /api/resellers CRUD surface (reseller.tform.php).
// Resellers are managed by the admin only; the limit_client field is what
// makes the row a reseller and must stay non-zero.
func resellerEntity() *Entity {
	return &Entity{
		Name:      "resellers",
		Title:     "reseller_edit_title",
		AdminOnly: true,
		Tabs:      clientTabs(true),
		ListScope: func(tx *gorm.DB) *gorm.DB {
			return tx.Where("limit_client != 0")
		},
		Prepare:      clientPrepare(true),
		AfterInsert:  clientAfterInsert,
		BeforeUpdate: clientBeforeUpdate,
		BeforeDelete: clientBeforeDelete,
		Decorate:     redactClientSecrets,
	}
}

// intLimit is a numeric limit field (tform ISINT).
func intLimit(name string, def string) Field {
	return Field{Name: name, Label: name + "_txt", Datatype: "INTEGER", Formtype: "TEXT",
		Default: def,
		Validators: []validator.Rule{
			{Type: "ISINT", AllowEmpty: true, ErrKey: name + "_error_notint"},
		}}
}

// clientTabs builds the shared client/reseller tab set. The reseller
// variant adds limit_client (the role discriminator, reseller.tform.php);
// the client variant instead exposes parent_client_id (admin only).
func clientTabs(reseller bool) []Tab {
	address := Tab{
		Name:  "address",
		Label: "address_txt",
		Fields: []Field{
			text("company_name", "company_name_txt"),
			text("company_id", "company_id_txt"),
			selectField("gender", "gender_txt", "VARCHAR", "", []Option{
				{Value: "", Label: ""}, {Value: "m", Label: "gender_m_txt"}, {Value: "f", Label: "gender_f_txt"},
			}),
			text("contact_firstname", "contact_firstname_txt"),
			text("contact_name", "contact_name_txt",
				validator.Rule{Type: "NOTEMPTY", ErrKey: "contact_error_empty"}),
			text("customer_no", "customer_no_txt"),
			text("username", "username_txt",
				validator.Rule{Type: "NOTEMPTY", ErrKey: "username_error_empty"},
				validator.Rule{Type: "CUSTOM", ErrKey: "username_error_regex", Fn: checkClientUsername}),
			// The plaintext password is bcrypt-hashed in Prepare and never
			// echoed back (redacted in every response).
			text("password", "password_txt"),
			selectField("language", "language_txt", "VARCHAR", "en", nil),
			text("usertheme", "usertheme_txt"),
			text("street", "street_txt"),
			text("zip", "zip_txt"),
			text("city", "city_txt"),
			text("state", "state_txt"),
			text("country", "country_txt"),
			text("telephone", "telephone_txt"),
			text("mobile", "mobile_txt"),
			text("fax", "fax_txt"),
			text("email", "email_txt",
				validator.Rule{Type: "ISEMAIL", AllowEmpty: true, ErrKey: "email_error_isemail"}),
			text("internet", "internet_txt"),
			text("icq", "icq_txt"),
			text("vat_id", "vat_id_txt"),
			text("bank_account_owner", "bank_account_owner_txt"),
			text("bank_account_number", "bank_account_number_txt"),
			text("bank_code", "bank_code_txt"),
			text("bank_name", "bank_name_txt"),
			text("bank_account_iban", "bank_account_iban_txt"),
			text("bank_account_swift", "bank_account_swift_txt"),
			text("paypal_email", "paypal_email_txt",
				validator.Rule{Type: "ISEMAIL", AllowEmpty: true, ErrKey: "paypal_email_error_isemail"}),
			textarea("notes", "notes_txt"),
			checkbox("locked", "locked_txt", "n"),
			checkbox("canceled", "canceled_txt", "n"),
			checkbox("can_use_api", "can_use_api_txt", "n"),
			{Name: "template_master", Label: "template_master_txt",
				Datatype: "INTEGER", Formtype: "SELECT", Default: "0"},
		},
	}
	if !reseller {
		address.Fields = append(address.Fields, Field{
			Name: "parent_client_id", Label: "parent_client_id_txt",
			Datatype: "INTEGER", Formtype: "SELECT", Default: "0", AdminOnly: true,
		})
	}

	limits := Tab{
		Name:  "limits",
		Label: "limits_txt",
		Fields: []Field{
			// Mail (defaults per client table DDL; enforcement follows the
			// module that owns the counted table).
			intField("default_mailserver", "default_mailserver_txt", "1"),
			text("mail_servers", "mail_servers_txt"),
			intLimit("limit_maildomain", "-1"),
			intLimit("limit_mailbox", "-1"),
			intLimit("limit_mailalias", "-1"),
			intLimit("limit_mailaliasdomain", "-1"),
			intLimit("limit_mailmailinglist", "-1"),
			intLimit("limit_mailforward", "-1"),
			intLimit("limit_mailcatchall", "-1"),
			intLimit("limit_mailrouting", "0"),
			intLimit("limit_mail_wblist", "0"),
			intLimit("limit_mailfilter", "-1"),
			intLimit("limit_fetchmail", "-1"),
			intLimit("limit_mailquota", "-1"),
			intLimit("limit_spamfilter_wblist", "0"),
			intLimit("limit_spamfilter_user", "0"),
			intLimit("limit_spamfilter_policy", "0"),
			checkbox("limit_mail_backup", "limit_mail_backup_txt", "y"),
			checkbox("limit_relayhost", "limit_relayhost_txt", "n"),
			// XMPP
			intField("default_xmppserver", "default_xmppserver_txt", "1"),
			text("xmpp_servers", "xmpp_servers_txt"),
			intLimit("limit_xmpp_domain", "-1"),
			intLimit("limit_xmpp_user", "-1"),
			// Web
			intField("default_webserver", "default_webserver_txt", "1"),
			text("web_servers", "web_servers_txt"),
			intLimit("limit_web_domain", "-1"),
			intLimit("limit_web_quota", "-1"),
			text("web_php_options", "web_php_options_txt"),
			checkbox("limit_cgi", "limit_cgi_txt", "n"),
			checkbox("limit_ssi", "limit_ssi_txt", "n"),
			checkbox("limit_perl", "limit_perl_txt", "n"),
			checkbox("limit_ruby", "limit_ruby_txt", "n"),
			checkbox("limit_python", "limit_python_txt", "n"),
			checkbox("force_suexec", "force_suexec_txt", "y"),
			checkbox("limit_hterror", "limit_hterror_txt", "n"),
			checkbox("limit_wildcard", "limit_wildcard_txt", "n"),
			checkbox("limit_ssl", "limit_ssl_txt", "n"),
			checkbox("limit_ssl_letsencrypt", "limit_ssl_letsencrypt_txt", "n"),
			intLimit("limit_web_subdomain", "-1"),
			intLimit("limit_web_aliasdomain", "-1"),
			intLimit("limit_ftp_user", "-1"),
			intLimit("limit_shell_user", "0"),
			text("ssh_chroot", "ssh_chroot_txt"),
			intLimit("limit_webdav_user", "0"),
			checkbox("limit_backup", "limit_backup_txt", "y"),
			checkbox("limit_directive_snippets", "limit_directive_snippets_txt", "n"),
			intLimit("limit_traffic_quota", "-1"),
			// DNS
			intField("default_dnsserver", "default_dnsserver_txt", "1"),
			text("dns_servers", "dns_servers_txt"),
			intLimit("limit_dns_zone", "-1"),
			intField("default_slave_dnsserver", "default_slave_dnsserver_txt", "1"),
			intLimit("limit_dns_slave_zone", "-1"),
			intLimit("limit_dns_record", "-1"),
			// Database
			intField("default_dbserver", "default_dbserver_txt", "1"),
			text("db_servers", "db_servers_txt"),
			intLimit("limit_database", "-1"),
			intLimit("limit_database_user", "-1"),
			intLimit("limit_database_quota", "-1"),
			intLimit("limit_database_postgresql", "-1"),
			// Cron
			intLimit("limit_cron", "0"),
			selectField("limit_cron_type", "limit_cron_type_txt", "VARCHAR", "url", []Option{
				{Value: "url", Label: "URL Cron"}, {Value: "chrooted", Label: "Chrooted Cron"}, {Value: "full", Label: "Full Cron"},
			}),
			intLimit("limit_cron_frequency", "5"),
		},
	}
	if reseller {
		limits.Fields = append([]Field{
			intLimit("limit_client", "100"),
		}, limits.Fields...)
	}

	ip := Tab{
		Name:  "ip",
		Label: "ip_txt",
		Fields: []Field{
			text("limit_web_ip", "limit_web_ip_txt"),
		},
	}
	return []Tab{address, limits, ip}
}

// checkClientUsername ports the tform username REGEX (letters, digits,
// dot, dash, underscore, max 64).
func checkClientUsername(_ *validator.Context, value string) string {
	if value == "" {
		return "" // NOTEMPTY reports the empty case
	}
	if !clients.ValidUsername(value) {
		return "username_error_regex"
	}
	return ""
}

// clientPrepare returns the Prepare hook shared by both entities: hashes
// a submitted password (empty on update = unchanged), enforces username
// uniqueness across client and sys_user, and applies the reseller body
// rules (no parent, limit_client != 0).
func clientPrepare(reseller bool) func(c *echo.Context, d *Deps, id *repository.Identity, body map[string]any) error {
	return func(c *echo.Context, d *Deps, id *repository.Identity, body map[string]any) error {
		ctx := c.Request().Context()
		isUpdate := c.Request().Method == http.MethodPut

		pw, _ := body["password"].(string)
		switch {
		case pw == "" && isUpdate:
			delete(body, "password") // unchanged
		case pw == "":
			return &ValidationError{Fields: map[string][]string{"password": {"password_error_empty"}}}
		default:
			hash, err := auth.HashPassword(pw)
			if err != nil {
				return err
			}
			body["password"] = hash
		}

		if u, ok := body["username"].(string); ok && u != "" {
			var exclude uint32
			if isUpdate {
				n, _ := strconv.ParseUint(c.Param("id"), 10, 32)
				exclude = uint32(n)
			}
			taken, err := clients.UsernameTaken(ctx, d.DB, u, exclude)
			if err != nil {
				return err
			}
			if taken {
				return &ValidationError{Fields: map[string][]string{"username": {"username_error_unique"}}}
			}
		}

		if reseller {
			if _, ok := body["parent_client_id"]; ok && bodyInt(body, "parent_client_id") != 0 {
				// Nesting guard (design D2): a reseller never has a parent.
				return &ValidationError{Fields: map[string][]string{"parent_client_id": {"error.reseller_cannot_have_parent"}}}
			}
			if _, ok := body["limit_client"]; ok && bodyInt(body, "limit_client") == 0 {
				return &ValidationError{Fields: map[string][]string{"limit_client": {"limit_client_error_positive"}}}
			}
		}
		return nil
	}
}

// validateClientParent checks the parent reference of a client row:
// the parent must exist, be a reseller and not create reseller nesting.
func validateClientParent(ctx context.Context, tx *gorm.DB, c *model.Client) error {
	if c.ParentClientID == 0 {
		return nil
	}
	parent, err := clients.LoadParent(ctx, tx, c.ParentClientID)
	if err != nil {
		return &ValidationError{Fields: map[string][]string{"parent_client_id": {"error.parent_not_found"}}}
	}
	if err := clients.CheckParent(parent, clients.IsReseller(c)); err != nil {
		key := "error.parent_not_reseller"
		if errors.Is(err, clients.ErrNestedReseller) {
			key = "error.reseller_cannot_have_parent"
		}
		return &ValidationError{Fields: map[string][]string{"parent_client_id": {key}}}
	}
	return nil
}

// clientAfterInsert provisions the login identity (sys_group + sys_user)
// and materializes limit templates inside the insert transaction. A
// non-admin creator (reseller) always becomes the parent of the new
// client (client_edit.php onAfterInsert parity).
func clientAfterInsert(ctx context.Context, tx *gorm.DB, id *repository.Identity, rec any) error {
	c := rec.(*model.Client)
	actor := "admin"
	if id != nil {
		actor = id.Username
	}
	if id != nil && !id.IsAdmin() {
		var u model.SysUser
		err := tx.WithContext(ctx).Select("client_id").Where("userid = ?", id.UserID).Take(&u).Error
		if err != nil || u.ClientID == 0 {
			return repository.ErrPermissionDenied
		}
		c.ParentClientID = u.ClientID
		err = tx.WithContext(ctx).Model(&model.Client{}).Where("client_id = ?", c.ClientID).
			Update("parent_client_id", u.ClientID).Error
		if err != nil {
			return err
		}
	}
	if err := validateClientParent(ctx, tx, c); err != nil {
		return err
	}
	if err := clients.ProvisionIdentity(ctx, tx, c, c.Password, actor); err != nil {
		return err
	}
	return clients.ApplyTemplates(ctx, tx, c)
}

// clientBeforeUpdate re-materializes templates, caps the limits to the
// parent reseller and syncs the login identity — all inside the update
// transaction, before the row is written (so the datalog diff carries the
// final limits).
func clientBeforeUpdate(ctx context.Context, tx *gorm.DB, id *repository.Identity, body map[string]any, old, rec any) error {
	o := old.(*model.Client)
	n := rec.(*model.Client)
	if n.ParentClientID != o.ParentClientID {
		if err := validateClientParent(ctx, tx, n); err != nil {
			return err
		}
	}
	if err := clients.MaterializeInto(ctx, tx, n); err != nil {
		return err
	}
	if n.ParentClientID != 0 {
		parent, err := clients.LoadParent(ctx, tx, n.ParentClientID)
		if err != nil {
			return err
		}
		if _, err := clients.CapToParent(n, parent); err != nil {
			return err
		}
	}
	newHash := ""
	if n.Password != o.Password {
		newHash = n.Password
	}
	return clients.SyncIdentity(ctx, tx, o, n, newHash)
}

// clientBeforeDelete removes the login identity, group memberships and
// template assignments. A reseller with child clients cannot be deleted
// (client_del.php parity).
func clientBeforeDelete(ctx context.Context, tx *gorm.DB, id *repository.Identity, rec any) error {
	c := rec.(*model.Client)
	actor := "admin"
	if id != nil {
		actor = id.Username
	}
	var children int64
	err := tx.WithContext(ctx).Model(&model.Client{}).
		Where("parent_client_id = ?", c.ClientID).Count(&children).Error
	if err != nil {
		return err
	}
	if children > 0 {
		return &ValidationError{Fields: map[string][]string{"client_id": {"error.client_has_children"}}}
	}
	return clients.DeprovisionIdentity(ctx, tx, c, actor)
}

// redactClientSecrets removes credential and key material from every
// client JSON response (password hash, ssh keys, tmp blob) — spec:
// password fields never appear in list/get output.
func redactClientSecrets(_ context.Context, _ *gorm.DB, items []map[string]any) error {
	for _, it := range items {
		delete(it, "password")
		delete(it, "tmp_data")
		delete(it, "id_rsa")
		delete(it, "ssh_rsa")
	}
	return nil
}

// --- extra routes (remote.d/client.inc.php helpers) ---

// registerClientExtraRoutes mounts the client lookup helpers, the
// change-password endpoint and delete-everything.
func registerClientExtraRoutes(g *echo.Group, d *Deps) {
	g.GET("/clients/by-username/:username", func(c *echo.Context) error {
		return clientLookup(c, d, "username = ?", c.Param("username"))
	})
	g.GET("/clients/by-customer-no/:no", func(c *echo.Context) error {
		return clientLookup(c, d, "customer_no = ?", c.Param("no"))
	})
	g.GET("/clients/by-groupid/:groupid", func(c *echo.Context) error {
		var grp model.SysGroup
		err := d.DB.WithContext(c.Request().Context()).
			Where("groupid = ?", c.Param("groupid")).Take(&grp).Error
		if err != nil {
			return err
		}
		return clientLookup(c, d, "client_id = ?", grp.ClientID)
	})
	g.GET("/clients/id-by-sysuser/:userid", func(c *echo.Context) error {
		// remote client_get_id: client id of an interface sys_user.
		var u model.SysUser
		err := d.DB.WithContext(c.Request().Context()).Select("client_id").
			Where("userid = ?", c.Param("userid")).Take(&u).Error
		if err != nil {
			return err
		}
		if u.ClientID == 0 {
			return gorm.ErrRecordNotFound
		}
		return c.JSON(http.StatusOK, map[string]any{"client_id": u.ClientID})
	})
	g.POST("/clients/:id/change-password", func(c *echo.Context) error {
		return clientChangePassword(c, d)
	})
	g.DELETE("/clients/:id/everything", requireAdmin(func(c *echo.Context) error {
		return clientDeleteEverything(c, d)
	}))
}

// clientLookup loads one client row under the caller's read scope and
// returns it redacted; inaccessible and missing rows are both 404.
func clientLookup(c *echo.Context, d *Deps, where string, arg any) error {
	id := identity(c)
	var row model.Client
	err := d.DB.WithContext(c.Request().Context()).Model(&model.Client{}).
		Scopes(repository.WithPerm(id, repository.PermRead)).
		Where(where, arg).Take(&row).Error
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, clientJSON(c.Request().Context(), d, &row))
}

// clientJSON renders a client row as the same column-keyed map the entity
// framework produces, with secrets redacted.
func clientJSON(ctx context.Context, d *Deps, row *model.Client) map[string]any {
	s, err := schema.Parse(&model.Client{}, entitySchemaCache, d.DB.NamingStrategy)
	if err != nil {
		return nil
	}
	out := make(map[string]any, len(s.Fields))
	rv := reflect.ValueOf(row)
	for _, f := range s.Fields {
		v, _ := f.ValueOf(ctx, rv)
		out[f.DBName] = v
	}
	_ = redactClientSecrets(ctx, nil, []map[string]any{out})
	return out
}

// changePasswordBody is the change-password request payload.
type changePasswordBody struct {
	Password string `json:"password"`
}

// minClientPasswordLen is the password policy floor for the dedicated
// change-password endpoint.
const minClientPasswordLen = 8

// clientChangePassword sets a new login password on client.password and
// sys_user.passwort (remote client_change_password): update-scoped, so
// only the admin or the owning reseller may call it.
func clientChangePassword(c *echo.Context, d *Deps) error {
	id := identity(c)
	ctx := c.Request().Context()
	var body changePasswordBody
	if err := c.Bind(&body); err != nil {
		return err
	}
	if len(body.Password) < minClientPasswordLen {
		return &ValidationError{Fields: map[string][]string{"password": {"password_error_length"}}}
	}
	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		return err
	}
	err = d.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row model.Client
		err := tx.Scopes(repository.WithPerm(id, repository.PermUpdate)).
			Where("client_id = ?", c.Param("id")).Take(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return repository.ErrPermissionDenied
		}
		if err != nil {
			return err
		}
		if err := tx.Model(&model.Client{}).Where("client_id = ?", row.ClientID).
			Update("password", hash).Error; err != nil {
			return err
		}
		updated := row
		updated.Password = hash
		return clients.SyncIdentity(ctx, tx, &row, &updated, hash)
	})
	if err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

// clientDeleteEverything removes a client together with every resource
// its group owns (web domains, folders, dns zones/records/slaves), each
// journaled to sys_datalog so the daemons tear the resources down —
// remote client_delete_everything. Admin only.
func clientDeleteEverything(c *echo.Context, d *Deps) error {
	id := identity(c)
	ctx, flush := datalog.NotifyAfterCommit(c.Request().Context())
	err := d.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row model.Client
		err := tx.Where("client_id = ?", c.Param("id")).Take(&row).Error
		if err != nil {
			return err
		}
		var grp model.SysGroup
		if err := tx.Where("client_id = ?", row.ClientID).Take(&grp).Error; err != nil &&
			!errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if grp.GroupID != 0 {
			// Children before parents so daemon teardown order is sane.
			if err := purgeOwned[model.WebFolderUser](ctx, tx, grp.GroupID, id.Username); err != nil {
				return err
			}
			if err := purgeOwned[model.WebFolder](ctx, tx, grp.GroupID, id.Username); err != nil {
				return err
			}
			if err := purgeOwned[model.WebDomain](ctx, tx, grp.GroupID, id.Username); err != nil {
				return err
			}
			if err := purgeOwned[model.DNSRr](ctx, tx, grp.GroupID, id.Username); err != nil {
				return err
			}
			if err := purgeOwned[model.DNSSoa](ctx, tx, grp.GroupID, id.Username); err != nil {
				return err
			}
			if err := purgeOwned[model.DNSSlave](ctx, tx, grp.GroupID, id.Username); err != nil {
				return err
			}
		}
		if err := clientBeforeDelete(ctx, tx, id, &row); err != nil {
			return err
		}
		if err := tx.Delete(&model.Client{}, row.ClientID).Error; err != nil {
			return err
		}
		return datalog.LogDelete(tx, &row, id.Username)
	})
	if err != nil {
		return err
	}
	flush()
	return c.NoContent(http.StatusNoContent)
}

// purgeOwned deletes every row of T owned by the group, one datalog
// delete per row (the daemons replay them like manual deletes).
func purgeOwned[T any](ctx context.Context, tx *gorm.DB, groupID uint32, username string) error {
	var rows []T
	if err := tx.WithContext(ctx).Where("sys_groupid = ?", groupID).Find(&rows).Error; err != nil {
		return err
	}
	for i := range rows {
		if err := tx.WithContext(ctx).Where("sys_groupid = ?", groupID).Delete(&rows[i]).Error; err != nil {
			return err
		}
		if err := datalog.LogDelete(tx, &rows[i], username); err != nil {
			return err
		}
	}
	return nil
}
