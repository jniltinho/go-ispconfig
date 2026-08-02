package importer

import (
	"fmt"
	"net/url"
	"strings"

	"go-ispconfig/internal/model"
)

// LegacyHost extracts the host (no port, no scheme, IPv6-safe) from the
// legacy panel URL, for rsync suggestions and report display.
func LegacyHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return strings.TrimSuffix(rawURL, "/")
	}
	return u.Hostname()
}

// ReportInput carries the run context the report must echo.
type ReportInput struct {
	// LegacyHost is the legacy panel host, used in rsync suggestions.
	LegacyHost string
	// Insecure marks a run with TLS verification disabled.
	Insecure bool
	// PlainHTTP marks a run over unencrypted http://.
	PlainHTTP bool
	// MultiServer marks a multi-server legacy panel mapped onto the
	// single local server after explicit operator confirmation.
	MultiServer bool
}

// Report is the final import report rendered by the CLI and the wizard.
type Report struct {
	// Counts is the per-table created/updated/skipped/conflict tally.
	Counts map[string]EntityCount `json:"counts"`
	// Conflicts lists every conflicting record with its reason.
	Conflicts []Item `json:"conflicts"`
	// ResetRequired lists panel users created with an unusable password;
	// the bulk reset flow issues their one-time tokens.
	ResetRequired []string `json:"reset_required"`
	// Warnings collects everything the operator must not miss: insecure
	// transport, multi-server mapping, SSL re-issue, orphan assignments.
	Warnings []string `json:"warnings"`
	// RsyncSuggestions is one suggested command per imported vhost for
	// transferring site files (file transfer itself is out of scope),
	// including uid/gid remapping.
	RsyncSuggestions []string `json:"rsync_suggestions"`
	// OperationalOrder states the mandatory post-import sequence.
	OperationalOrder []string `json:"operational_order"`
}

// BuildReport assembles the final report from the plan, the apply tally
// and the run context.
func BuildReport(plan *Plan, counts map[string]EntityCount, in ReportInput) *Report {
	r := &Report{
		Counts:        counts,
		Conflicts:     plan.Conflicts(),
		ResetRequired: plan.ResetRequired(),
		OperationalOrder: []string{
			"1. Wait for the go-ispconfig daemon to drain sys_datalog (vhosts and zones written).",
			"2. Transfer site files with the suggested rsync commands (uid/gid remapped).",
			"3. Only then enable SSL / trigger Let's Encrypt issuance — the webroot challenge fails on an empty docroot; legacy certificates are not reused.",
			"4. Lower DNS TTLs, switch DNS/IPs, then decommission the legacy host.",
		},
	}

	if in.Insecure {
		r.Warnings = append(r.Warnings,
			"TLS certificate verification was DISABLED for the legacy connection (insecure mode).")
	}
	if in.PlainHTTP {
		r.Warnings = append(r.Warnings,
			"The legacy panel was reached over plain http:// — credentials traveled unencrypted.")
	}
	if in.MultiServer {
		r.Warnings = append(r.Warnings,
			"The legacy panel reported multiple servers; every record was mapped onto the single local server.")
	}
	r.Warnings = append(r.Warnings, plan.Warnings...)

	sslSites := 0
	for _, it := range plan.Items {
		if it.Table != "web_domain" || it.Action == ActionConflict {
			continue
		}
		dom, ok := it.rec.(*model.WebDomain)
		if !ok {
			continue
		}
		if dom.SSL == "y" || dom.SSLLetsencrypt == "y" {
			sslSites++
		}
		if dom.Type == "vhost" && dom.DocumentRoot != "" {
			r.RsyncSuggestions = append(r.RsyncSuggestions, fmt.Sprintf(
				"rsync -a --usermap=*:%s --groupmap=*:%s %s:%s/ %s/   # or afterwards: chown -R %s:%s %s",
				dom.SystemUser, dom.SystemGroup,
				in.LegacyHost, dom.DocumentRoot, dom.DocumentRoot,
				dom.SystemUser, dom.SystemGroup, dom.DocumentRoot))
		}
	}
	if sslSites > 0 {
		r.Warnings = append(r.Warnings, fmt.Sprintf(
			"%d imported site(s) have SSL enabled: certificates must be re-issued on the new host after the site files are transferred. Imported certificate/key material was also journaled to sys_datalog — treat it as exposed and rotate after migration.", sslSites))
	}
	return r
}
