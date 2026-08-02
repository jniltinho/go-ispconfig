package api

import (
	"context"
	"fmt"
	"net/http"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v5"
	"gorm.io/gorm"

	"go-ispconfig/internal/datalog"
	"go-ispconfig/internal/model"
	"go-ispconfig/internal/repository"
)

// This file ports the DNS zone wizard (design D9,
// dns_wizard.inc.php/dns_templatezone_add): dns_template rows keep
// ISPConfig's [ZONE]/[DNS_RECORDS] format with {DOMAIN}/{IP}/{IPV6}/{NS1}/
// {NS2}/{EMAIL} placeholders and TYPE|name|data|aux|ttl record lines, so
// templates from migrated ISPConfig3 databases expand unchanged. Expansion
// creates the SOA plus all records plus datalog rows in one transaction.

// registerDNSWizardRoutes mounts the wizard endpoints under /dns.
func registerDNSWizardRoutes(g *echo.Group, d *Deps) {
	g.GET("/templates", listDNSTemplates(d))
	g.POST("/zones/wizard", dnsWizardCreate(d))
}

// DNSTemplateInfo is one visible template offered by the zone wizard.
type DNSTemplateInfo struct {
	// TemplateID identifies the dns_template row.
	TemplateID uint32 `json:"template_id"`
	// Name is the display name.
	Name string `json:"name"`
	// Fields is the CSV of wizard inputs the template declares
	// (DOMAIN,IP,IPV6,NS1,NS2,EMAIL,DKIM,DNSSEC).
	Fields string `json:"fields"`
}

// listDNSTemplates implements GET /api/dns/templates.
//
//	@Summary		List zone wizard templates
//	@Description	The dns_template rows with visible = 'y', offered to every authenticated user by the zone wizard (port of the dns_wizard template list; admin CRUD lives at /dns/zone-templates).
//	@Tags			dns
//	@Produce		json
//	@Success		200	{array}		DNSTemplateInfo
//	@Failure		401	{object}	ErrorResponse
//	@Router			/dns/templates [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func listDNSTemplates(d *Deps) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var rows []model.DNSTemplate
		err := d.DB.WithContext(c.Request().Context()).
			Where("visible = 'y'").Order("name").Find(&rows).Error
		if err != nil {
			return err
		}
		out := make([]DNSTemplateInfo, len(rows))
		for i, r := range rows {
			out[i] = DNSTemplateInfo{TemplateID: r.TemplateID, Name: r.Name, Fields: r.Fields}
		}
		return c.JSON(http.StatusOK, out)
	}
}

// DNSWizardRequest is the POST /api/dns/zones/wizard body
// (dns_templatezone_add semantics).
type DNSWizardRequest struct {
	// TemplateID selects the dns_template row.
	TemplateID uint32 `json:"template_id"`
	// ServerID optionally selects the DNS server; the first dns_server=1
	// server is used when omitted.
	ServerID int32 `json:"server_id"`
	// ClientGroupID optionally assigns the zone to a client group
	// (admin only).
	ClientGroupID uint32 `json:"client_group_id"`
	// Domain fills {DOMAIN}.
	Domain string `json:"domain"`
	// IP fills {IP}.
	IP string `json:"ip"`
	// IPv6 fills {IPV6}.
	IPv6 string `json:"ipv6"`
	// NS1 fills {NS1}.
	NS1 string `json:"ns1"`
	// NS2 fills {NS2}.
	NS2 string `json:"ns2"`
	// Email fills {EMAIL}.
	Email string `json:"email"`
	// DNSSEC injects dnssec_wanted=Y into the [ZONE] section.
	DNSSEC bool `json:"dnssec"`
}

// templateRecord is one parsed [DNS_RECORDS] line (TYPE|name|data|aux|ttl).
type templateRecord struct {
	Type string
	Name string
	Data string
	Aux  uint32
	TTL  uint32
}

// expandDNSTemplate replaces the wizard placeholders in tpl and parses the
// [ZONE] key=value section and the [DNS_RECORDS] TYPE|name|data|aux|ttl
// lines (port of dns_wizard.inc.php). Empty replacement values leave their
// placeholder untouched, exactly like the PHP original.
func expandDNSTemplate(tpl string, req *DNSWizardRequest) (map[string]string, []templateRecord, error) {
	for placeholder, value := range map[string]string{
		"{DOMAIN}": req.Domain, "{IP}": req.IP, "{IPV6}": req.IPv6,
		"{NS1}": req.NS1, "{NS2}": req.NS2, "{EMAIL}": req.Email,
	} {
		if value != "" {
			tpl = strings.ReplaceAll(tpl, placeholder, value)
		}
	}
	vars := map[string]string{
		"xfer": "", "also_notify": "", "update_acl": "",
		"dnssec_wanted": "N", "dnssec_algo": "ECDSAP256SHA256",
	}
	var records []templateRecord
	section := ""
	for _, row := range strings.Split(tpl, "\n") {
		row = strings.TrimSpace(row)
		switch {
		case row == "":
		case strings.HasPrefix(row, "["):
			switch row {
			case "[ZONE]":
				section = "zone"
			case "[DNS_RECORDS]":
				section = "records"
			default:
				return nil, nil, fmt.Errorf("api: unknown template section %q", row)
			}
		case section == "zone":
			key, val, _ := strings.Cut(row, "=")
			if key = strings.TrimSpace(key); key != "" {
				vars[key] = strings.TrimSpace(val)
			}
		case section == "records":
			parts := strings.Split(row, "|")
			if len(parts) < 5 {
				return nil, nil, fmt.Errorf("api: malformed template record line %q", row)
			}
			aux, _ := strconv.ParseUint(parts[3], 10, 32)
			ttl, _ := strconv.ParseUint(parts[4], 10, 32)
			records = append(records, templateRecord{
				Type: parts[0], Name: parts[1], Data: parts[2],
				Aux: uint32(aux), TTL: uint32(ttl),
			})
		}
	}
	// The DNSSEC option wins over the template's pinned value. (The PHP
	// original injects dnssec_wanted=Y after [ZONE], which a later
	// dnssec_wanted=N template line silently overrides — the checkbox was
	// dead with the stock template; here the user's choice takes effect.)
	if req.DNSSEC {
		vars["dnssec_wanted"] = "Y"
	}
	return vars, records, nil
}

// wizardDomainRe / wizardHostRe are the dns_wizard input checks.
var (
	wizardDomainRe = regexp.MustCompile(`^[\w.\-]{1,64}\.[a-zA-Z0-9\-]{2,63}$`)
	wizardHostRe   = regexp.MustCompile(`^[\w.\-]{1,64}\.[a-zA-Z0-9]{2,63}$`)
	wizardEmailRe  = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
)

// validateWizardInputs checks the wizard values required by the template's
// fields CSV (port of the dns_wizard.inc.php checks; IPV6 is optional
// there, DKIM needs the mail module and is ignored for now).
func validateWizardInputs(fields string, req *DNSWizardRequest) map[string][]string {
	errs := map[string][]string{}
	for _, f := range strings.Split(fields, ",") {
		switch strings.TrimSpace(f) {
		case "DOMAIN":
			switch {
			case req.Domain == "":
				errs["domain"] = append(errs["domain"], "error_domain_empty")
			case !wizardDomainRe.MatchString(req.Domain):
				errs["domain"] = append(errs["domain"], "error_domain_regex")
			}
		case "IP":
			if req.IP == "" {
				errs["ip"] = append(errs["ip"], "error_ip_empty")
			} else if addr, err := netip.ParseAddr(req.IP); err != nil || !addr.Is4() {
				errs["ip"] = append(errs["ip"], "ip_error_wrong")
			}
		case "IPV6":
			if req.IPv6 != "" {
				if addr, err := netip.ParseAddr(req.IPv6); err != nil || !addr.Is6() {
					errs["ipv6"] = append(errs["ipv6"], "ip_error_wrong")
				}
			}
		case "NS1":
			switch {
			case req.NS1 == "":
				errs["ns1"] = append(errs["ns1"], "error_ns1_empty")
			case !wizardHostRe.MatchString(req.NS1):
				errs["ns1"] = append(errs["ns1"], "error_ns1_regex")
			}
		case "NS2":
			switch {
			case req.NS2 == "":
				errs["ns2"] = append(errs["ns2"], "error_ns2_empty")
			case !wizardHostRe.MatchString(req.NS2):
				errs["ns2"] = append(errs["ns2"], "error_ns2_regex")
			}
		case "EMAIL":
			switch {
			case req.Email == "":
				errs["email"] = append(errs["email"], "error_email_empty")
			case !wizardEmailRe.MatchString(req.Email):
				errs["email"] = append(errs["email"], "error_email_regex")
			}
		}
	}
	return errs
}

// soaFromTemplateVars builds the DNSSoa from the parsed [ZONE] vars,
// reporting empty required keys like the PHP wizard.
func soaFromTemplateVars(vars map[string]string) (*model.DNSSoa, map[string][]string) {
	errs := map[string][]string{}
	for _, key := range []string{"origin", "ns", "mbox", "refresh", "retry", "expire", "minimum", "ttl"} {
		if vars[key] == "" {
			errs[key] = append(errs[key], "error_"+key+"_empty")
		}
	}
	if len(errs) > 0 {
		return nil, errs
	}
	num := func(key string) uint32 {
		n, _ := strconv.ParseUint(vars[key], 10, 32)
		return uint32(n)
	}
	soa := &model.DNSSoa{
		Origin:       vars["origin"],
		NS:           vars["ns"],
		Mbox:         strings.ReplaceAll(vars["mbox"], "@", "."),
		Serial:       NextSerial(0, time.Now()),
		Refresh:      num("refresh"),
		Retry:        num("retry"),
		Expire:       num("expire"),
		Minimum:      num("minimum"),
		TTL:          num("ttl"),
		Active:       "Y",
		Xfer:         vars["xfer"],
		AlsoNotify:   vars["also_notify"],
		UpdateACL:    vars["update_acl"],
		DNSSECWanted: vars["dnssec_wanted"],
		DNSSECAlgo:   vars["dnssec_algo"],
	}
	return soa, nil
}

// wizardServerID resolves the target DNS server: the requested one
// (verified DNS-capable) or the first dns_server=1 server.
func wizardServerID(ctx context.Context, db *gorm.DB, requested int32) (int32, error) {
	q := db.WithContext(ctx).Model(&model.Server{}).
		Where("dns_server = 1 AND mirror_server_id = 0")
	if requested > 0 {
		q = q.Where("server_id = ?", requested)
	}
	var server model.Server
	if err := q.Order("server_id").First(&server).Error; err != nil {
		return 0, &ValidationError{Fields: map[string][]string{"server_id": {"error_no_server_id"}}}
	}
	return int32(server.ServerID), nil
}

// dnsWizardCreate implements POST /api/dns/zones/wizard.
//
//	@Summary		Create a zone from a template (wizard)
//	@Description	Port of dns_templatezone_add: replaces the {DOMAIN}/{IP}/{IPV6}/{NS1}/{NS2}/{EMAIL} placeholders in the selected dns_template, parses its [ZONE]/[DNS_RECORDS] sections and creates the SOA plus all records plus their datalog rows in one transaction, with the initial serial <today>01. The DNSSEC flag injects dnssec_wanted=Y. Admins may assign the zone to a client group via client_group_id. Legacy ISPConfig3 templates expand unchanged.
//	@Tags			dns
//	@Accept			json
//	@Produce		json
//	@Param			request	body		DNSWizardRequest	true	"Template id and wizard field values"
//	@Success		201		{object}	model.DNSSoa
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Failure		422		{object}	ErrorResponse	"Per-field validation errors"
//	@Router			/dns/zones/wizard [post]
//	@Security		CookieAuth
//	@Security		BearerAuth
func dnsWizardCreate(d *Deps) echo.HandlerFunc {
	return func(c *echo.Context) error {
		id := identity(c)
		if id == nil {
			return repository.ErrPermissionDenied
		}
		req := new(DNSWizardRequest)
		if err := c.Bind(req); err != nil {
			return err
		}
		ctx := c.Request().Context()

		var tplRow model.DNSTemplate
		q := d.DB.WithContext(ctx).Where("template_id = ?", req.TemplateID)
		if !id.IsAdmin() {
			q = q.Where("visible = 'y'")
		}
		if err := q.First(&tplRow).Error; err != nil {
			return &ValidationError{Fields: map[string][]string{"template_id": {"template_id_error_empty"}}}
		}

		// Normalize the free-form inputs (tform IDNTOASCII/TOLOWER filters).
		for _, field := range []*string{&req.Domain, &req.NS1, &req.NS2, &req.Email} {
			b := map[string]any{"v": *field}
			idnLower(b, "v")
			*field, _ = b["v"].(string)
		}
		if errs := validateWizardInputs(tplRow.Fields, req); len(errs) > 0 {
			return &ValidationError{Fields: errs}
		}

		vars, records, err := expandDNSTemplate(tplRow.Template, req)
		if err != nil {
			return &ValidationError{Fields: map[string][]string{"template_id": {"template_error_invalid"}}}
		}
		soa, errs := soaFromTemplateVars(vars)
		if len(errs) > 0 {
			return &ValidationError{Fields: errs}
		}

		serverID, err := wizardServerID(ctx, d.DB, req.ServerID)
		if err != nil {
			return err
		}
		soa.ServerID = serverID

		// Ownership: the caller, or the requested client group when an
		// admin (or a reseller owning that group) creates the zone for a
		// client (dns_wizard sys_groupid resolution).
		group := id.DefaultGroup
		if req.ClientGroupID > 0 && (id.IsAdmin() || id.InGroup(req.ClientGroupID)) {
			group = req.ClientGroupID
		}
		if group == 0 {
			group = 1 // admin group
		}
		soa.SysUserID = id.UserID
		soa.SysGroupID = group
		soa.SysPermUser, soa.SysPermGroup, soa.SysPermOther = "riud", "riud", ""

		// A duplicate origin must fail cleanly before the transaction.
		var n int64
		if err := d.DB.WithContext(ctx).Model(&model.DNSSoa{}).
			Where("origin = ?", soa.Origin).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return &ValidationError{Fields: map[string][]string{"domain": {"origin_error_unique"}}}
		}

		err = dnsTxn(ctx, d.DB, func(tx *gorm.DB) error {
			if err := tx.Create(soa).Error; err != nil {
				return err
			}
			if err := datalog.LogInsert(tx, soa, id.Username); err != nil {
				return err
			}
			for _, r := range records {
				rr := &model.DNSRr{
					SysUserID: soa.SysUserID, SysGroupID: soa.SysGroupID,
					SysPermUser: soa.SysPermUser, SysPermGroup: soa.SysPermGroup,
					SysPermOther: soa.SysPermOther,
					ServerID:     soa.ServerID, Zone: soa.ID,
					Name: r.Name, Type: r.Type, Data: r.Data,
					Aux: r.Aux, TTL: r.TTL, Active: "Y",
				}
				if err := tx.Create(rr).Error; err != nil {
					return err
				}
				if err := datalog.LogInsert(tx, rr, id.Username); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
		return c.JSON(http.StatusCreated, modelToMap(ctx, d.DB, soa))
	}
}
