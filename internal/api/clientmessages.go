package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"

	"go-ispconfig/internal/model"
	"go-ispconfig/internal/repository"
	"go-ispconfig/internal/validator"
)

// clientMessageTemplateEntity is the /api/client-message-templates CRUD
// surface (client_message_template.tform.php). Admin only. template_type
// "welcome" powers the welcome-on-create hook.
func clientMessageTemplateEntity() *Entity {
	return &Entity{
		Name:      "client-message-templates",
		Title:     "client_message_template_edit_title",
		AdminOnly: true,
		Tabs: []Tab{{
			Name:  "template",
			Label: "template_txt",
			Fields: []Field{
				selectField("template_type", "template_type_txt", "VARCHAR", "other", []Option{
					{Value: "welcome", Label: "welcome_email_txt"},
					{Value: "other", Label: "other_txt"},
				}),
				text("template_name", "template_name_txt",
					validator.Rule{Type: "NOTEMPTY", ErrKey: "template_name_error_empty"}),
				text("subject", "subject_txt",
					validator.Rule{Type: "NOTEMPTY", ErrKey: "subject_error_empty"}),
				textarea("message", "message_txt",
					validator.Rule{Type: "NOTEMPTY", ErrKey: "message_error_empty"}),
			},
		}},
	}
}

// sendMessageBody is the send-message request (port of client_message.php).
type sendMessageBody struct {
	// ClientIDs restricts the recipients; empty means every client the
	// caller can read.
	ClientIDs []int64 `json:"client_ids"`
	// TemplateID, when non-zero, supplies subject and message from a
	// client_message_template row.
	TemplateID int64  `json:"template_id"`
	Subject    string `json:"subject"`
	Message    string `json:"message"`
}

// sendMessageResult reports the send-message outcome.
type sendMessageResult struct {
	Sent    int `json:"sent"`
	Skipped int `json:"skipped"` // no email address or transport failure
}

// registerClientMessageRoutes mounts the send-message endpoint.
func registerClientMessageRoutes(g *echo.Group, d *Deps) {
	g.POST("/clients/send-message", func(c *echo.Context) error {
		return clientSendMessage(c, d)
	})
}

// clientSendMessage emails a rendered message to the selected clients
// (client_message.php): recipients are permission-scoped, so a reseller
// only ever reaches its own clients.
func clientSendMessage(c *echo.Context, d *Deps) error {
	if d.Mailer == nil {
		return &ValidationError{Fields: map[string][]string{"smtp": {"error.smtp_not_configured"}}}
	}
	id := identity(c)
	ctx := c.Request().Context()
	var body sendMessageBody
	if err := c.Bind(&body); err != nil {
		return err
	}
	subject, message := body.Subject, body.Message
	if body.TemplateID != 0 {
		var tpl model.ClientMessageTemplate
		err := d.DB.WithContext(ctx).
			Where("client_message_template_id = ?", body.TemplateID).Take(&tpl).Error
		if err != nil {
			return err // 404 on unknown template
		}
		subject, message = tpl.Subject, tpl.Message
	}
	if subject == "" || message == "" {
		return &ValidationError{Fields: map[string][]string{"message": {"message_error_empty"}}}
	}

	q := d.DB.WithContext(ctx).Model(&model.Client{}).
		Scopes(repository.WithPerm(id, repository.PermRead))
	if len(body.ClientIDs) > 0 {
		q = q.Where("client_id IN ?", body.ClientIDs)
	}
	var recipients []model.Client
	if err := q.Find(&recipients).Error; err != nil {
		return err
	}
	res := sendMessageResult{}
	for i := range recipients {
		r := &recipients[i]
		if r.Email == "" {
			res.Skipped++
			continue
		}
		err := d.Mailer.Send(r.Email,
			renderClientMessage(ctx, d, subject, r, ""),
			renderClientMessage(ctx, d, message, r, ""))
		if err != nil {
			slog.Warn("api: client message not sent", "client_id", r.ClientID, "error", err)
			res.Skipped++
			continue
		}
		res.Sent++
	}
	return c.JSON(http.StatusOK, res)
}

// renderClientMessage substitutes {column} placeholders with the client's
// field values (client_message.php / welcome email parity). {password}
// resolves to the plaintext only in the welcome-on-create path; it is
// never readable back from the stored hash.
func renderClientMessage(ctx context.Context, d *Deps, tpl string, row *model.Client, plainPassword string) string {
	if !strings.Contains(tpl, "{") {
		return tpl
	}
	vals := clientJSON(ctx, d, row) // redacted: no hash, no keys
	vals["password"] = plainPassword
	out := tpl
	for k, v := range vals {
		key := "{" + k + "}"
		if !strings.Contains(out, key) {
			continue
		}
		out = strings.ReplaceAll(out, key, fmt.Sprint(v))
	}
	return out
}

// sendWelcomeMessage emails the "welcome" message template to a freshly
// created client. Best effort: a missing template, missing address,
// unconfigured SMTP or transport failure never fails the create.
func sendWelcomeMessage(ctx context.Context, d *Deps, row *model.Client, plainPassword string) {
	if d.Mailer == nil || row.Email == "" {
		return
	}
	var tpl model.ClientMessageTemplate
	err := d.DB.WithContext(ctx).Where("template_type = 'welcome'").
		Order("client_message_template_id").First(&tpl).Error
	if err != nil {
		return
	}
	err = d.Mailer.Send(row.Email,
		renderClientMessage(ctx, d, tpl.Subject, row, plainPassword),
		renderClientMessage(ctx, d, tpl.Message, row, plainPassword))
	if err != nil {
		slog.Warn("api: welcome email not sent", "client_id", row.ClientID, "error", err)
	}
}
