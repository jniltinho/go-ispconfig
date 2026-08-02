package dns

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/getconf"
)

// soaDelete handles dns_soa_delete (port of bind_plugin soa_delete):
// named.conf rebuild, DNSSEC material cleanup, zone file (+ .err/.signed)
// removal, delayed reload.
func (p *Plugin) soaDelete(ctx context.Context, _ string, data engine.Data) error {
	cfg, err := p.dnsConfig()
	if err != nil {
		return err
	}
	var errs []error
	if err := p.writeNamedConf(ctx, cfg); err != nil {
		errs = append(errs, err)
	}
	// DNSSEC materials (keys, .signed, dsset) go first (PHP parity:
	// soa_dnssec_delete with sql_update=false), then the zone files.
	origin := row(data.New).str("origin")
	if origin == "" {
		origin = row(data.Old).str("origin")
	}
	if origin != "" {
		// Reject malicious origins before they reach globs/paths; the
		// named.conf rebuild above already happened, so the stale entry
		// is gone either way.
		if err := checkOrigin(origin); err != nil {
			errs = append(errs, err)
		} else {
			if err := p.dnssecDelete(ctx, cfg, origin, 0, false); err != nil {
				errs = append(errs, err)
			}
			p.cleanupZoneFiles(zoneFilePath(cfg, origin))
		}
	}
	p.services.RestartServiceDelayed(BindService, engine.ActionReload)
	return errors.Join(errs...)
}

// slaveInsert handles dns_slave_insert (PHP parity: same path as update).
func (p *Plugin) slaveInsert(ctx context.Context, _ string, data engine.Data) error {
	return p.slaveApply(ctx, data)
}

// slaveUpdate handles dns_slave_update.
func (p *Plugin) slaveUpdate(ctx context.Context, _ string, data engine.Data) error {
	return p.slaveApply(ctx, data)
}

// slaveApply is the shared insert/update path (port of bind_plugin
// slave_update): named.conf rebuild, slave directory ownership, delayed
// reload. The PHP old-origin cleanup block is a no-op upstream (it unsets
// the variable instead of unlinking the file) and is deliberately not
// ported; named ignores stale transferred files not referenced by
// named.conf.local.
func (p *Plugin) slaveApply(ctx context.Context, _ engine.Data) error {
	cfg, err := p.dnsConfig()
	if err != nil {
		return err
	}
	var errs []error
	if err := p.writeNamedConf(ctx, cfg); err != nil {
		errs = append(errs, err)
	}
	if err := p.ensureSlaveDir(ctx, cfg); err != nil {
		errs = append(errs, err)
	}
	p.services.RestartServiceDelayed(BindService, engine.ActionReload)
	return errors.Join(errs...)
}

// slaveDelete handles dns_slave_delete: named.conf rebuild, transferred
// zone file removal, delayed reload.
func (p *Plugin) slaveDelete(ctx context.Context, _ string, data engine.Data) error {
	cfg, err := p.dnsConfig()
	if err != nil {
		return err
	}
	var errs []error
	if err := p.writeNamedConf(ctx, cfg); err != nil {
		errs = append(errs, err)
	}
	if origin := row(data.Old).str("origin"); origin != "" {
		if err := checkOrigin(origin); err != nil {
			errs = append(errs, err)
		} else {
			file := slaveZoneFilePath(cfg, origin)
			if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
				p.log.Warn("dns: could not remove slave zone file", "file", file, "error", err)
			}
		}
	}
	p.services.RestartServiceDelayed(BindService, engine.ActionReload)
	return errors.Join(errs...)
}

// ensureSlaveDir makes sure the slave zonefile directory exists with mode
// 0770 owned by the bind user/group, so named can write transferred zones
// (port of the slave_update directory block: the directory derives from
// the slave prefix — its dirname unless it ends in a slash — or the
// zonefiles dir when the prefix is empty).
func (p *Plugin) ensureSlaveDir(ctx context.Context, cfg *getconf.DNSConfig) error {
	dir := cfg.BindZonefilesDir
	if cfg.BindZonefilesSlavePfx != "" {
		dir = filepath.Join(cfg.BindZonefilesDir, cfg.BindZonefilesSlavePfx)
		if !strings.HasSuffix(cfg.BindZonefilesSlavePfx, "/") {
			dir = filepath.Dir(dir)
		}
	}
	if err := os.MkdirAll(dir, 0o770); err != nil {
		return fmt.Errorf("dns: creating slave zone dir %s: %w", dir, err)
	}
	if err := os.Chmod(dir, 0o770); err != nil {
		return fmt.Errorf("dns: chmod slave zone dir %s: %w", dir, err)
	}
	return p.chown(ctx, dir, cfg.BindUser, cfg.BindGroup)
}
