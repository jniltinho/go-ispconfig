package api

import (
	"github.com/labstack/echo/v5"

	"go-ispconfig/internal/model"
	"go-ispconfig/internal/repository"
	"go-ispconfig/internal/validator"
)

// mailTransportEntity is the /api/mail/transports CRUD surface
// (mail_transport.tform.php): unique (server_id, domain).
func mailTransportEntity() *Entity {
	return &Entity{
		Name:  "transports",
		Title: "mail_transport_edit_title",
		Tabs: []Tab{{
			Name:  "transport",
			Label: "transport_txt",
			Fields: []Field{
				intField("server_id", "server_id_txt", "0"),
				text("domain", "domain_txt",
					validator.Rule{Type: "NOTEMPTY", ErrKey: "domain_error_empty"},
					validator.Rule{Type: "UNIQUE", ErrKey: "domain_error_unique"}),
				text("transport", "transport_txt",
					validator.Rule{Type: "NOTEMPTY", ErrKey: "transport_error_empty"}),
				intField("sort_order", "sort_order_txt", "5"),
				checkbox("active", "active_txt", "y"),
			},
		}},
		Prepare:  mailTransportPrepare,
		Decorate: datalogStateDecorator("mail_transport", "transport_id"),
	}
}

// mailTransportPrepare lowercases the domain and rejects a collision
// with an existing mail_domain on the same server (validate_transport
// domain_is_maildomain parity; the UNIQUE validator covers transport
// duplicates).
func mailTransportPrepare(c *echo.Context, d *Deps, _ *repository.Identity, body map[string]any) error {
	dom, _ := body["domain"].(string)
	if dom == "" {
		return nil
	}
	if sid, ok := body["server_id"]; ok {
		var n int64
		if err := d.DB.WithContext(c.Request().Context()).Model(&model.MailDomain{}).
			Where("server_id = ? AND domain = ?", sid, dom).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return &ValidationError{Fields: map[string][]string{"domain": {"domain_is_maildomain"}}}
		}
	}
	return nil
}

// registerMailTransportEntity mounts the transport entity.
func registerMailTransportEntity(g *echo.Group, d *Deps) error {
	return RegisterEntity[model.MailTransport](g, d, mailTransportEntity())
}
