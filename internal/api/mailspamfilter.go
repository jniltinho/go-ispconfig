package api

import (
	"github.com/labstack/echo/v5"

	"go-ispconfig/internal/model"
	"go-ispconfig/internal/validator"
)

// registerSpamfilterEntities mounts the mail_access and spamfilter CRUD
// surfaces (remote mail_whitelist/blacklist, mail_policy,
// mail_spamfilter_user, mail_spamfilter_whitelist/blacklist).
func registerSpamfilterEntities(g *echo.Group, d *Deps) error {
	if err := RegisterEntity[model.MailAccess](g, d, mailAccessEntity()); err != nil {
		return err
	}
	sg := g.Group("/spamfilter")
	if err := RegisterEntity[model.SpamfilterPolicy](sg, d, spamfilterPolicyEntity()); err != nil {
		return err
	}
	if err := RegisterEntity[model.SpamfilterUser](sg, d, spamfilterUserEntity()); err != nil {
		return err
	}
	return RegisterEntity[model.SpamfilterWblist](sg, d, spamfilterWblistEntity())
}

// mailAccessEntity is /api/mail/access (mail_access): global Postfix
// access / Rspamd wblist rows, unique (server_id, source, type).
func mailAccessEntity() *Entity {
	return &Entity{
		Name:  "access",
		Title: "mail_access_edit_title",
		Tabs: []Tab{{
			Name:  "access",
			Label: "access_txt",
			Fields: []Field{
				intField("server_id", "server_id_txt", "0"),
				text("source", "source_txt",
					validator.Rule{Type: "NOTEMPTY", ErrKey: "source_error_empty"}),
				text("access", "access_txt",
					validator.Rule{Type: "NOTEMPTY", ErrKey: "access_error_empty"}),
				selectField("type", "type_txt", "VARCHAR", "recipient", []Option{
					{Value: "recipient", Label: "recipient_txt"},
					{Value: "sender", Label: "sender_txt"},
					{Value: "client", Label: "client_txt"},
				}),
				checkbox("active", "active_txt", "y"),
			},
		}},
		Decorate: datalogStateDecorator("mail_access", "access_id"),
	}
}

// spamfilterPolicyEntity is /api/mail/spamfilter/policies
// (spamfilter_policy): admin catalog, no daemon hook (design D2).
func spamfilterPolicyEntity() *Entity {
	return &Entity{
		Name:      "policies",
		Title:     "spamfilter_policy_edit_title",
		AdminOnly: true,
		Tabs: []Tab{{
			Name:  "policy",
			Label: "policy_txt",
			Fields: []Field{
				text("policy_name", "policy_name_txt",
					validator.Rule{Type: "NOTEMPTY", ErrKey: "policy_name_error_empty"}),
				checkbox("rspamd_greylisting", "rspamd_greylisting_txt", "n"),
				selectField("rspamd_spam_tag_method", "rspamd_spam_tag_method_txt", "VARCHAR", "rewrite_subject", []Option{
					{Value: "add_header", Label: "add_header_txt"},
					{Value: "rewrite_subject", Label: "rewrite_subject_txt"},
				}),
				{Name: "rspamd_spam_tag_level", Label: "rspamd_spam_tag_level_txt", Datatype: "DOUBLE", Formtype: "TEXT"},
				{Name: "rspamd_spam_kill_level", Label: "rspamd_spam_kill_level_txt", Datatype: "DOUBLE", Formtype: "TEXT"},
				{Name: "rspamd_spam_greylisting_level", Label: "rspamd_spam_greylisting_level_txt", Datatype: "DOUBLE", Formtype: "TEXT"},
			},
		}},
	}
}

// spamfilterUserEntity is /api/mail/spamfilter/users (spamfilter_users):
// per-identity policy assignment, unique email.
func spamfilterUserEntity() *Entity {
	return &Entity{
		Name:  "users",
		Title: "spamfilter_user_edit_title",
		Tabs: []Tab{{
			Name:  "user",
			Label: "user_txt",
			Fields: []Field{
				clientGroupField(),
				intField("server_id", "server_id_txt", "0"),
				text("email", "email_txt",
					validator.Rule{Type: "NOTEMPTY", ErrKey: "email_error_empty"},
					validator.Rule{Type: "UNIQUE", ErrKey: "email_error_unique"}),
				text("fullname", "fullname_txt"),
				intField("policy_id", "policy_id_txt", "0"),
				intField("priority", "priority_txt", "7"),
				{Name: "local", Label: "local_txt", Datatype: "VARCHAR", Formtype: "TEXT", Default: "Y"},
			},
		}},
		Decorate: datalogStateDecorator("spamfilter_users", "id"),
	}
}

// spamfilterWblistEntity is /api/mail/spamfilter/wblists
// (spamfilter_wblist): white/black entries tied to a spamfilter user.
func spamfilterWblistEntity() *Entity {
	return &Entity{
		Name:  "wblists",
		Title: "spamfilter_wblist_edit_title",
		Tabs: []Tab{{
			Name:  "wblist",
			Label: "wblist_txt",
			Fields: []Field{
				clientGroupField(),
				intField("server_id", "server_id_txt", "0"),
				selectField("wb", "wb_txt", "VARCHAR", "W", []Option{
					{Value: "W", Label: "whitelist_txt"},
					{Value: "B", Label: "blacklist_txt"},
				}),
				intField("rid", "rid_txt", "0",
					validator.Rule{Type: "ISPOSITIVE", ErrKey: "rid_error_empty"}),
				text("email", "email_txt",
					validator.Rule{Type: "NOTEMPTY", ErrKey: "email_error_empty"}),
				intField("priority", "priority_txt", "0"),
				checkbox("active", "active_txt", "y"),
			},
		}},
		Decorate: datalogStateDecorator("spamfilter_wblist", "wblist_id"),
	}
}
