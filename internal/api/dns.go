package api

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v5"
	"golang.org/x/net/idna"

	"go-ispconfig/internal/model"
	"go-ispconfig/internal/repository"
	"go-ispconfig/internal/validator"
)

// This file defines the DNS module zone entities (design D7/D8): the Go
// port of dns_soa.tform.php, dns_slave.tform.php and dns_template.tform.php
// on the declarative entity framework, mounted under /api/dns. The API only
// persists records + sys_datalog; the daemon (internal/dns) renders zone
// files and named.conf.local from them.

// registerDNSEntities mounts the DNS CRUD entities and extra routes on the
// /dns group.
func registerDNSEntities(g *echo.Group, d *Deps) error {
	if err := RegisterEntity[model.DNSSoa](g, d, dnsZoneEntity()); err != nil {
		return err
	}
	if err := RegisterEntity[model.DNSSlave](g, d, dnsSlaveEntity()); err != nil {
		return err
	}
	if err := RegisterEntity[model.DNSTemplate](g, d, dnsTemplateEntity()); err != nil {
		return err
	}
	registerDNSRecordTypeRoutes(g)
	return nil
}

// ynUpperOptions is the uppercase Y/N CHECKBOX value array the dns_ tables
// use (unlike the lowercase y/n of the web module).
func ynUpperOptions() []Option {
	return []Option{{Value: "N", Label: "no_txt"}, {Value: "Y", Label: "yes_txt"}}
}

// upperCheckbox is a Y/N CHECKBOX field.
func upperCheckbox(name, label, def string) Field {
	return Field{Name: name, Label: label, Datatype: "VARCHAR", Formtype: "CHECKBOX",
		Default: def, Options: ynUpperOptions()}
}

// minRule ports the tform RANGE 'min:' validator: an integer >= min.
func minRule(min int64, errKey string) validator.Rule {
	return validator.Rule{Type: "CUSTOM", ErrKey: errKey, Fn: func(_ *validator.Context, value string) string {
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil || n < min {
			return errKey
		}
		return ""
	}}
}

// ipListRule ports the validate_dns::validate_ip / tform ISIP separator ','
// validator: an empty value (when allowed) or a comma-separated list of
// valid IP addresses.
func ipListRule(errKey string, allowEmpty bool) validator.Rule {
	return validator.Rule{Type: "CUSTOM", ErrKey: errKey, Fn: func(_ *validator.Context, value string) string {
		if strings.TrimSpace(value) == "" {
			if allowEmpty {
				return ""
			}
			return errKey
		}
		for _, part := range strings.Split(value, ",") {
			if _, err := netip.ParseAddr(strings.TrimSpace(part)); err != nil {
				return errKey
			}
		}
		return ""
	}}
}

// dnssecAlgoRule validates dnssec_algo as a non-empty CSV subset of the
// supported algorithms (dns_soa.tform.php CHECKBOXARRAY values).
func dnssecAlgoRule() validator.Rule {
	return validator.Rule{Type: "CUSTOM", ErrKey: "dnssec_algo_error", Fn: func(_ *validator.Context, value string) string {
		if value == "" {
			return ""
		}
		for _, part := range strings.Split(value, ",") {
			switch strings.TrimSpace(part) {
			case "NSEC3RSASHA1", "ECDSAP256SHA256":
			default:
				return "dnssec_algo_error"
			}
		}
		return ""
	}}
}

// idnLower applies the tform IDNTOASCII + TOLOWER save filters to a string
// body field.
func idnLower(body map[string]any, key string) {
	v, ok := body[key].(string)
	if !ok {
		return
	}
	v = strings.ToLower(strings.TrimSpace(v))
	if ascii, err := idna.Lookup.ToASCII(strings.TrimSuffix(v, ".")); err == nil {
		if strings.HasSuffix(v, ".") {
			v = ascii + "."
		} else {
			v = ascii
		}
	}
	body[key] = v
}

// originRegex is the dns_soa/dns_slave origin FQDN rule.
const originRegex = `^[a-zA-Z0-9.\-/]{1,255}\.[a-zA-Z0-9\-]{2,63}\.?$`

// --- dns zone entity (port of dns_soa.tform.php) ---

// dnsZoneEntity is the declarative definition of the dns_soa zone form.
// serial is declared so the server-side bump (dnsZonePrepare) reaches the
// record; rendered_zone and dnssec_info stay undeclared (daemon-written,
// still returned by GET) so request bodies can never overwrite them.
func dnsZoneEntity() *Entity {
	return &Entity{
		Name:     "zones",
		Title:    "dns_soa_edit_title",
		Prepare:  dnsZonePrepare,
		Decorate: datalogStateDecorator("dns_soa", "id"),
		Tabs: []Tab{{
			Name: "dns_soa", Label: "dns_soa_tab_txt",
			Fields: []Field{
				selectField("server_id", "server_id_txt", "INTEGER", nil, nil,
					validator.Rule{Type: "ISPOSITIVE", ErrKey: "server_id_error_empty"}),
				text("origin", "origin_txt",
					validator.Rule{Type: "NOTEMPTY", ErrKey: "origin_error_empty"},
					validator.Rule{Type: "UNIQUE", ErrKey: "origin_error_unique"},
					regex(originRegex, "origin_error_regex")),
				text("ns", "ns_txt",
					regex(`^[a-zA-Z0-9.\-]{1,255}$`, "ns_error_regex")),
				text("mbox", "mbox_txt",
					validator.Rule{Type: "NOTEMPTY", ErrKey: "mbox_error_empty"},
					regex(`^[a-zA-Z0-9.\-_+]{0,255}\.$`, "mbox_error_regex")),
				intField("serial", "serial_txt", nil),
				intField("refresh", "refresh_txt", "7200", minRule(60, "refresh_range_error")),
				intField("retry", "retry_txt", "540", minRule(60, "retry_range_error")),
				intField("expire", "expire_txt", "604800", minRule(60, "expire_range_error")),
				intField("minimum", "minimum_txt", "3600", minRule(60, "minimum_range_error")),
				intField("ttl", "ttl_txt", "3600", minRule(60, "ttl_range_error")),
				text("xfer", "xfer_txt", ipListRule("xfer_error_regex", true)),
				text("also_notify", "also_notify_txt", ipListRule("also_notify_error_regex", true)),
				{Name: "update_acl", Label: "update_acl_txt", Datatype: "VARCHAR",
					Formtype: "TEXT", AdminOnly: true},
				upperCheckbox("active", "active_txt", "Y"),
				upperCheckbox("dnssec_wanted", "dnssec_wanted_txt", "N"),
				selectField("dnssec_algo", "dnssec_algo_txt", "VARCHAR", "ECDSAP256SHA256",
					[]Option{
						{Value: "ECDSAP256SHA256", Label: "13 (ECDSAP256SHA256)"},
						{Value: "NSEC3RSASHA1", Label: "7 (NSEC3RSASHA1)"},
					}, dnssecAlgoRule()),
			},
		}},
	}
}

// zoneBodyFields are the declared dns_soa fields compared against the
// stored row to decide whether an update really changes the zone (and must
// bump the serial).
var zoneBodyFields = []string{"server_id", "origin", "ns", "mbox", "refresh", "retry",
	"expire", "minimum", "ttl", "xfer", "also_notify", "update_acl", "active",
	"dnssec_wanted", "dnssec_algo"}

// zoneFieldString renders a stored DNSSoa field in the string form request
// body values are compared with.
func zoneFieldString(z *model.DNSSoa, field string) string {
	switch field {
	case "server_id":
		return strconv.FormatInt(int64(z.ServerID), 10)
	case "origin":
		return z.Origin
	case "ns":
		return z.NS
	case "mbox":
		return z.Mbox
	case "refresh":
		return strconv.FormatUint(uint64(z.Refresh), 10)
	case "retry":
		return strconv.FormatUint(uint64(z.Retry), 10)
	case "expire":
		return strconv.FormatUint(uint64(z.Expire), 10)
	case "minimum":
		return strconv.FormatUint(uint64(z.Minimum), 10)
	case "ttl":
		return strconv.FormatUint(uint64(z.TTL), 10)
	case "xfer":
		return z.Xfer
	case "also_notify":
		return z.AlsoNotify
	case "update_acl":
		return z.UpdateACL
	case "active":
		return z.Active
	case "dnssec_wanted":
		return z.DNSSECWanted
	case "dnssec_algo":
		return z.DNSSECAlgo
	}
	return ""
}

// bodyString renders a JSON body value in comparable string form.
func bodyString(v any) string {
	switch n := v.(type) {
	case string:
		return n
	case float64:
		return strconv.FormatFloat(n, 'f', -1, 64)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// checkDNSServer verifies the referenced server is DNS-capable
// (dns_soa.tform.php server_id datasource: dns_server = 1).
func checkDNSServer(c *echo.Context, d *Deps, body map[string]any) error {
	sid := bodyInt(body, "server_id")
	if sid <= 0 {
		return nil
	}
	var n int64
	err := d.DB.WithContext(c.Request().Context()).Model(&model.Server{}).
		Where("server_id = ? AND dns_server = 1 AND mirror_server_id = 0", sid).Count(&n).Error
	if err != nil {
		return err
	}
	if n == 0 {
		return &ValidationError{Fields: map[string][]string{"server_id": {"server_id_error_empty"}}}
	}
	return nil
}

// dnsZonePrepare normalizes zone body fields before validation (tform
// IDNTOASCII/TOLOWER save filters on origin/ns/mbox, STRIPTAGS/STRIPNL on
// update_acl), verifies the target server is DNS-capable and manages the
// SOA serial (design D7): creates start at <today>01, updates that change
// any declared field bump the serial unless update_serial=false is passed.
func dnsZonePrepare(c *echo.Context, d *Deps, id *repository.Identity, body map[string]any) error {
	idnLower(body, "origin")
	idnLower(body, "ns")
	idnLower(body, "mbox")
	if v, ok := body["update_acl"].(string); ok {
		body["update_acl"] = strings.NewReplacer("\n", "", "\r", "", "<", "", ">", "").Replace(v)
	}
	if err := checkDNSServer(c, d, body); err != nil {
		return err
	}

	updateSerial := true
	if v, ok := body["update_serial"].(bool); ok {
		updateSerial = v
	}
	if c.Param("id") == "" {
		// Create: initial serial <today>01 unless explicitly provided.
		if _, ok := body["serial"]; !ok && updateSerial {
			body["serial"] = float64(NextSerial(0, time.Now()))
		}
		return nil
	}
	if !updateSerial {
		return nil
	}
	stored, err := loadOwned[model.DNSSoa](c, d, id, c.Param("id"))
	if err != nil {
		return err
	}
	for _, f := range zoneBodyFields {
		v, ok := body[f]
		if !ok {
			continue
		}
		if bodyString(v) != zoneFieldString(stored, f) {
			body["serial"] = float64(NextSerial(stored.Serial, time.Now()))
			return nil
		}
	}
	return nil
}

// --- dns slave entity (port of dns_slave.tform.php) ---

// dnsSlaveEntity is the declarative definition of the dns_slave secondary
// zone form: origin like a zone, ns is the master IP list.
func dnsSlaveEntity() *Entity {
	return &Entity{
		Name:     "slave-zones",
		Title:    "dns_slave_edit_title",
		Prepare:  dnsSlavePrepare,
		Decorate: datalogStateDecorator("dns_slave", "id"),
		Tabs: []Tab{{
			Name: "dns_slave", Label: "dns_slave_tab_txt",
			Fields: []Field{
				selectField("server_id", "server_id_txt", "INTEGER", nil, nil,
					validator.Rule{Type: "ISPOSITIVE", ErrKey: "server_id_error_empty"}),
				text("origin", "origin_txt",
					validator.Rule{Type: "NOTEMPTY", ErrKey: "origin_error_empty"},
					validator.Rule{Type: "UNIQUE", ErrKey: "origin_error_unique"},
					regex(originRegex, "origin_error_regex")),
				text("ns", "slave_ns_txt", ipListRule("ns_error_regex", false)),
				text("xfer", "xfer_txt", ipListRule("xfer_error_regex", true)),
				upperCheckbox("active", "active_txt", "Y"),
			},
		}},
	}
}

// dnsSlavePrepare normalizes the slave origin and verifies the DNS server.
func dnsSlavePrepare(c *echo.Context, d *Deps, _ *repository.Identity, body map[string]any) error {
	idnLower(body, "origin")
	return checkDNSServer(c, d, body)
}

// --- dns template entity (port of dns_template.tform.php, admin only) ---

// templateFieldsRule validates the fields CSV against the wizard
// placeholders dns_template.tform.php offers.
func templateFieldsRule() validator.Rule {
	return validator.Rule{Type: "CUSTOM", ErrKey: "fields_error", Fn: func(_ *validator.Context, value string) string {
		if value == "" {
			return ""
		}
		for _, part := range strings.Split(value, ",") {
			switch strings.TrimSpace(part) {
			case "DOMAIN", "IP", "IPV6", "NS1", "NS2", "EMAIL", "DKIM", "DNSSEC":
			default:
				return "fields_error"
			}
		}
		return ""
	}}
}

// dnsTemplateEntity is the declarative definition of the dns_template
// wizard template form. The whole entity is admin-gated: templates are
// panel-wide configuration, not client data (the dns module menu exposes
// them under Admin in ISPConfig).
func dnsTemplateEntity() *Entity {
	return &Entity{
		Name:      "zone-templates",
		Title:     "dns_template_edit_title",
		AdminOnly: true,
		Tabs: []Tab{{
			Name: "template", Label: "dns_template_tab_txt",
			Fields: []Field{
				text("name", "name_txt",
					validator.Rule{Type: "NOTEMPTY", ErrKey: "name_error_empty"}),
				text("fields", "fields_txt", templateFieldsRule()),
				textarea("template", "template_txt"),
				{Name: "visible", Label: "visible_txt", Datatype: "VARCHAR", Formtype: "CHECKBOX",
					Default: "y", Options: []Option{{Value: "n", Label: "no_txt"}, {Value: "y", Label: "yes_txt"}}},
			},
		}},
	}
}
