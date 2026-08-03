package powerdns

// DNSSEC via native PowerDNS tools (port of powerdns_plugin.inc.php
// handle_dnssec / soa_dnssec_* / format_dnssec_pubkeys, design D6).
// Diverges from the Bind flow by design: keys live inside PowerDNS,
// there is no zone signing on disk and no dns_resign job.

import (
	"context"
	"regexp"
	"strings"
	"time"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/model"
)

// rawLogHeader separates the parsed key block from the command log inside
// dns_soa.dnssec_info (PHP parity).
const rawLogHeader = "== Raw log ============================"

// handleDNSSEC ports handle_dnssec: origin change drops the old keys first,
// then disable (wanted Y→N) or create (wanted →Y) run for the new origin.
func (p *Plugin) doHandleDNSSEC(ctx context.Context, data engine.Data) {
	n, o := row(data.New), row(data.Old)
	id := int(n.num("id"))
	if o.str("origin") != n.str("origin") &&
		o.str("dnssec_initialized") == "Y" && len(o.str("origin")) > 3 {
		p.dnssecDelete(ctx, id, stripTrailingDot(o.str("origin")))
	}
	zone := stripTrailingDot(n.str("origin"))
	switch {
	case n.str("dnssec_wanted") == "N" && o.str("dnssec_wanted") == "Y":
		p.dnssecDisable(ctx, id, zone)
	case n.str("dnssec_wanted") == "Y" && (o.str("dnssec_wanted") == "" || o.str("dnssec_wanted") == "N"):
		p.dnssecCreate(ctx, id, zone)
	}
}

// dnssecCreate ports soa_dnssec_create: KSK + ZSK + NSEC3, then show-zone
// parsed into dns_soa.dnssec_info with dnssec_initialized='Y'.
func (p *Plugin) dnssecCreate(ctx context.Context, id int, zone string) {
	tool := p.pdnsUtilTool(ctx)
	if tool == "" || zone == "" {
		return
	}
	var log []string
	run := p.dnssecRunner(ctx, tool, zone, &log)
	run("add-zone-key ksk", "add-zone-key", zone, "ksk", "active", "2048", "rsasha256")
	run("add-zone-key zsk", "add-zone-key", zone, "zsk", "active", "1024", "rsasha256")
	run("set-nsec3", "set-nsec3", zone, "1 0 10 deadbeef")
	pubkeys := run("show-zone", "show-zone", zone)

	info := append(FormatDNSSECPubkeys(pubkeys), "", rawLogHeader)
	info = append(info, log...)
	p.updateSOADNSSEC(ctx, id, map[string]any{
		"dnssec_info":        strings.Join(info, "\r\n"),
		"dnssec_initialized": "Y",
	})
}

// dnssecDisable ports soa_dnssec_disable: keys stay in dnssec_info, only the
// initialized flag flips.
func (p *Plugin) dnssecDisable(ctx context.Context, id int, zone string) {
	tool := p.pdnsUtilTool(ctx)
	if tool == "" || zone == "" {
		return
	}
	var log []string
	p.dnssecRunner(ctx, tool, zone, &log)("disable-dnssec", "disable-dnssec", zone)
	p.updateSOADNSSEC(ctx, id, map[string]any{"dnssec_initialized": "N"})
}

// dnssecDelete ports soa_dnssec_delete: disable against the *old* origin and
// replace dnssec_info with the raw log.
func (p *Plugin) dnssecDelete(ctx context.Context, id int, oldZone string) {
	tool := p.pdnsUtilTool(ctx)
	if tool == "" || oldZone == "" {
		return
	}
	var log []string
	p.dnssecRunner(ctx, tool, oldZone, &log)("disable-dnssec", "disable-dnssec", oldZone)
	p.updateSOADNSSEC(ctx, id, map[string]any{
		"dnssec_info":        strings.Join(append([]string{rawLogHeader}, log...), "\r\n"),
		"dnssec_initialized": "N",
	})
}

// dnssecRunner returns a helper that runs one pdnsutil subcommand, appends
// the PHP-style header plus output to log, and returns the output lines.
// Command arguments are deliberately not logged (PHP: they trip the panel IDS).
func (p *Plugin) dnssecRunner(ctx context.Context, tool, zone string, log *[]string) func(desc string, args ...string) []string {
	return func(desc string, args ...string) []string {
		*log = append(*log, "\r\n"+time.Now().Format(time.RFC3339)+" Running "+desc+" command...")
		out, err := p.runner.Run(ctx, tool, args...)
		lines := outputLines(out)
		if err != nil {
			p.log.Warn("powerdns: dnssec command failed", "command", desc, "zone", zone, "error", err)
			lines = append(lines, "error: "+err.Error())
		}
		*log = append(*log, lines...)
		return lines
	}
}

// updateSOADNSSEC writes the DNSSEC columns back to the panel dns_soa row.
func (p *Plugin) updateSOADNSSEC(ctx context.Context, id int, updates map[string]any) {
	if p.panelDB == nil || id == 0 {
		return
	}
	if err := p.panelDB.WithContext(ctx).Model(&model.DNSSoa{}).
		Where("id = ?", id).Updates(updates).Error; err != nil {
		p.log.Warn("powerdns: update dns_soa dnssec fields", "id", id, "error", err)
	}
}

// outputLines splits command output into lines the way PHP exec() does.
func outputLines(out []byte) []string {
	s := strings.TrimRight(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// show-zone line parsers (PHP format_dnssec_pubkeys regexes).
var (
	reKeyType = regexp.MustCompile(`(KSK|ZSK|CSK)`)
	reKeyTag  = regexp.MustCompile(` tag = (\d+),`)
	reAlgoID  = regexp.MustCompile(` algo = (\d+),`)
	reParen   = regexp.MustCompile(` \( (.*) \)$`)
	reBits    = regexp.MustCompile(` bits = (\d+)`)
	reDNSKEY  = regexp.MustCompile(` IN DNSKEY \d+ \d+ \d+ (.*) ;`)
	reDS      = regexp.MustCompile(` IN DS (\d+) (\d+) (\d+) (.*) ;`)
)

// FormatDNSSECPubkeys ports format_dnssec_pubkeys: turn `pdnsutil|pdnssec
// show-zone` output into the human-readable block stored in
// dns_soa.dnssec_info. Only active KSK/CSK key lines ("Active: 1" is
// pdnssec/PowerDNS 3.x, "Active ( " is pdnsutil/4.x) plus DNSKEY and DS
// lines are kept; the first two lines (presigning/NSEC info) are skipped.
func FormatDNSSECPubkeys(lines []string) []string {
	if len(lines) > 2 {
		lines = lines[2:]
	} else {
		lines = nil
	}
	var formatted []string
	appendMatch := func(re *regexp.Regexp, line, format string) {
		if m := re.FindStringSubmatch(line); m != nil {
			formatted = append(formatted, format+m[1])
		}
	}
	for _, line := range lines {
		if len(line) < 3 {
			continue
		}
		switch line[:3] {
		case "ID ":
			if !containsActive(line) {
				continue
			}
			keyType := reKeyType.FindString(line)
			if keyType != "KSK" && keyType != "CSK" {
				continue
			}
			appendMatch(reKeyTag, line, keyType+" key tag: ")
			if algo := reAlgoID.FindStringSubmatch(line); algo != nil {
				name := ""
				if m := reParen.FindStringSubmatch(line); m != nil {
					name = m[1]
				}
				formatted = append(formatted, "Algo: "+algo[1]+" ("+name+")")
			}
			appendMatch(reBits, line, "Bits: ")
		case "KSK", "CSK":
			appendMatch(reDNSKEY, line, "DNSKEY: ")
		case "DS ":
			m := reDS.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			name := ""
			if p := reParen.FindStringSubmatch(line); p != nil {
				name = p[1]
			}
			formatted = append(formatted,
				"  - DS key tag: "+m[1],
				"    Algo: "+m[2],
				"    Digest: "+m[3]+" ("+name+")",
				"    Public key: "+m[4])
		}
	}
	return formatted
}

// containsActive reports whether a show-zone key line marks an active key
// ("Active: 1" on PowerDNS 3.x, "Active ( " on 4.x).
func containsActive(line string) bool {
	return strings.Contains(line, "Active: 1") || strings.Contains(line, "Active ( ")
}
