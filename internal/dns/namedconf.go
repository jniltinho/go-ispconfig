package dns

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"go-ispconfig/internal/getconf"
	"go-ispconfig/internal/mastertpl"
)

// Master template names for named.conf.local reconstruction.
const (
	namedConfMasterTemplate = "bind_named.conf.local.master"
	namedConfSlaveTemplate  = "bind_named.conf.local.slave"
)

// aclPattern is the charset accepted for named.conf address-match-list
// values from the DB (xfer/also_notify/update_acl/ns): addresses,
// prefixes, acl names and `!` negation. Quotes, braces and `;` are
// excluded so a DB value can never break out of its `{...;};` list
// (hardening: the API validates, the daemon does not trust the DB).
var aclPattern = regexp.MustCompile(`^[A-Za-z0-9 !.,:/_-]+$`)

// aclList converts a comma-separated DB value into a named.conf address
// list body ("a;b"). ok is false for values that could inject config.
func aclList(v string) (string, bool) {
	if !aclPattern.MatchString(v) {
		return "", false
	}
	return strings.ReplaceAll(v, ",", ";"), true
}

// primaryZoneRows builds the template rows for the active primary zones
// (port of the first write_named_conf loop): zone file path suffixed
// `.signed` when DNSSEC is wanted and the signed file exists (missing
// signed file falls back to the unsigned zone with an error log instead
// of dropping the zone from named.conf), options from xfer/also_notify/
// update_acl with commas replaced by `;`, and only zones whose file
// exists on disk (a zone without records has no file yet). exists is
// injectable for deterministic tests.
func primaryZoneRows(cfg *getconf.DNSConfig, zones []map[string]any, exists func(string) bool, log *slog.Logger) []map[string]any {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	var out []map[string]any
	for _, z := range zones {
		r := row(z)
		if err := checkOrigin(r.str("origin")); err != nil {
			log.Error("dns: skipping zone with invalid origin in named.conf.local", "error", err)
			continue
		}
		zoneFile := zoneFilePath(cfg, r.str("origin"))
		if r.str("dnssec_wanted") == "Y" {
			if exists(zoneFile + ".signed") {
				zoneFile += ".signed"
			} else {
				log.Error("dns: signed zone file missing, serving the unsigned zone until the next re-sign",
					"origin", r.str("origin"), "missing", zoneFile+".signed")
			}
		}
		var options strings.Builder
		for _, o := range []struct{ clause, value string }{
			{"allow-transfer", r.str("xfer")},
			{"also-notify", r.str("also_notify")},
			{"allow-update", r.str("update_acl")},
		} {
			if strings.TrimSpace(o.value) == "" {
				continue
			}
			list, ok := aclList(o.value)
			if !ok {
				log.Error("dns: dropping unsafe named.conf option value",
					"origin", r.str("origin"), "clause", o.clause, "value", o.value)
				continue
			}
			options.WriteString("        " + o.clause + " {" + list + ";};\n")
		}
		if !exists(zoneFile) {
			continue
		}
		out = append(out, map[string]any{
			"zone":          domainOf(r.str("origin")),
			"zonefile_path": zoneFile,
			"options":       options.String(),
		})
	}
	return out
}

// secondaryZoneRows builds the template rows for the active secondary
// zones (port of the second write_named_conf loop): masters from ns,
// allow-transfer from xfer or {none;} when empty. Zones with an invalid
// origin or an unsafe masters value are skipped; an unsafe xfer value
// degrades to {none;}.
func secondaryZoneRows(cfg *getconf.DNSConfig, zones []map[string]any, log *slog.Logger) []map[string]any {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	var out []map[string]any
	for _, z := range zones {
		r := row(z)
		if err := checkOrigin(r.str("origin")); err != nil {
			log.Error("dns: skipping slave zone with invalid origin in named.conf.local", "error", err)
			continue
		}
		masters, ok := aclList(r.str("ns"))
		if !ok {
			log.Error("dns: skipping slave zone with unsafe masters value",
				"origin", r.str("origin"), "ns", r.str("ns"))
			continue
		}
		options := "        masters {" + masters + ";};\n"
		if xfer, ok := aclList(r.str("xfer")); ok && strings.TrimSpace(r.str("xfer")) != "" {
			options += "        allow-transfer {" + xfer + ";};\n"
		} else {
			if strings.TrimSpace(r.str("xfer")) != "" {
				log.Error("dns: dropping unsafe slave allow-transfer value",
					"origin", r.str("origin"), "xfer", r.str("xfer"))
			}
			options += "        allow-transfer {none;};\n"
		}
		out = append(out, map[string]any{
			"zone":          domainOf(r.str("origin")),
			"zonefile_path": slaveZoneFilePath(cfg, r.str("origin")),
			"options":       options,
		})
	}
	return out
}

// renderNamedConf renders the full named.conf.local content: the master
// template over the primary rows, a newline, then the slave template over
// the secondary rows (PHP: grab()."\n".grab()).
func renderNamedConf(primary, secondary []map[string]any, customTplDir string) (string, error) {
	var parts [2]string
	for i, t := range []struct {
		name string
		rows []map[string]any
	}{
		{namedConfMasterTemplate, primary},
		{namedConfSlaveTemplate, secondary},
	} {
		src, _, err := mastertpl.Load(t.name, customTplDir)
		if err != nil {
			return "", err
		}
		tpl := mastertpl.New(src)
		tpl.SetLoop("zones", t.rows)
		parts[i], err = tpl.Render()
		if err != nil {
			return "", fmt.Errorf("dns: rendering %s: %w", t.name, err)
		}
	}
	return parts[0] + "\n" + parts[1], nil
}

// writeNamedConf rebuilds named.conf.local from all active dns_soa and
// dns_slave rows of this server (design D4). The write is atomic (temp
// file + rename in the target directory).
func (p *Plugin) writeNamedConf(ctx context.Context, cfg *getconf.DNSConfig) error {
	var zones []map[string]any
	err := p.db.WithContext(ctx).Table("dns_soa").
		Select("origin, xfer, also_notify, update_acl, dnssec_wanted").
		Where("active = 'Y' AND server_id = ?", p.serverID).
		Order("id").Find(&zones).Error
	if err != nil {
		return fmt.Errorf("dns: loading active zones: %w", err)
	}
	var slaves []map[string]any
	err = p.db.WithContext(ctx).Table("dns_slave").
		Select("origin, xfer, ns").
		Where("active = 'Y' AND server_id = ?", p.serverID).
		Order("id").Find(&slaves).Error
	if err != nil {
		return fmt.Errorf("dns: loading active slave zones: %w", err)
	}

	content, err := renderNamedConf(
		primaryZoneRows(cfg, zones, fileExists, p.log),
		secondaryZoneRows(cfg, slaves, p.log),
		p.customTplDir,
	)
	if err != nil {
		return err
	}
	if err := atomicWrite(cfg.NamedConfLocalPath, content); err != nil {
		return err
	}
	p.log.Info("dns: wrote named.conf.local", "file", cfg.NamedConfLocalPath,
		"zones", len(zones), "slave_zones", len(slaves))
	return nil
}

// fileExists reports whether path exists.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// atomicWrite writes content to path via a temp file + rename in the same
// directory, so named never reads a half-written config.
func atomicWrite(path, content string) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp*")
	if err != nil {
		return fmt.Errorf("dns: creating temp file for %s: %w", path, err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("dns: writing %s: %w", tmp.Name(), err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("dns: chmod %s: %w", tmp.Name(), err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("dns: closing %s: %w", tmp.Name(), err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("dns: replacing %s: %w", path, err)
	}
	return nil
}
