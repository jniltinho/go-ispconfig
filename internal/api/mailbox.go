package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v5"
	"golang.org/x/net/idna"
	"gorm.io/gorm"

	"go-ispconfig/internal/getconf"
	"go-ispconfig/internal/model"
	"go-ispconfig/internal/repository"
	"go-ispconfig/internal/validator"
)

// mailboxEntity is the /api/mail/mailboxes CRUD surface
// (mail_user.tform.php).
func mailboxEntity(d *Deps) *Entity {
	return &Entity{
		Name:  "mailboxes",
		Title: "mailuser_edit_title",
		Tabs: []Tab{
			{
				Name:  "mailuser",
				Label: "mailbox_txt",
				Fields: []Field{
					clientGroupField(),
					text("email", "email_txt",
						validator.Rule{Type: "NOTEMPTY", ErrKey: "email_error_empty"},
						validator.Rule{Type: "ISEMAIL", ErrKey: "email_error_isemail"},
						validator.Rule{Type: "UNIQUE", ErrKey: "email_error_unique"}),
					// Plaintext on write, CRYPTMAIL-hashed in Prepare, never
					// echoed back.
					{Name: "password", Label: "password_txt", Datatype: "VARCHAR", Formtype: "PASSWORD"},
					text("name", "name_txt"),
					text("login", "login_txt"),
					intField("quota", "quota_txt", "0",
						validator.Rule{Type: "ISINT", AllowEmpty: true, ErrKey: "quota_error_isint"}),
					text("cc", "cc_txt"),
					// PHP mail_user_mailbox_edit.htm places forward_in_lda on the
					// Mailbox tab between CC and BCC (not on Mail Filter).
					checkbox("forward_in_lda", "forward_in_lda_txt", "n"),
					text("sender_cc", "sender_cc_txt"),
					{Name: "imap_prefix", Label: "imap_prefix_txt", Datatype: "VARCHAR", Formtype: "TEXT", AdminOnly: true},
					checkbox("postfix", "postfix_txt", "y"),
					checkbox("access", "access_txt", "y"),
					checkbox("greylisting", "greylisting_txt", "n"),
					checkbox("disableimap", "disableimap_txt", "n"),
					checkbox("disablepop3", "disablepop3_txt", "n"),
					checkbox("disablesmtp", "disablesmtp_txt", "n"),
					checkbox("disabledeliver", "disabledeliver_txt", "n"),
					checkbox("disablesieve", "disablesieve_txt", "n"),
					checkbox("disablelmtp", "disablelmtp_txt", "n"),
					// Server-derived columns (set in Prepare on create,
					// protected from client tampering on update).
					intField("server_id", "server_id_txt", "0"),
					text("maildir", "maildir_txt"),
					text("maildir_format", "maildir_format_txt"),
					text("homedir", "homedir_txt"),
					intField("uid", "uid_txt", "5000"),
					intField("gid", "gid_txt", "5000"),
				},
			},
			{
				Name:  "autoresponder",
				Label: "autoresponder_txt",
				Fields: []Field{
					checkbox("autoresponder", "autoresponder_txt", "n"),
					text("autoresponder_subject", "autoresponder_subject_txt"),
					textarea("autoresponder_text", "autoresponder_text_txt"),
					text("autoresponder_start_date", "autoresponder_start_date_txt"),
					text("autoresponder_end_date", "autoresponder_end_date_txt"),
				},
			},
			{
				// PHP tab title "Mail Filter" (filter_records) — junk/move + purge;
				// nested mail_user_filter rule list is a follow-up (no REST yet).
				Name:  "filter_records",
				Label: "mail_filter_txt",
				Fields: []Field{
					selectField("move_junk", "move_junk_txt", "VARCHAR", "y", []Option{
						{Value: "y", Label: "move_junk_before_txt"},
						{Value: "a", Label: "move_junk_after_txt"},
						{Value: "n", Label: "no_txt"},
					}),
					intField("purge_trash_days", "purge_trash_days_txt", "0"),
					intField("purge_junk_days", "purge_junk_days_txt", "0"),
				},
			},
			{
				// PHP: Custom Rules tab only for admin (mail_user.tform.php).
				Name:      "mailfilter",
				Label:     "custom_rules_txt",
				AdminOnly: true,
				Fields: []Field{
					textarea("custom_mailfilter", "custom_mailfilter_txt"),
				},
			},
			{
				// PHP tab "Backup" when backup is available.
				Name:  "backup",
				Label: "backup_tab_txt",
				Fields: []Field{
					selectField("backup_interval", "backup_interval_txt", "VARCHAR", "none", []Option{
						{Value: "none", Label: "no_backup_txt"},
						{Value: "daily", Label: "daily_backup_txt"},
						{Value: "weekly", Label: "weekly_backup_txt"},
						{Value: "monthly", Label: "monthly_backup_txt"},
					}),
					selectField("backup_copies", "backup_copies_txt", "INTEGER", "1", []Option{
						{Value: "1", Label: "1"}, {Value: "2", Label: "2"}, {Value: "3", Label: "3"},
						{Value: "4", Label: "4"}, {Value: "5", Label: "5"}, {Value: "6", Label: "6"},
						{Value: "7", Label: "7"}, {Value: "8", Label: "8"}, {Value: "9", Label: "9"},
						{Value: "10", Label: "10"}, {Value: "15", Label: "15"}, {Value: "20", Label: "20"},
						{Value: "30", Label: "30"},
					}),
				},
			},
		},
		Prepare:  mailboxPrepare,
		Decorate: mailboxDecorate(),
	}
}

// mailboxPrepare validates the email, verifies the domain part exists as
// a primary mail_domain (not only an aliasdomain), CRYPTMAIL-hashes a
// supplied password (empty on update = unchanged), and derives
// maildir/homedir/uid/gid from the domain's server mail getconf
// (mail_user_edit onBeforeInsert parity).
func mailboxPrepare(c *echo.Context, d *Deps, _ *repository.Identity, body map[string]any) error {
	ctx := c.Request().Context()
	isUpdate := c.Request().Method == http.MethodPut

	// Server-owned columns the client may never set directly. maildir/
	// homedir/server_id are re-derived below from the email when it
	// changes, so they are dropped from the raw body first either way.
	if isUpdate {
		for _, k := range []string{"maildir", "maildir_format", "homedir", "server_id", "uid", "gid"} {
			delete(body, k)
		}
	}

	pw, _ := body["password"].(string)
	switch {
	case pw == "" && isUpdate:
		delete(body, "password")
	case pw == "" && !isUpdate:
		return &ValidationError{Fields: map[string][]string{"password": {"password_error_empty"}}}
	default:
		if err := cryptBodyPassword(body, "password"); err != nil {
			return err
		}
	}

	email, _ := body["email"].(string)
	if email != "" {
		email = strings.ToLower(email)
		if local, domain, ok := strings.Cut(email, "@"); ok {
			if ascii, err := idna.Lookup.ToASCII(domain); err == nil {
				domain = ascii
			}
			email = local + "@" + domain
		}
		body["email"] = email
	}

	// Create always derives the server-owned columns; update re-derives
	// them only when the email (and thus maildir) changes — PHP
	// mail_user_edit recomputes maildir on every submit.
	if !isUpdate || email != "" {
		if err := deriveMailboxColumns(ctx, d, email, body, isUpdate); err != nil {
			return err
		}
	}
	return nil
}

// deriveMailboxColumns looks up the primary mail_domain of the email and
// fills maildir/maildir_format/homedir/server_id/uid/gid (and login on
// create). On update it leaves uid/gid/format untouched (the daemon
// forbids format changes) but refreshes the path for an email rename.
func deriveMailboxColumns(ctx context.Context, d *Deps, email string, body map[string]any, isUpdate bool) error {
	local, domain, ok := strings.Cut(email, "@")
	if !ok || local == "" || domain == "" {
		return &ValidationError{Fields: map[string][]string{"email": {"email_error_isemail"}}}
	}
	var md model.MailDomain
	if err := d.DB.WithContext(ctx).Where("domain = ?", domain).Take(&md).Error; err != nil {
		return &ValidationError{Fields: map[string][]string{"email": {"mail_domain_does_not_exist"}}}
	}
	mailCfg := getconf.DefaultMailConfig()
	if sc, err := getconf.GetServerConfig(d.DB, md.ServerID); err == nil {
		mailCfg = sc.Mail
	}
	maildir := strings.ReplaceAll(mailCfg.MaildirPath, "[domain]", domain)
	maildir = strings.ReplaceAll(maildir, "[localpart]", local)
	body["maildir"] = maildir
	body["homedir"] = mailCfg.HomedirPath
	body["server_id"] = md.ServerID
	if isUpdate {
		return nil
	}
	if body["login"] == nil || body["login"] == "" {
		body["login"] = email
	}
	body["maildir_format"] = mailCfg.MaildirFormat
	if mailCfg.MailboxVirtualUidgidMaps == "y" {
		body["uid"], body["gid"] = -1, -1
	} else {
		body["uid"], _ = strconv.Atoi(mailCfg.MailuserUID)
		body["gid"], _ = strconv.Atoi(mailCfg.MailuserGID)
	}
	return nil
}

// registerMailboxRoutes mounts the mailbox list-by-client helper.
func registerMailboxRoutes(g *echo.Group, d *Deps) {
	g.GET("/mailboxes/by-client/:client_id", func(c *echo.Context) error {
		id := identity(c)
		var grp model.SysGroup
		err := d.DB.WithContext(c.Request().Context()).
			Where("client_id = ?", c.Param("client_id")).Take(&grp).Error
		if err != nil {
			return err
		}
		var rows []model.MailUser
		err = d.DB.WithContext(c.Request().Context()).Model(&model.MailUser{}).
			Scopes(repository.WithPerm(id, repository.PermRead)).
			Where("sys_groupid = ?", grp.GroupID).Find(&rows).Error
		if err != nil {
			return err
		}
		out := make([]map[string]any, 0, len(rows))
		for i := range rows {
			out = append(out, redactMailbox(&rows[i]))
		}
		return c.JSON(http.StatusOK, out)
	})
}

// redactMailbox renders a mailbox row without the password hash.
func redactMailbox(m *model.MailUser) map[string]any {
	return map[string]any{
		"mailuser_id": m.MailuserID, "server_id": m.ServerID,
		"email": m.Email, "login": m.Login, "name": m.Name,
		"quota": m.Quota, "maildir": m.Maildir, "active": "y",
		"postfix": m.Postfix, "access": m.Access, "greylisting": m.Greylisting,
		"autoresponder": m.Autoresponder, "move_junk": m.MoveJunk,
		"sys_userid": m.SysUserID, "sys_groupid": m.SysGroupID,
	}
}

// mailboxDecorate strips the password from every response and adds the
// datalog state.
func mailboxDecorate() func(ctx context.Context, db *gorm.DB, items []map[string]any) error {
	state := datalogStateDecorator("mail_user", "mailuser_id")
	return func(ctx context.Context, db *gorm.DB, items []map[string]any) error {
		for _, it := range items {
			delete(it, "password")
		}
		return state(ctx, db, items)
	}
}
