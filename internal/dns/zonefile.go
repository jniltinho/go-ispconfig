package dns

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go-ispconfig/internal/getconf"
)

// domainOf strips the trailing dot of a zone origin ("example.com." ->
// "example.com").
func domainOf(origin string) string { return strings.TrimSuffix(origin, ".") }

// zoneFileBase converts an origin into its zone file base name: trailing
// dot stripped and "/" replaced by "_" (classless in-addr.arpa delegation
// origins contain slashes).
func zoneFileBase(origin string) string {
	return strings.ReplaceAll(domainOf(origin), "/", "_")
}

// zoneFilePath returns the primary zone file path of an origin.
func zoneFilePath(cfg *getconf.DNSConfig, origin string) string {
	return filepath.Join(cfg.BindZonefilesDir, cfg.BindZonefilesMasterPfx+zoneFileBase(origin))
}

// slaveZoneFilePath returns the secondary zone file path of an origin.
// The slave prefix may contain a subdirectory ("slave/sec.").
func slaveZoneFilePath(cfg *getconf.DNSConfig, origin string) string {
	return filepath.Join(cfg.BindZonefilesDir, cfg.BindZonefilesSlavePfx+zoneFileBase(origin))
}

// chown changes ownership of path through the command runner (the daemon
// runs as root; tests inject a fake runner).
func (p *Plugin) chown(ctx context.Context, path, user, group string) error {
	if out, err := p.runner.Run(ctx, "chown", user+":"+group, path); err != nil {
		return fmt.Errorf("dns: chown %s: %w: %s", path, err, out)
	}
	return nil
}

// writeZoneFile writes the rendered zone to its file owned by the bind
// user/group and returns the previous file content ("" when none) for the
// validation rollback.
func (p *Plugin) writeZoneFile(ctx context.Context, cfg *getconf.DNSConfig, filename, content string) (oldContent string, err error) {
	if prev, readErr := os.ReadFile(filename); readErr == nil {
		oldContent = string(prev)
	}
	if err := os.WriteFile(filename, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("dns: writing zone file %s: %w", filename, err)
	}
	if err := p.chown(ctx, filename, cfg.BindUser, cfg.BindGroup); err != nil {
		return "", err
	}
	p.log.Info("dns: wrote zone file", "file", filename)
	return oldContent, nil
}
