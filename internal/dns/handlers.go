package dns

import (
	"context"
	"errors"
	"fmt"
	"os"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/getconf"
)

// OnLoad subscribes the plugin to the dns module events (port of
// bind_plugin.inc.php onLoad).
func (p *Plugin) OnLoad(r *engine.Registry) error {
	subs := []struct {
		event string
		fn    engine.EventFunc
	}{
		{"dns_soa_insert", p.soaInsert},
		{"dns_soa_update", p.soaUpdate},
	}
	for _, s := range subs {
		if err := r.RegisterEvent(s.event, s.fn); err != nil {
			return err
		}
	}
	return nil
}

// dnsConfig loads the [dns] section of this server's config and fills
// empty keys with the server.ini.master defaults, so adopted databases
// without a [dns] section still resolve sane Debian paths.
func (p *Plugin) dnsConfig() (*getconf.DNSConfig, error) {
	cfg, err := getconf.GetServerConfig(p.db, p.serverID)
	if err != nil {
		return nil, fmt.Errorf("dns: loading server %d dns config: %w", p.serverID, err)
	}
	d := cfg.DNS
	defaults := map[*string]string{
		&d.BindUser:               "root",
		&d.BindGroup:              "bind",
		&d.BindZonefilesDir:       "/etc/bind",
		&d.BindKeyfilesDir:        "/etc/bind",
		&d.BindZonefilesMasterPfx: "pri.",
		&d.NamedConfLocalPath:     "/etc/bind/named.conf.local",
	}
	for field, def := range defaults {
		if *field == "" {
			*field = def
		}
	}
	return &d, nil
}

// soaInsert handles dns_soa_insert (PHP parity: same path as update).
func (p *Plugin) soaInsert(ctx context.Context, _ string, data engine.Data) error {
	return p.soaApply(ctx, data)
}

// soaUpdate handles dns_soa_update.
func (p *Plugin) soaUpdate(ctx context.Context, _ string, data engine.Data) error {
	return p.soaApply(ctx, data)
}

// soaApply is the shared insert/update path (port of bind_plugin
// soa_update): render -> write -> validate -> named.conf rebuild ->
// old-origin cleanup -> delayed restart/reload. A zone validation failure
// does not abort the pipeline (the previous zone was restored); it is
// joined into the returned error so the daemon records it on the datalog
// row, exactly like the PHP datalogError call.
func (p *Plugin) soaApply(ctx context.Context, data engine.Data) error {
	cfg, err := p.dnsConfig()
	if err != nil {
		return err
	}
	newRow, oldRow := row(data.New), row(data.Old)
	var errs []error

	if newRow.num("id") != 0 {
		records, err := p.loadActiveRecords(ctx, newRow.num("id"))
		if err != nil {
			return err
		}
		if len(records) == 0 {
			// PHP parity: soa_update returns entirely for a recordless zone
			// (no named.conf rebuild, no reload).
			p.log.Debug("dns: zone has no records yet, skip", "origin", newRow.str("origin"))
			return nil
		}
		content, err := RenderZone(data.New, records, p.bindCAASupported(ctx), p.customTplDir)
		if err != nil {
			return err
		}
		filename := zoneFilePath(cfg, newRow.str("origin"))
		oldContent, err := p.writeZoneFile(ctx, cfg, filename, content)
		if err != nil {
			return err
		}
		if err := p.saveRenderedZone(ctx, newRow.num("id"), content); err != nil {
			errs = append(errs, err)
		}
		if err := p.validateZone(ctx, cfg, newRow.str("origin"), filename, oldContent); err != nil {
			errs = append(errs, err)
		}
	}

	if err := p.writeNamedConf(ctx, cfg); err != nil {
		errs = append(errs, err)
	}

	// Delete the old zone's files when the origin changed.
	if oldRow.str("origin") != "" && oldRow.str("origin") != newRow.str("origin") {
		p.cleanupZoneFiles(zoneFilePath(cfg, oldRow.str("origin")))
	}

	// Restart bind when update_acl is set, otherwise reload (design D5).
	p.services.RestartServiceDelayed(BindService, bindAction(newRow))
	return errors.Join(errs...)
}

// bindAction returns the delayed service action for a SOA row: restart
// when update_acl is non-empty, reload otherwise.
func bindAction(newRow row) string {
	if newRow.str("update_acl") != "" {
		return engine.ActionRestart
	}
	return engine.ActionReload
}

// cleanupZoneFiles removes a zone file plus its .err and .signed
// variants (origin rename / zone delete cleanup).
func (p *Plugin) cleanupZoneFiles(zoneFile string) {
	for _, f := range []string{zoneFile, zoneFile + ".err", zoneFile + ".signed"} {
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
			p.log.Warn("dns: could not remove stale zone file", "file", f, "error", err)
		}
	}
}

// loadActiveRecords returns the active dns_rr rows of a zone in id order
// (deterministic version of the PHP unordered SELECT).
func (p *Plugin) loadActiveRecords(ctx context.Context, zoneID int64) ([]map[string]any, error) {
	var records []map[string]any
	err := p.db.WithContext(ctx).Table("dns_rr").
		Where("zone = ? AND active = 'Y'", zoneID).
		Order("id").Find(&records).Error
	if err != nil {
		return nil, fmt.Errorf("dns: loading records of zone %d: %w", zoneID, err)
	}
	return records, nil
}

// saveRenderedZone caches the rendered zone text in dns_soa.rendered_zone
// for exports and the panel UI.
func (p *Plugin) saveRenderedZone(ctx context.Context, zoneID int64, content string) error {
	err := p.db.WithContext(ctx).
		Exec("UPDATE dns_soa SET rendered_zone = ? WHERE id = ?", content, zoneID).Error
	if err != nil {
		return fmt.Errorf("dns: caching rendered zone %d: %w", zoneID, err)
	}
	return nil
}
