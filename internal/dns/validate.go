package dns

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"go-ispconfig/internal/getconf"
)

// validateZone runs `named-checkzone <origin> <file>` after a zone write
// (port of the bind_plugin soa_update check block). A stale `<file>.err`
// is removed before the run. On failure the bad render is quarantined as
// `<file>.err`, the previous zone content (when one existed) is restored
// with bind ownership, the checker output is logged (WARN, or DEBUG when
// disable_bind_log = 'y') and returned as an error so the daemon records
// it via the datalog error mechanism.
func (p *Plugin) validateZone(ctx context.Context, cfg *getconf.DNSConfig, origin, filename, oldContent string) error {
	errFile := filename + ".err"
	if err := os.Remove(errFile); err != nil && !os.IsNotExist(err) {
		p.log.Warn("dns: could not remove stale quarantine file", "file", errFile, "error", err)
	}

	out, err := p.runner.Run(ctx, "named-checkzone", origin, filename)
	if err == nil {
		p.log.Debug("dns: zone file valid", "file", filename)
		return nil
	}

	level := slog.LevelWarn
	if cfg.DisableBindLog == "y" {
		level = slog.LevelDebug
	}
	p.log.Log(ctx, level, "dns: zone check failed, restoring previous zone file",
		"file", filename, "origin", origin, "output", string(out))

	if renameErr := os.Rename(filename, errFile); renameErr != nil {
		p.log.Error("dns: could not quarantine invalid zone file", "file", filename, "error", renameErr)
	}
	if oldContent != "" {
		if writeErr := os.WriteFile(filename, []byte(oldContent), 0o644); writeErr != nil {
			p.log.Error("dns: could not restore previous zone file", "file", filename, "error", writeErr)
		} else if chownErr := p.chown(ctx, filename, cfg.BindUser, cfg.BindGroup); chownErr != nil {
			p.log.Error("dns: could not chown restored zone file", "file", filename, "error", chownErr)
		}
	}
	return fmt.Errorf("dns: named-checkzone failed for %s: %w: %s", origin, err, out)
}
