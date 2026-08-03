package powerdns

import (
	"context"
	"fmt"
	"strings"

	"go-ispconfig/internal/engine"
)

// ServiceName is the delayed services registry key (PHP registers 'powerdns').
const ServiceName = "powerdns"

// controlTools holds discovered binary paths (empty = missing).
type controlTools struct {
	pdnsControl string
	pdnsUtil    string
	version     string
}

// ensureTools lazily discovers pdns_control and pdnssec/pdnsutil via the
// CommandRunner (PHP `type -p` parity: try bare name + common prefixes,
// missing binaries are non-fatal).

func (p *Plugin) ensureTools(ctx context.Context) *controlTools {
	if p.tools != nil {
		return p.tools
	}
	t := &controlTools{}
	if p.runner == nil {
		p.tools = t
		return t
	}
	// Prefer absolute paths from common prefixes, then bare names.
	t.pdnsControl = findBinary(ctx, p.runner, "pdns_control")
	// PHP prefers pdnssec (v3) then pdnsutil (v4+).
	t.pdnsUtil = findBinary(ctx, p.runner, "pdnssec")
	if t.pdnsUtil == "" {
		t.pdnsUtil = findBinary(ctx, p.runner, "pdnsutil")
	}
	if t.pdnsControl != "" {
		if out, err := p.runner.Run(ctx, t.pdnsControl, "version"); err == nil {
			t.version = strings.TrimSpace(string(out))
		}
	}
	p.tools = t
	return t
}

// findBinary returns the first candidate that responds to `--help` or
// `version` without "not found" style failures. Uses bare name (PATH).
func findBinary(ctx context.Context, runner engine.CommandRunner, name string) string {
	candidates := []string{
		name,
		"/usr/bin/" + name,
		"/usr/sbin/" + name,
		"/bin/" + name,
	}
	for _, c := range candidates {
		// Probe with a harmless arg; missing binary yields error from exec.
		if _, err := runner.Run(ctx, c, "version"); err == nil {
			return c
		}
		// Some tools only accept --version; try once more.
		if out, err := runner.Run(ctx, c, "--version"); err == nil && len(out) > 0 {
			return c
		}
	}
	return ""
}

// VersionSupported reports whether major version starts with 3 or 4 (PHP
// is_pdns_version_supported).
func VersionSupported(version string) bool {
	v := strings.TrimSpace(version)
	if v == "" {
		return false
	}
	// Take first character of the version string (PHP preg_match /^[34]/).
	return v[0] == '3' || v[0] == '4'
}

func (p *Plugin) doRediscover(ctx context.Context) {
	t := p.ensureTools(ctx)
	if t.pdnsControl == "" {
		return
	}
	if _, err := p.runner.Run(ctx, t.pdnsControl, "rediscover"); err != nil {
		p.log.Warn("powerdns: rediscover failed", "error", err)
	}
}

func (p *Plugin) doNotify(ctx context.Context, data engine.Data) {
	t := p.ensureTools(ctx)
	if t.pdnsControl == "" {
		return
	}
	origin := stripTrailingDot(row(data.New).str("origin"))
	if origin == "" {
		return
	}
	if _, err := p.runner.Run(ctx, t.pdnsControl, "notify", origin); err != nil {
		p.log.Warn("powerdns: notify failed", "origin", origin, "error", err)
	}
}

func (p *Plugin) doRetrieve(ctx context.Context, data engine.Data) {
	t := p.ensureTools(ctx)
	if t.pdnsControl == "" {
		return
	}
	origin := stripTrailingDot(row(data.New).str("origin"))
	if origin == "" {
		return
	}
	if _, err := p.runner.Run(ctx, t.pdnsControl, "retrieve", origin); err != nil {
		p.log.Warn("powerdns: retrieve failed", "origin", origin, "error", err)
	}
}

// pdnsUtilTool returns the pdnsutil/pdnssec path when zone maintenance
// commands may run: binary present and, when the version probe succeeded,
// major version 3 or 4 (PHP is_pdns_version_supported). An empty version
// (pdns_control missing) stays permissive.
func (p *Plugin) pdnsUtilTool(ctx context.Context) string {
	t := p.ensureTools(ctx)
	if t.pdnsUtil == "" || (t.version != "" && !VersionSupported(t.version)) {
		return ""
	}
	return t.pdnsUtil
}

func (p *Plugin) doRectify(ctx context.Context, data engine.Data) {
	tool := p.pdnsUtilTool(ctx)
	if tool == "" {
		return
	}
	origin := stripTrailingDot(row(data.New).str("origin"))
	if origin == "" {
		// Resolve from PowerDNS by RR ispconfig_id (PHP query).
		id := int(row(data.New).num("id"))
		if id > 0 && p.pdnsDB != nil {
			var name string
			err := p.pdnsDB.WithContext(ctx).Raw(`
				SELECT d.name FROM domains d
				INNER JOIN records r ON r.domain_id = d.id
				WHERE r.ispconfig_id = ? LIMIT 1`, id).Scan(&name).Error
			if err == nil {
				origin = name
			}
		}
	}
	if origin == "" {
		return
	}
	if _, err := p.runner.Run(ctx, tool, "rectify-zone", origin); err != nil {
		p.log.Warn("powerdns: rectify-zone failed", "origin", origin, "error", err)
	}
}

// SetToolsForTest presets discovery results (unit tests).
func (p *Plugin) SetToolsForTest(pdnsControl, pdnsUtil, version string) {
	p.tools = &controlTools{
		pdnsControl: pdnsControl,
		pdnsUtil:    pdnsUtil,
		version:     version,
	}
}

// ProbeVersion returns the cached pdns version string (for tests/DNSSEC).
func (p *Plugin) ProbeVersion(ctx context.Context) string {
	return p.ensureTools(ctx).version
}

// Format tools status for errors.
func (t *controlTools) String() string {
	if t == nil {
		return "tools:<nil>"
	}
	return fmt.Sprintf("control=%q util=%q version=%q", t.pdnsControl, t.pdnsUtil, t.version)
}
