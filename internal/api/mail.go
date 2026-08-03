package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v5"
	"golang.org/x/net/idna"
	"gorm.io/gorm"

	"go-ispconfig/internal/getconf"
	"go-ispconfig/internal/mail"
	"go-ispconfig/internal/model"
	"go-ispconfig/internal/repository"
	"go-ispconfig/internal/validator"
)

// registerMailEntities mounts the mail CRUD entities and extra routes on
// the /mail group (port of remote.d/mail.inc.php onto the entity
// framework, design D11).
func registerMailEntities(g *echo.Group, d *Deps) error {
	mg := g.Group("/mail")
	if err := RegisterEntity[model.MailDomain](mg, d, mailDomainEntity(d)); err != nil {
		return err
	}
	if err := RegisterEntity[model.MailUser](mg, d, mailboxEntity(d)); err != nil {
		return err
	}
	if err := registerForwardingEntities(mg, d); err != nil {
		return err
	}
	if err := registerMailTransportEntity(mg, d); err != nil {
		return err
	}
	if err := registerSpamfilterEntities(mg, d); err != nil {
		return err
	}
	registerMailDomainRoutes(mg, d)
	registerMailboxRoutes(mg, d)
	registerRspamdRoutes(mg, d)
	return nil
}

// publisher returns the configured DNS publisher or the no-op.
func (d *Deps) publisher() DNSPublisher {
	if d.DNSPub != nil {
		return d.DNSPub
	}
	return NoopDNSPublisher{}
}

// mailDomainEntity is the /api/mail/domains CRUD surface
// (mail_domain.tform.php).
func mailDomainEntity(d *Deps) *Entity {
	return &Entity{
		Name:  "domains",
		Title: "mail_domain_edit_title",
		Tabs: []Tab{{
			Name:  "domain",
			Label: "domain_txt",
			Fields: []Field{
				{
					Name: "server_id", Label: "server_id_txt",
					Datatype: "INTEGER", Formtype: "SELECT",
					Validators: []validator.Rule{
						{Type: "ISPOSITIVE", ErrKey: "server_id_error_empty"},
					},
				},
				text("domain", "domain_txt",
					validator.Rule{Type: "NOTEMPTY", ErrKey: "domain_error_empty"},
					validator.Rule{Type: "CUSTOM", ErrKey: "domain_error_regex", Fn: checkIsDomain}),
				checkbox("active", "active_txt", "y"),
				checkbox("local_delivery", "local_delivery_txt", "y"),
				checkbox("dkim", "dkim_txt", "n"),
				text("dkim_selector", "dkim_selector_txt",
					validator.Rule{Type: "CUSTOM", ErrKey: "dkim_selector_error", Fn: checkDKIMSelector}),
				textarea("dkim_private", "dkim_private_txt"),
				textarea("dkim_public", "dkim_public_txt"),
				{Name: "relay_host", Label: "relay_host_txt", Datatype: "VARCHAR", Formtype: "TEXT", AdminOnly: true},
				{Name: "relay_user", Label: "relay_user_txt", Datatype: "VARCHAR", Formtype: "TEXT", AdminOnly: true},
				{Name: "relay_pass", Label: "relay_pass_txt", Datatype: "VARCHAR", Formtype: "PASSWORD", AdminOnly: true},
			},
		}},
		Prepare:      mailDomainPrepare,
		AfterInsert:  mailDomainAfterInsert(d),
		BeforeUpdate: mailDomainBeforeUpdate(d),
		BeforeDelete: mailDomainBeforeDelete(d),
		Decorate:     serverNameDecorate(mailDomainDecorate()),
		ListFilters: map[string]ListFilterFunc{
			"_server_name": relatedNameFilter("server_id", "server", "server_id", "server_name"),
		},
	}
}

// mailDomainDecorate strips the DKIM private key from every response
// (spec mail-dkim: list endpoints never return dkim_private) and adds
// the datalog pending/error state.
func mailDomainDecorate() func(ctx context.Context, db *gorm.DB, items []map[string]any) error {
	state := datalogStateDecorator("mail_domain", "domain_id")
	return func(ctx context.Context, db *gorm.DB, items []map[string]any) error {
		names := make([]string, 0, len(items))
		for _, it := range items {
			delete(it, "dkim_private")
			delete(it, "relay_pass")
			if it["dkim"] == "y" {
				name := mail.DKIMRecordName(fmt.Sprint(it["dkim_selector"]), fmt.Sprint(it["domain"]))
				data := mail.DKIMTXTValue(fmt.Sprint(it["dkim_public"]))
				it["suggested_record"] = name + ` 3600 IN TXT "` + data + `"`
				it["dns_published"] = false
				names = append(names, name)
			}
		}
		if len(names) > 0 {
			var published []string
			if err := db.WithContext(ctx).Model(&model.DNSRr{}).
				Where("type = 'TXT' AND name IN ?", names).Pluck("name", &published).Error; err != nil {
				return err
			}
			set := make(map[string]struct{}, len(published))
			for _, n := range published {
				set[n] = struct{}{}
			}
			for _, it := range items {
				if it["dkim"] == "y" {
					name := mail.DKIMRecordName(fmt.Sprint(it["dkim_selector"]), fmt.Sprint(it["domain"]))
					if _, ok := set[name]; ok {
						it["dns_published"] = true
					}
				}
			}
		}
		return state(ctx, db, items)
	}
}

// checkIsDomain ports the ISDOMAIN validator (labels of alnum/hyphen,
// dot-separated).
func checkIsDomain(_ *validator.Context, value string) string {
	if value == "" {
		return ""
	}
	if _, err := idna.Lookup.ToASCII(strings.TrimSuffix(value, ".")); err != nil {
		return "domain_error_regex"
	}
	if !strings.Contains(value, ".") {
		return "domain_error_regex"
	}
	return ""
}

// checkDKIMSelector ports the selector REGEX.
func checkDKIMSelector(_ *validator.Context, value string) string {
	if value == "" || mail.ValidDKIMSelector(value) {
		return ""
	}
	return "dkim_selector_error"
}

// mailDomainPrepare lowercases/IDN-encodes the domain, defaults the
// selector, validates any supplied private key and generates a DKIM key
// pair when DKIM is enabled without one (port of the mail_domain_edit
// create_dkim + tform filters).
func mailDomainPrepare(c *echo.Context, d *Deps, _ *repository.Identity, body map[string]any) error {
	if err := requireTargetServer("mail_server")(c, d, body); err != nil {
		return err
	}
	if dom, ok := body["domain"].(string); ok && dom != "" {
		dom = strings.ToLower(strings.TrimSuffix(dom, "."))
		if ascii, err := idna.Lookup.ToASCII(dom); err == nil {
			dom = ascii
		}
		body["domain"] = dom
		// A mail domain may not collide with a transport domain on the
		// same server (validate_isnot_mailtransport parity).
		if sid, ok := body["server_id"]; ok {
			var n int64
			if err := d.DB.WithContext(c.Request().Context()).Model(&model.MailTransport{}).
				Where("server_id = ? AND domain = ?", sid, dom).Count(&n).Error; err != nil {
				return err
			}
			if n > 0 {
				return &ValidationError{Fields: map[string][]string{"domain": {"domain_is_transport"}}}
			}
		}
	}

	if body["dkim"] != "y" {
		return nil
	}
	if _, ok := body["dkim_selector"].(string); !ok || body["dkim_selector"] == "" {
		body["dkim_selector"] = "default"
	}
	priv, _ := body["dkim_private"].(string)
	if priv != "" {
		if _, err := mail.ParseDKIMPrivate(priv); err != nil {
			return &ValidationError{Fields: map[string][]string{"dkim_private": {"dkim_private_key_error"}}}
		}
		if body["dkim_public"] == nil || body["dkim_public"] == "" {
			pub, err := mail.DeriveDKIMPublic(priv)
			if err != nil {
				return &ValidationError{Fields: map[string][]string{"dkim_private": {"dkim_private_key_error"}}}
			}
			body["dkim_public"] = pub
		}
		return nil
	}
	// DKIM enabled without a key: generate one at the configured strength.
	privPEM, pubPEM, err := mail.GenerateDKIMKey(dkimStrength(c, d, uint32(bodyInt(body, "server_id"))))
	if err != nil {
		return err
	}
	body["dkim_private"] = privPEM
	body["dkim_public"] = pubPEM
	return nil
}

// dkimStrength reads dkim_strength from the domain's server mail config
// (default 2048).
func dkimStrength(_ *echo.Context, d *Deps, serverID uint32) int {
	cfg := getconf.DefaultMailConfig()
	if sc, err := getconf.GetServerConfig(d.DB, serverID); err == nil {
		cfg = sc.Mail
	}
	if n, err := strconv.Atoi(cfg.DKIMStrength); err == nil && n >= 1024 {
		return n
	}
	return 2048
}

// dkimStateFor builds the publisher input from a domain record.
func dkimStateFor(m *model.MailDomain) *DKIMDomain {
	return &DKIMDomain{
		Domain: m.Domain, Selector: m.DKIMSelector, Public: m.DKIMPublic,
		Active: m.Active == "y", DKIM: m.DKIM == "y",
	}
}

// mailDomainAfterInsert reconciles DKIM DNS after an insert (old=nil).
func mailDomainAfterInsert(d *Deps) func(context.Context, *gorm.DB, *repository.Identity, any, map[string]any) error {
	return func(ctx context.Context, tx *gorm.DB, _ *repository.Identity, rec any, _ map[string]any) error {
		SyncDKIMDNS(ctx, tx, d.publisher(), nil, dkimStateFor(rec.(*model.MailDomain)))
		return nil
	}
}

// mailDomainBeforeDelete withdraws the DKIM TXT inside the delete
// transaction (the daemon removes the maildomain tree from the datalog).
func mailDomainBeforeDelete(d *Deps) func(context.Context, *gorm.DB, *repository.Identity, any) error {
	return func(ctx context.Context, tx *gorm.DB, _ *repository.Identity, rec any) error {
		SyncDKIMDNS(ctx, tx, d.publisher(), dkimStateFor(rec.(*model.MailDomain)), nil)
		return nil
	}
}

// mailDomainBeforeUpdate reconciles DKIM DNS after an update using the
// old and new records.
func mailDomainBeforeUpdate(d *Deps) func(context.Context, *gorm.DB, *repository.Identity, map[string]any, any, any) error {
	return func(ctx context.Context, tx *gorm.DB, _ *repository.Identity, _ map[string]any, old, rec any) error {
		SyncDKIMDNS(ctx, tx, d.publisher(),
			dkimStateFor(old.(*model.MailDomain)), dkimStateFor(rec.(*model.MailDomain)))
		return nil
	}
}

// --- extra routes (remote.d/mail.inc.php helpers) ---

func registerMailDomainRoutes(g *echo.Group, d *Deps) {
	g.GET("/domains/by-domain/:domain", func(c *echo.Context) error {
		id := identity(c)
		var row model.MailDomain
		err := d.DB.WithContext(c.Request().Context()).Model(&model.MailDomain{}).
			Scopes(repository.WithPerm(id, repository.PermRead)).
			Where("domain = ?", c.Param("domain")).Take(&row).Error
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, redactMailDomain(&row))
	})

	g.POST("/domains/:id/set-status", func(c *echo.Context) error {
		var body struct {
			Active string `json:"active"`
		}
		if err := c.Bind(&body); err != nil {
			return err
		}
		if body.Active != "y" && body.Active != "n" {
			return &ValidationError{Fields: map[string][]string{"active": {"active_error_invalid"}}}
		}
		return mailDomainSetStatus(c, d, body.Active)
	})

	g.POST("/domains/generate-dkim", func(c *echo.Context) error {
		priv, pub, err := mail.GenerateDKIMKey(2048)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, map[string]string{"dkim_private": priv, "dkim_public": pub})
	})
}

// mailDomainSetStatus flips active and reconciles DKIM DNS, journaling
// the change through the repository (remote mail_domain_set_status).
func mailDomainSetStatus(c *echo.Context, d *Deps, active string) error {
	id := identity(c)
	repo, err := repository.New[model.MailDomain](d.DB)
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	var rec model.MailDomain
	if err := repo.Get(ctx, id, c.Param("id"), &rec); err != nil {
		return err
	}
	old := rec
	rec.Active = active
	err = repo.UpdateFn(ctx, id, &rec, func(tx *gorm.DB, _ *model.MailDomain) error {
		SyncDKIMDNS(ctx, tx, d.publisher(), dkimStateFor(&old), dkimStateFor(&rec))
		return nil
	})
	if err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

// redactMailDomain strips the private key from a single-record response.
func redactMailDomain(m *model.MailDomain) map[string]any {
	return map[string]any{
		"domain_id": m.DomainID, "server_id": m.ServerID, "domain": m.Domain,
		"active": m.Active, "local_delivery": m.LocalDelivery,
		"dkim": m.DKIM, "dkim_selector": m.DKIMSelector, "dkim_public": m.DKIMPublic,
		"sys_userid": m.SysUserID, "sys_groupid": m.SysGroupID,
	}
}
