package api

import (
	"context"
	"net/http"
	"net/netip"
	"strings"

	"github.com/labstack/echo/v5"
	"gorm.io/gorm"

	"golang.org/x/net/idna"

	"go-ispconfig/internal/model"
	"go-ispconfig/internal/repository"
	"go-ispconfig/internal/validator"
)

// fetchmailTypes are the four retrievers ISPConfig offers
// (mail_get.tform.php); the daemon maps them to getmail classes.
func fetchmailTypes() []Option {
	return []Option{
		{Value: "pop3", Label: "pop3_txt"},
		{Value: "imap", Label: "imap_txt"},
		{Value: "pop3ssl", Label: "pop3ssl_txt"},
		{Value: "imapssl", Label: "imapssl_txt"},
	}
}

// registerFetchmailEntity mounts /api/mail/fetchmail (port of
// remote.d/mail.inc.php mail_fetchmail_get|add|update|delete).
func registerFetchmailEntity(g *echo.Group, d *Deps) error {
	return RegisterEntity[model.MailGet](g, d, fetchmailEntity())
}

// fetchmailEntity is the mail_get CRUD surface (mail_get.tform.php).
func fetchmailEntity() *Entity {
	return &Entity{
		Name:  "fetchmail",
		Title: "mail_get_edit_title",
		Tabs: []Tab{{
			Name:  "mailget",
			Label: "mailget_txt",
			Fields: []Field{
				clientGroupField(),
				selectField("type", "type_txt", "VARCHAR", "pop3", fetchmailTypes(),
					validator.Rule{Type: "CUSTOM", ErrKey: "type_error_regex", Fn: checkFetchmailType}),
				text("source_server", "source_server_txt",
					validator.Rule{Type: "NOTEMPTY", ErrKey: "source_server_error_isempty"},
					validator.Rule{Type: "CUSTOM", ErrKey: "source_server_error_regex", Fn: checkIsHostOrIP}),
				text("source_username", "source_username_txt",
					validator.Rule{Type: "NOTEMPTY", ErrKey: "source_username_error_isempty"}),
				{
					Name: "source_password", Label: "source_password_txt",
					Datatype: "VARCHAR", Formtype: "PASSWORD",
					// varchar(64): reject rather than truncate silently
					// (the PHP form declares 255 against a 64-char column).
					Validators: []validator.Rule{
						{Type: "CUSTOM", ErrKey: "source_password_error_length", Fn: checkFetchmailPassword},
					},
				},
				text("destination", "destination_txt",
					validator.Rule{Type: "NOTEMPTY", ErrKey: "destination_error_isempty"},
					validator.Rule{Type: "ISEMAIL", ErrKey: "destination_error_isemail"}),
				checkbox("source_delete", "source_delete_txt", "y"),
				checkbox("source_read_all", "source_read_all_txt", "n"),
				checkbox("active", "active_txt", "y"),
			},
		}},
		Prepare: fetchmailPrepare,
		// source_password is write-only: never returned by list or get.
		Decorate: fetchmailDecorate(),
	}
}

// checkFetchmailType restricts the retriever to the four PHP offers.
func checkFetchmailType(_ *validator.Context, value string) string {
	switch value {
	case "pop3", "imap", "pop3ssl", "imapssl":
		return ""
	}
	return "type_error_regex"
}

// checkFetchmailPassword enforces the real varchar(64) column width
// (the PHP form declares 255 and truncates silently).
func checkFetchmailPassword(_ *validator.Context, value string) string {
	if len(value) > 64 {
		return "source_password_error_length"
	}
	return ""
}

// checkIsHostOrIP accepts a hostname or a bare IP address for the
// remote server (mail_get.tform.php REGEX parity, IDN-tolerant).
func checkIsHostOrIP(_ *validator.Context, value string) string {
	if value == "" {
		return ""
	}
	if _, err := netip.ParseAddr(value); err == nil {
		return ""
	}
	ascii, err := idna.Lookup.ToASCII(strings.TrimSuffix(value, "."))
	if err != nil || !strings.Contains(ascii, ".") {
		return "source_server_error_regex"
	}
	return ""
}

// fetchmailPrepare ports mail_get_edit.php::onSubmit: strip markup from
// the username, refuse the delete/read_all combination that re-downloads
// the whole remote mailbox every run, and derive server_id/sys_groupid
// from a destination mailbox the caller may see.
func fetchmailPrepare(c *echo.Context, d *Deps, id *repository.Identity, body map[string]any) error {
	ctx := c.Request().Context()
	isUpdate := c.Request().Method == http.MethodPut

	if u, ok := body["source_username"].(string); ok {
		body["source_username"] = stripMarkup(u)
	}
	// An empty password on update keeps the stored one; on create the
	// field is required (getmail cannot authenticate without it).
	if pw, ok := body["source_password"].(string); ok && pw == "" {
		if !isUpdate {
			return &ValidationError{Fields: map[string][]string{
				"source_password": {"source_password_error_isempty"}}}
		}
		delete(body, "source_password")
	}
	if body["source_delete"] != nil && body["source_delete"] != "y" && body["source_read_all"] == "y" {
		return &ValidationError{Fields: map[string][]string{
			"source_read_all": {"error_delete_read_all_combination"}}}
	}

	// server_id is always derived; a client-supplied value is ignored.
	delete(body, "server_id")
	dest, _ := body["destination"].(string)
	if dest == "" {
		if isUpdate {
			return nil
		}
		return &ValidationError{Fields: map[string][]string{"destination": {"destination_error_isempty"}}}
	}
	dest = strings.ToLower(dest)
	body["destination"] = dest

	var mb model.MailUser
	if err := d.DB.WithContext(ctx).Where("email = ?", dest).Take(&mb).Error; err != nil {
		return &ValidationError{Fields: map[string][]string{"destination": {"no_destination_perm"}}}
	}
	if !id.IsAdmin() && !id.InGroup(mb.SysGroupID) {
		return &ValidationError{Fields: map[string][]string{"destination": {"no_destination_perm"}}}
	}
	body["server_id"] = mb.ServerID
	// PHP copies the mailbox ownership onto the row (onAfterInsert).
	body["sys_groupid"] = mb.SysGroupID
	return nil
}

// stripMarkup removes HTML tags and newlines from a submitted username
// (tform's striptags + nl strip).
func stripMarkup(s string) string {
	s = strings.NewReplacer("\r", "", "\n", "").Replace(s)
	for {
		open := strings.IndexByte(s, '<')
		if open < 0 {
			return s
		}
		close := strings.IndexByte(s[open:], '>')
		if close < 0 {
			return s[:open]
		}
		s = s[:open] + s[open+close+1:]
	}
}

// fetchmailDecorate strips the cleartext password from every response
// and adds the datalog state badge.
func fetchmailDecorate() func(ctx context.Context, db *gorm.DB, items []map[string]any) error {
	state := datalogStateDecorator("mail_get", "mailget_id")
	return func(ctx context.Context, db *gorm.DB, items []map[string]any) error {
		for _, it := range items {
			delete(it, "source_password")
		}
		return state(ctx, db, items)
	}
}
