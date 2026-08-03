package api

import (
	"github.com/labstack/echo/v5"
	"gorm.io/gorm"

	"go-ispconfig/internal/model"
	"go-ispconfig/internal/repository"
	"go-ispconfig/internal/validator"
)

// forwardingType is a mail_forwarding discriminator with its API surface
// name (each surface forces its type server-side).
type forwardingType struct {
	name  string // route + entity name
	typ   string // mail_forwarding.type value
	title string
	// sourceEmail/destEmail control whether ISEMAIL is enforced (alias/
	// forward use email forms; aliasdomain/catchall use @domain forms).
	sourceEmail bool
	destEmail   bool
}

var forwardingTypes = []forwardingType{
	{"aliases", "alias", "mail_alias_edit_title", true, true},
	{"forwards", "forward", "mail_forward_edit_title", true, true},
	{"catchalls", "catchall", "mail_catchall_edit_title", false, true},
	{"alias-domains", "aliasdomain", "mail_aliasdomain_edit_title", false, false},
}

// registerForwardingEntities mounts the four mail_forwarding surfaces.
func registerForwardingEntities(g *echo.Group, d *Deps) error {
	for _, ft := range forwardingTypes {
		if err := RegisterEntity[model.MailForwarding](g, d, forwardingEntity(ft)); err != nil {
			return err
		}
	}
	return nil
}

// forwardingEntity builds one typed mail_forwarding surface.
func forwardingEntity(ft forwardingType) *Entity {
	sourceRules := []validator.Rule{{Type: "NOTEMPTY", ErrKey: "source_error_empty"}}
	if ft.sourceEmail {
		sourceRules = append(sourceRules, validator.Rule{Type: "ISEMAIL", ErrKey: "email_error_isemail"})
	}
	destRules := []validator.Rule{{Type: "NOTEMPTY", ErrKey: "destination_error_empty"}}
	if ft.destEmail {
		destRules = append(destRules, validator.Rule{Type: "ISEMAIL", ErrKey: "destination_error_isemail"})
	}
	typ := ft.typ
	return &Entity{
		Name:  ft.name,
		Title: ft.title,
		Tabs: []Tab{{
			Name:  "forwarding",
			Label: "forwarding_txt",
			Fields: []Field{
				clientGroupField(),
				intField("server_id", "server_id_txt", "0"),
				text("source", "source_txt", sourceRules...),
				textarea("destination", "destination_txt", destRules...),
				checkbox("active", "active_txt", "y"),
				checkbox("allow_send_as", "allow_send_as_txt", "n"),
				checkbox("greylisting", "greylisting_txt", "n"),
			},
		}},
		// The type is forced server-side (spec: each surface forces its
		// mail_forwarding.type), and the list is filtered by it.
		ListScope: forwardingListScope(typ),
		Guard:     forwardingGuard(typ),
		Prepare: func(_ *echo.Context, _ *Deps, _ *repository.Identity, body map[string]any) error {
			body["type"] = typ
			return nil
		},
		Decorate: datalogStateDecorator("mail_forwarding", "forwarding_id"),
	}
}

// forwardingListScope filters the list route to one type.
func forwardingListScope(typ string) func(tx *gorm.DB) *gorm.DB {
	return func(tx *gorm.DB) *gorm.DB { return tx.Where("type = ?", typ) }
}

// forwardingGuard hides rows of other types on the by-id routes.
func forwardingGuard(typ string) func(rec any) error {
	return func(rec any) error {
		if rec.(*model.MailForwarding).Type != typ {
			return gorm.ErrRecordNotFound
		}
		return nil
	}
}
