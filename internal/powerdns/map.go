package powerdns

import (
	"strconv"
	"strings"
)

// stripTrailingDot removes a single trailing '.' from s (PHP substr(..., 0, -1)
// when the last char is '.'). Empty stays empty.
func stripTrailingDot(s string) string {
	return strings.TrimSuffix(s, ".")
}

// absoluteOrRelative builds a PowerDNS FQDN name from a panel field that may
// be absolute (trailing '.') or relative to origin (already without trailing
// dot). Empty relative → origin. Matches powerdns_plugin name/content rules.
func absoluteOrRelative(field, origin string) string {
	if strings.HasSuffix(field, ".") {
		return stripTrailingDot(field)
	}
	if field == "" {
		return origin
	}
	out := field + "." + origin
	if out == "" {
		return origin
	}
	return out
}

// soaNS resolves the SOA nameserver field (absolute trailing dot, relative,
// or empty → origin).
func soaNS(ns, origin string) string {
	if strings.HasSuffix(ns, ".") {
		ns = stripTrailingDot(ns)
	} else if ns != "" {
		ns = ns + "." + origin
	}
	if ns == "" {
		return origin
	}
	return ns
}

// soaHostmaster strips the trailing dot from mbox (PHP substr mbox, 0, -1
// always — mbox is stored with trailing dot like hostmaster.example.com.).
func soaHostmaster(mbox string) string {
	return stripTrailingDot(mbox)
}

// SOAContent builds the PowerDNS SOA record content string:
// "<ns> <hostmaster> <serial> <refresh> <retry> <expire> <minimum>".
func SOAContent(ns, mbox, origin string, serial, refresh, retry, expire, minimum int64) string {
	return strings.Join([]string{
		soaNS(ns, origin),
		soaHostmaster(mbox),
		itoa(serial),
		itoa(refresh),
		itoa(retry),
		itoa(expire),
		itoa(minimum),
	}, " ")
}

// RRName maps a dns_rr.name field to a PowerDNS absolute name for origin.
func RRName(name, origin string) string {
	out := absoluteOrRelative(name, origin)
	if out == "" {
		return origin
	}
	return out
}

// nameTypes use absolute/relative content mapping (CNAME/MX/NS/ALIAS/PTR/SRV).
var nameTypes = map[string]bool{
	"CNAME": true,
	"MX":    true,
	"NS":    true,
	"ALIAS": true,
	"PTR":   true,
	"SRV":   true,
}

// RRContent maps dns_rr type + data to PowerDNS content (PHP switch).
func RRContent(typ, data, origin string) string {
	switch {
	case nameTypes[typ]:
		return absoluteOrRelative(data, origin)
	case typ == "HINFO":
		return transformHINFO(data)
	default:
		return data
	}
}

// transformHINFO ports the PHP quote/space-to-underscore transform from
// powerdns_plugin.inc.php rr_insert (HINFO case). Quirks of the PHP
// strpos-on-substr arithmetic are preserved on purpose.
func transformHINFO(content string) string {
	quote1 := strings.IndexByte(content, '"')
	if quote1 < 0 {
		return content
	}
	rest := content[quote1+1:]
	quote2 := strings.IndexByte(rest, '"') // relative to rest, as in PHP
	if quote2 < 0 {
		return content
	}
	// substr($content, quote1+1, quote2-quote1) then spaces → underscores.
	length := quote2 - quote1
	if length < 0 {
		return content
	}
	start := quote1 + 1
	end := start + length
	if end > len(content) {
		end = len(content)
	}
	between := strings.ReplaceAll(content[start:end], " ", "_")
	// substr($content, quote2+2) with $quote2 the relative index (PHP).
	remainderStart := quote2 + 2
	if remainderStart > len(content) {
		remainderStart = len(content)
	}
	return between + content[remainderStart:]
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
