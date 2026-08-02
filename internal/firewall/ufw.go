package firewall

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"go-ispconfig/internal/engine"
)

// minUFWVersion is the minimum supported UFW version (PHP parity).
const minUFWVersion = "0.30"

// ufwUpdate is the insert/update apply path (port of
// firewall_plugin::ufw_update). event is "firewall_insert" or
// "firewall_update".
//
// Pipeline (design D5):
//  1. Probe ufw install + version ≥ 0.30; else warn and return.
//  2. On insert only: force disable, force reset, default deny in / allow out.
//  3. Diff cleaned tcp/udp old vs new; allow added, delete removed.
//  4. active=y → enable (fresh) or reload (already y); else disable.
//
// Lock-out guard (task 2.4) extends the effective TCP set; until then
// this path applies the cleaned record ports only.
func (p *Plugin) ufwUpdate(ctx context.Context, event string, data engine.Data) error {
	if !p.isLocal(data) {
		p.log.Debug("firewall: skipping non-local server_id",
			"event", event, "local", p.serverID, "payload", payloadServerID(data))
		return nil
	}
	if err := p.ensureUFW(ctx); err != nil {
		p.log.Warn("firewall: UFW not available, apply skipped", "error", err)
		return nil
	}

	newRow, oldRow := row(data.New), row(data.Old)

	if event == "firewall_insert" {
		if err := p.insertBaseline(ctx); err != nil {
			return err
		}
	}

	tcpNew := splitPorts(CleanPorts(newRow.str("tcp_port"), ","))
	tcpOld := splitPorts(CleanPorts(oldRow.str("tcp_port"), ","))
	udpNew := splitPorts(CleanPorts(newRow.str("udp_port"), ","))
	udpOld := splitPorts(CleanPorts(oldRow.str("udp_port"), ","))

	if err := p.diffAllowDelete(ctx, "tcp", tcpNew, tcpOld); err != nil {
		return err
	}
	if err := p.diffAllowDelete(ctx, "udp", udpNew, udpOld); err != nil {
		return err
	}

	return p.applyActive(ctx, newRow.str("active"), oldRow.str("active"))
}

// insertBaseline runs the first-time UFW policy (PHP insert branch).
func (p *Plugin) insertBaseline(ctx context.Context) error {
	steps := [][]string{
		{"--force", "disable"},
		{"--force", "reset"},
		{"default", "deny", "incoming"},
		{"default", "allow", "outgoing"},
	}
	for _, args := range steps {
		if _, err := p.runner.Run(ctx, "ufw", args...); err != nil {
			return fmt.Errorf("firewall: ufw %s: %w", strings.Join(args, " "), err)
		}
	}
	return nil
}

// diffAllowDelete issues allow for ports only in new and delete allow for
// ports only in old (proto is "tcp" or "udp").
func (p *Plugin) diffAllowDelete(ctx context.Context, proto string, newPorts, oldPorts []string) error {
	oldSet := portSet(oldPorts)
	newSet := portSet(newPorts)

	for _, port := range newPorts {
		if port == "" || port == "0" {
			continue
		}
		if _, ok := oldSet[port]; ok {
			continue
		}
		arg := port + "/" + proto
		if _, err := p.runner.Run(ctx, "ufw", "allow", arg); err != nil {
			return fmt.Errorf("firewall: ufw allow %s: %w", arg, err)
		}
		p.log.Debug("firewall: ufw allow", "rule", arg)
	}
	for _, port := range oldPorts {
		if port == "" || port == "0" {
			continue
		}
		if _, ok := newSet[port]; ok {
			continue
		}
		arg := port + "/" + proto
		if _, err := p.runner.Run(ctx, "ufw", "delete", "allow", arg); err != nil {
			return fmt.Errorf("firewall: ufw delete allow %s: %w", arg, err)
		}
		p.log.Debug("firewall: ufw delete allow", "rule", arg)
	}
	return nil
}

// applyActive maps the active flag to enable/reload/disable (PHP parity).
func (p *Plugin) applyActive(ctx context.Context, newActive, oldActive string) error {
	if newActive == "y" {
		if newActive == oldActive {
			if _, err := p.runner.Run(ctx, "ufw", "reload"); err != nil {
				return fmt.Errorf("firewall: ufw reload: %w", err)
			}
			p.log.Debug("firewall: reloading UFW")
			return nil
		}
		if _, err := p.runner.Run(ctx, "ufw", "--force", "enable"); err != nil {
			return fmt.Errorf("firewall: ufw --force enable: %w", err)
		}
		p.log.Debug("firewall: enabling UFW")
		return nil
	}
	if _, err := p.runner.Run(ctx, "ufw", "disable"); err != nil {
		return fmt.Errorf("firewall: ufw disable: %w", err)
	}
	p.log.Debug("firewall: disabling UFW")
	return nil
}

// ensureUFW verifies ufw is installed and version ≥ minUFWVersion.
// Returns a descriptive error when the apply must be skipped (caller logs).
func (p *Plugin) ensureUFW(ctx context.Context) error {
	out, err := p.runner.Run(ctx, "ufw", "--version")
	if err != nil {
		return fmt.Errorf("ufw is not installed or not runnable: %w", err)
	}
	ver, err := parseUFWVersion(string(out))
	if err != nil {
		return err
	}
	if compareVersion(ver, minUFWVersion) < 0 {
		return fmt.Errorf("UFW version %s is too old (minimum %s)", ver, minUFWVersion)
	}
	return nil
}

// ufwDelete is the delete apply path (port of firewall_plugin::ufw_delete):
// force reset then disable. Non-local server_id is a no-op; missing/old
// UFW logs a warning and returns without error (PHP parity).
func (p *Plugin) ufwDelete(ctx context.Context, event string, data engine.Data) error {
	if !p.isLocal(data) {
		p.log.Debug("firewall: skipping non-local server_id",
			"event", event, "local", p.serverID, "payload", payloadServerID(data))
		return nil
	}
	if err := p.ensureUFW(ctx); err != nil {
		p.log.Warn("firewall: UFW not available, delete skipped", "error", err)
		return nil
	}
	if _, err := p.runner.Run(ctx, "ufw", "--force", "reset"); err != nil {
		return fmt.Errorf("firewall: ufw --force reset: %w", err)
	}
	if _, err := p.runner.Run(ctx, "ufw", "disable"); err != nil {
		return fmt.Errorf("firewall: ufw disable: %w", err)
	}
	p.log.Debug("firewall: stopped UFW after record delete")
	return nil
}

// parseUFWVersion extracts the version token from `ufw --version` output
// (first line typically "ufw 0.36.1").
func parseUFWVersion(out string) (string, error) {
	line := strings.TrimSpace(out)
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	parts := strings.Fields(line)
	if len(parts) < 2 {
		return "", fmt.Errorf("could not parse ufw version from %q", out)
	}
	// parts[0] is "ufw", parts[1] is the version.
	return parts[1], nil
}

// compareVersion compares dotted numeric versions (e.g. "0.30" vs "0.36.1").
// Returns -1, 0, or 1. Non-numeric segments compare as 0.
func compareVersion(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		var ai, bi int
		if i < len(as) {
			ai, _ = strconv.Atoi(as[i])
		}
		if i < len(bs) {
			bi, _ = strconv.Atoi(bs[i])
		}
		if ai < bi {
			return -1
		}
		if ai > bi {
			return 1
		}
	}
	return 0
}
