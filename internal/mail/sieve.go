package mail

import (
	"context"
	"strings"

	"go-ispconfig/internal/mastertpl"
)

// sieveTemplate is the embedded template name (stock ISPConfig
// sieve_filter.master; conf-custom overrides via the daemon's custom
// template dir work like every other .master).
const sieveTemplate = "sieve_filter.master"

// sieveRelevantFields are the mail_user fields whose change triggers a
// sieve re-render (maildeliver_plugin::update condition list).
var sieveRelevantFields = []string{
	"custom_mailfilter", "move_junk",
	"autoresponder_subject", "autoresponder_text", "autoresponder",
	"email", "autoresponder_start_date", "autoresponder_end_date",
	"cc", "forward_in_lda",
}

// sieveRelevantChanged reports whether any maildeliver-relevant field
// differs between old and new.
func sieveRelevantChanged(oldRow, newRow row) bool {
	for _, f := range sieveRelevantFields {
		if oldRow.str(f) != newRow.str(f) {
			return true
		}
	}
	return false
}

// sieveAddressStr renders the vacation :addresses list exactly like the
// PHP plugin (`:addresses ["a","b"]`; empty for no addresses).
func sieveAddressStr(addresses []string) string {
	if len(addresses) == 0 {
		return ""
	}
	quoted := make([]string, len(addresses))
	for i, a := range addresses {
		quoted[i] = `"` + a + `"`
	}
	return ":addresses [" + strings.Join(quoted, ",") + "]"
}

// collectSieveAddresses gathers the identity list for the vacation
//
// filter: the mailbox address, alias sources pointing at it, and every
// address remapped onto alias domains of the mail domain (PHP order and
// de-duplication preserved).
//
//nolint:unused // wired by the sieve write handler (task 4.3).
func (p *Plugin) collectSieveAddresses(ctx context.Context, email string) []string {
	addresses := []string{email}
	var aliasSources []string
	err := p.db.WithContext(ctx).Table("mail_forwarding").
		Where("type = 'alias' AND destination = ?", email).
		Order("forwarding_id").Pluck("source", &aliasSources).Error
	if err != nil {
		p.log.Error("mail: alias lookup for sieve failed", "email", email, "error", err)
	}
	addresses = append(addresses, aliasSources...)

	at := strings.LastIndex(email, "@")
	if at < 0 {
		return addresses
	}
	var aliasDomains []string
	err = p.db.WithContext(ctx).Table("mail_forwarding").
		Where("type = 'aliasdomain' AND destination = ?", "@"+email[at+1:]).
		Order("forwarding_id").Pluck("source", &aliasDomains).Error
	if err != nil {
		p.log.Error("mail: aliasdomain lookup for sieve failed", "email", email, "error", err)
	}
	var mapped []string
	for _, src := range aliasDomains {
		domain := strings.TrimPrefix(src, "@")
		for _, addr := range addresses {
			if i := strings.LastIndex(addr, "@"); i >= 0 {
				mapped = append(mapped, addr[:i]+"@"+domain)
			}
		}
	}
	// array_unique(array_merge(...)) keeps the first occurrence order.
	seen := map[string]struct{}{}
	var out []string
	for _, a := range append(addresses, mapped...) {
		if _, dup := seen[a]; !dup {
			seen[a] = struct{}{}
			out = append(out, a)
		}
	}
	return out
}

// sieveVars builds the template vector from the new mail_user payload
// (maildeliver_plugin::update parity: CRLF-normalized custom filter,
// space→T sieve dates, double quotes → single in subject/text).
func sieveVars(newRow row, addresses []string) map[string]any {
	vars := map[string]any{
		"custom_mailfilter":     strings.ReplaceAll(newRow.str("custom_mailfilter"), "\r\n", "\n"),
		"move_junk":             newRow.str("move_junk"),
		"imap_prefix":           newRow.str("imap_prefix"),
		"start_date":            strings.ReplaceAll(newRow.str("autoresponder_start_date"), " ", "T"),
		"end_date":              strings.ReplaceAll(newRow.str("autoresponder_end_date"), " ", "T"),
		"autoresponder":         newRow.str("autoresponder"),
		"autoresponder_subject": strings.ReplaceAll(newRow.str("autoresponder_subject"), `"`, "'"),
		"autoresponder_text":    strings.ReplaceAll(newRow.str("autoresponder_text"), `"`, "'"),
		"addresses":             sieveAddressStr(addresses),
	}
	if newRow.str("forward_in_lda") == "y" && newRow.str("cc") != "" {
		vars["cc"] = newRow.str("cc")
		var loop []map[string]any
		for _, address := range strings.Split(newRow.str("cc"), ",") {
			if a := strings.TrimSpace(address); a != "" {
				loop = append(loop, map[string]any{"address": a})
			}
		}
		vars["ccloop"] = loop
	}
	return vars
}

// renderSieve renders the before or after script from the template
// source and vector.
func renderSieve(src, script string, vars map[string]any) (string, error) {
	tpl := mastertpl.New(src)
	for k, v := range vars {
		if k == "ccloop" {
			if rows, ok := v.([]map[string]any); ok {
				tpl.SetLoop(k, rows)
			}
			continue
		}
		tpl.SetVar(k, v)
	}
	tpl.SetVar("sieve_script", script)
	return tpl.Render()
}
