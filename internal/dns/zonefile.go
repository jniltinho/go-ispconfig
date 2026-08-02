package dns

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"go-ispconfig/internal/getconf"
)

// domainOf strips the trailing dot of a zone origin ("example.com." ->
// "example.com").
func domainOf(origin string) string { return strings.TrimSuffix(origin, ".") }

// originPattern is the strict charset accepted for zone origins: FQDN
// labels plus "/" (classless in-addr.arpa delegations, RFC 2317) and "_"
// (RFC 2782-style labels). Glob metacharacters (* ? [), quotes, braces
// and anything else shell- or named.conf-relevant are excluded.
var originPattern = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

// checkOrigin validates a zone origin coming from a datalog event or the
// database before it reaches file paths, key-file globs or
// named.conf.local. The daemon does not trust the DB: the API validates
// on write, but adopted/hand-edited rows may hold anything.
func checkOrigin(origin string) error {
	if origin == "" || strings.Contains(origin, "..") || !originPattern.MatchString(origin) {
		return fmt.Errorf("dns: invalid zone origin %q", origin)
	}
	return nil
}

// containedJoin joins parts onto dir and verifies the cleaned result
// stays inside dir (defense in depth behind checkOrigin). On escape it
// returns a non-existent sentinel inside dir so globs match nothing and
// file operations fail cleanly.
func containedJoin(dir string, parts ...string) string {
	p := filepath.Join(append([]string{dir}, parts...)...)
	if d := filepath.Clean(dir); p != d && !strings.HasPrefix(p, d+string(filepath.Separator)) {
		return filepath.Join(d, "_invalid_origin_rejected")
	}
	return p
}

// zoneFileBase converts an origin into its zone file base name: trailing
// dot stripped and "/" replaced by "_" (classless in-addr.arpa delegation
// origins contain slashes).
func zoneFileBase(origin string) string {
	return strings.ReplaceAll(domainOf(origin), "/", "_")
}

// zoneFilePath returns the primary zone file path of an origin, contained
// in bind_zonefiles_dir.
func zoneFilePath(cfg *getconf.DNSConfig, origin string) string {
	return containedJoin(cfg.BindZonefilesDir, cfg.BindZonefilesMasterPfx+zoneFileBase(origin))
}

// slaveZoneFilePath returns the secondary zone file path of an origin,
// contained in bind_zonefiles_dir. The slave prefix may contain a
// subdirectory ("slave/sec.").
func slaveZoneFilePath(cfg *getconf.DNSConfig, origin string) string {
	return containedJoin(cfg.BindZonefilesDir, cfg.BindZonefilesSlavePfx+zoneFileBase(origin))
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
// validation rollback. The previous content is returned even when the
// post-write chown fails — the new content is on disk by then, so the
// caller still needs it for a clean rollback.
func (p *Plugin) writeZoneFile(ctx context.Context, cfg *getconf.DNSConfig, filename, content string) (oldContent string, err error) {
	if prev, readErr := os.ReadFile(filename); readErr == nil {
		oldContent = string(prev)
	}
	if err := os.WriteFile(filename, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("dns: writing zone file %s: %w", filename, err)
	}
	if err := p.chown(ctx, filename, cfg.BindUser, cfg.BindGroup); err != nil {
		return oldContent, err
	}
	p.log.Info("dns: wrote zone file", "file", filename)
	return oldContent, nil
}
