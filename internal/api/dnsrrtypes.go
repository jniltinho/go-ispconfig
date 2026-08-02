package api

import (
	"net/http"
	"net/netip"
	"regexp"
	"strings"

	"github.com/labstack/echo/v5"
)

// This file holds the declarative DNS record-type metadata (design D8): one
// table of descriptors replacing the 20+ dns_<type>.tform.php files. The
// same definitions drive the API record validation and are exported as JSON
// to the Vue record editor dialog.

// DNSRecordType describes one record type of the DNS record editor: the
// dns_rr.type value it stores, the validation rules of name/data/aux/ttl
// and the UI hints for the metadata-driven dialog.
type DNSRecordType struct {
	// Type is the API discriminator (A, MX, ... plus the TXT-derived
	// SPF/DKIM/DMARC helper forms).
	Type string `json:"type"`
	// StoredType is the dns_rr.type enum value written to the DB.
	StoredType string `json:"stored_type"`
	// NameRegex validates the record name (Go regex, exported for the UI).
	NameRegex string `json:"name_regex"`
	// NameRequired marks types whose name must not be empty.
	NameRequired bool `json:"name_required"`
	// DataKind selects the data check: "ipv4", "ipv6" or "text".
	DataKind string `json:"data_kind"`
	// DataRegex additionally validates textual data when set.
	DataRegex string `json:"data_regex,omitempty"`
	// DataPrefix requires the data to start with this string (SPF/DMARC).
	DataPrefix string `json:"data_prefix,omitempty"`
	// DataNotContains rejects data containing any of these substrings
	// (plain TXT refuses DKIM/DMARC/SPF payloads — dedicated forms exist).
	DataNotContains []string `json:"data_not_contains,omitempty"`
	// DataLabel is the i18n label key of the data input.
	DataLabel string `json:"data_label"`
	// AuxUsed marks types whose aux column is meaningful (MX/SRV/NAPTR).
	AuxUsed bool `json:"aux_used"`
	// AuxLabel is the i18n label key of the aux input (priority).
	AuxLabel string `json:"aux_label,omitempty"`
	// AuxDefault is the default aux value.
	AuxDefault uint32 `json:"aux_default"`
	// TTLDefault is the default TTL.
	TTLDefault uint32 `json:"ttl_default"`
}

// hostnameRegex is the tform hostname data rule shared by several types.
const hostnameRegex = `^[a-zA-Z0-9.\-]{1,255}$`

// dnsRecordTypes is the ordered descriptor table, ported from the
// dns_<type>.tform.php validators.
var dnsRecordTypes = []DNSRecordType{
	{Type: "A", StoredType: "A", NameRegex: `^[a-zA-Z0-9.\-*]{0,64}$`,
		DataKind: "ipv4", DataLabel: "data_a_txt", TTLDefault: 3600},
	{Type: "AAAA", StoredType: "AAAA", NameRegex: `^[a-zA-Z0-9.\-*]{0,64}$`,
		DataKind: "ipv6", DataLabel: "data_aaaa_txt", TTLDefault: 3600},
	{Type: "ALIAS", StoredType: "ALIAS", NameRegex: `^[a-zA-Z0-9.\-_]{1,255}$`, NameRequired: true,
		DataKind: "text", DataRegex: hostnameRegex, DataLabel: "data_alias_txt", TTLDefault: 3600},
	{Type: "CAA", StoredType: "CAA", NameRegex: `^[a-zA-Z0-9.\-_*]{0,255}$`,
		DataKind: "text", DataRegex: `^\d+\s+(issue|issuewild|iodef)\s+".*"$`,
		DataLabel: "data_caa_txt", TTLDefault: 3600},
	{Type: "CNAME", StoredType: "CNAME", NameRegex: `^[a-zA-Z0-9.\-*_]{0,255}$`,
		DataKind: "text", DataRegex: `^[a-zA-Z0-9.\-_]{1,255}$`, DataLabel: "data_cname_txt", TTLDefault: 3600},
	{Type: "DNAME", StoredType: "DNAME", NameRegex: `^[a-zA-Z0-9.\-*_]{0,255}$`,
		DataKind: "text", DataRegex: `^[a-zA-Z0-9.\-_]{1,255}$`, DataLabel: "data_dname_txt", TTLDefault: 3600},
	{Type: "DS", StoredType: "DS", NameRegex: `^[a-zA-Z0-9.\-_]{0,255}$`,
		DataKind: "text", DataRegex: `^\d{1,5}\s\d{1,2}\s\d{1,2}\s.+$`, DataLabel: "data_ds_txt", TTLDefault: 3600},
	{Type: "HINFO", StoredType: "HINFO", NameRegex: `^[a-zA-Z0-9.\-]{1,64}$`, NameRequired: true,
		DataKind: "text", DataLabel: "data_hinfo_txt", TTLDefault: 3600},
	{Type: "LOC", StoredType: "LOC", NameRegex: `^[a-zA-Z0-9.\-_]{0,255}$`,
		DataKind: "text", DataLabel: "data_loc_txt", TTLDefault: 3600},
	{Type: "MX", StoredType: "MX", NameRegex: `^[a-zA-Z0-9.\-*]{0,255}$`,
		DataKind: "text", DataRegex: hostnameRegex, DataLabel: "data_mx_txt",
		AuxUsed: true, AuxLabel: "priority_txt", AuxDefault: 10, TTLDefault: 3600},
	{Type: "NAPTR", StoredType: "NAPTR",
		NameRegex: `^((\*|[a-zA-Z0-9\-_]{1,255})(\.[a-zA-Z0-9\-_]{1,255})*\.?)?$`,
		DataKind:  "text", DataRegex: `^\s*(\d+)\s+"([a-zA-Z0-9]*)"\s+"([^"]*)"\s+"(.*)"\s+([^\s]*\.)\s*$`,
		DataLabel: "data_naptr_txt", AuxUsed: true, AuxLabel: "order_txt", AuxDefault: 100, TTLDefault: 3600},
	{Type: "NS", StoredType: "NS", NameRegex: `^[_a-zA-Z0-9.\-]{0,255}$`,
		DataKind: "text", DataRegex: hostnameRegex, DataLabel: "data_ns_txt", TTLDefault: 3600},
	{Type: "PTR", StoredType: "PTR", NameRegex: `^[a-zA-Z0-9.\-]{1,256}$`, NameRequired: true,
		DataKind: "text", DataRegex: `^[a-zA-Z0-9.\-]{1,256}$`, DataLabel: "data_ptr_txt", TTLDefault: 3600},
	{Type: "RP", StoredType: "RP", NameRegex: `^[a-zA-Z0-9.\-]{0,255}$`,
		DataKind: "text", DataRegex: `^[\w.\-\s]{1,128}$`, DataLabel: "data_rp_txt", TTLDefault: 3600},
	{Type: "SRV", StoredType: "SRV", NameRegex: `^[a-zA-Z0-9.\-_]{0,255}$`,
		DataKind: "text", DataRegex: `^[\w.\-]{0,64}\s[\w.\-]{0,64}\s[\w.\-]{0,64}$`,
		DataLabel: "data_srv_txt", AuxUsed: true, AuxLabel: "priority_txt", TTLDefault: 3600},
	{Type: "SSHFP", StoredType: "SSHFP", NameRegex: `^(\*\.|[a-zA-Z0-9.\-_]){0,255}$`,
		DataKind: "text", DataLabel: "data_sshfp_txt", TTLDefault: 3600},
	{Type: "TLSA", StoredType: "TLSA", NameRegex: `^_\d{1,5}\._(tcp|udp)\.[a-zA-Z0-9.\-]{1,255}$`,
		NameRequired: true, DataKind: "text", DataRegex: `^\d \d \d [a-zA-Z0-9]*$`,
		DataLabel: "data_tlsa_txt", TTLDefault: 7200},
	{Type: "TXT", StoredType: "TXT", NameRegex: `^(\*\.|[a-zA-Z0-9.\-_]){0,255}$`,
		DataKind:        "text",
		DataNotContains: []string{"v=DKIM", "v=DMARC1; ", "v=spf"},
		DataLabel:       "data_txt_txt", TTLDefault: 3600},
	// TXT-derived helper forms (stored as TXT, design D8).
	{Type: "SPF", StoredType: "TXT", NameRegex: `^(\*\.|[a-zA-Z0-9.\-_]){0,255}$`,
		DataKind: "text", DataPrefix: "v=spf1", DataLabel: "data_spf_txt", TTLDefault: 3600},
	{Type: "DKIM", StoredType: "TXT", NameRegex: `^[a-zA-Z0-9.\-_]{0,255}$`,
		DataKind: "text", DataPrefix: "v=DKIM1", DataLabel: "data_dkim_txt", TTLDefault: 3600},
	{Type: "DMARC", StoredType: "TXT", NameRegex: `^[a-zA-Z0-9.\-_]{0,255}$`,
		DataKind: "text", DataPrefix: "v=DMARC1", DataLabel: "data_dmarc_txt", TTLDefault: 3600},
}

// dnsRecordTypeByName resolves a descriptor by its API discriminator
// (case-insensitive).
func dnsRecordTypeByName(name string) (*DNSRecordType, bool) {
	for i := range dnsRecordTypes {
		if strings.EqualFold(dnsRecordTypes[i].Type, name) {
			return &dnsRecordTypes[i], true
		}
	}
	return nil, false
}

// validateDNSRecord checks name/data/aux/ttl against the descriptor and
// returns the per-field i18n error keys (empty when valid). It mirrors the
// dns_<type>.tform.php validator sets.
func validateDNSRecord(rt *DNSRecordType, name, data string, aux, ttl int64) map[string][]string {
	fields := map[string][]string{}
	if rt.NameRequired && name == "" {
		fields["name"] = append(fields["name"], "name_error_empty")
	}
	if re, err := regexp.Compile(rt.NameRegex); err == nil && !re.MatchString(name) {
		fields["name"] = append(fields["name"], "name_error_regex")
	}

	switch {
	case data == "":
		fields["data"] = append(fields["data"], "data_error_empty")
	case rt.DataKind == "ipv4":
		if addr, err := netip.ParseAddr(data); err != nil || !addr.Is4() {
			fields["data"] = append(fields["data"], "ip_error_wrong")
		}
	case rt.DataKind == "ipv6":
		if addr, err := netip.ParseAddr(data); err != nil || !addr.Is6() {
			fields["data"] = append(fields["data"], "ip_error_wrong")
		}
	default:
		if rt.DataRegex != "" {
			if re, err := regexp.Compile(rt.DataRegex); err == nil && !re.MatchString(data) {
				fields["data"] = append(fields["data"], "data_error_regex")
			}
		}
		if rt.DataPrefix != "" && !strings.HasPrefix(data, rt.DataPrefix) {
			fields["data"] = append(fields["data"], "data_error_regex")
		}
		for _, bad := range rt.DataNotContains {
			if strings.Contains(data, bad) {
				// A dedicated form exists for this payload (SPF/DKIM/DMARC).
				fields["data"] = append(fields["data"], "data_error_use_dedicated_form")
			}
		}
	}

	if aux < 0 || aux > 65535 {
		fields["aux"] = append(fields["aux"], "aux_error_range")
	}
	if ttl < 60 {
		fields["ttl"] = append(fields["ttl"], "ttl_range_error")
	}
	return fields
}

// registerDNSRecordTypeRoutes mounts the record-type metadata export.
func registerDNSRecordTypeRoutes(g *echo.Group) {
	g.GET("/record-types", recordTypesHandler)
}

// recordTypesHandler implements GET /api/dns/record-types.
//
//	@Summary		DNS record type metadata
//	@Description	The declarative descriptor table of every supported DNS record type (18 dns_rr types plus the TXT-derived SPF/DKIM/DMARC helper forms): stored type, name/data validation rules, aux usage and defaults. The Vue record editor renders its add/edit dialog from this metadata — the same source of truth the API validates against.
//	@Tags			dns
//	@Produce		json
//	@Success		200	{array}		DNSRecordType
//	@Failure		401	{object}	ErrorResponse
//	@Router			/dns/record-types [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func recordTypesHandler(c *echo.Context) error {
	return c.JSON(http.StatusOK, dnsRecordTypes)
}
