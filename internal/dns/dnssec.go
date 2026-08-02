package dns

// DNSSEC lifecycle (port of the bind_plugin soa_dnssec_* functions,
// design D6). Deviations from PHP, both deliberate:
//   - the entropy_avail check is dropped (kernels >= 5.6 have an
//     always-seeded CSPRNG on every target distro);
//   - dnssec-keygen writes its key files via -K <keydir>;
//     dnssec-signzone runs with its working directory set to the keydir
//     (PHP `cd <keydir>` parity), because signzone always writes the
//     dsset file into the process CWD — its -d flag only sets the SEARCH
//     directory for existing dsset/keyset files, it does not relocate
//     the output.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/getconf"
)

// dnssecAlgoIDs maps the dnssec_algo SET members to the IANA algorithm
// numbers used in the key file names (K<domain>.+013+<tag>.key).
var dnssecAlgoIDs = map[string]string{
	"ECDSAP256SHA256": "013",
	"NSEC3RSASHA1":    "007",
}

// dnssecAlgos parses the dns_soa.dnssec_algo SET column into the known
// algorithm names (unknown members are ignored).
func dnssecAlgos(set string) []string {
	var out []string
	for _, f := range strings.Split(set, ",") {
		if _, ok := dnssecAlgoIDs[strings.TrimSpace(f)]; ok {
			out = append(out, strings.TrimSpace(f))
		}
	}
	return out
}

// dssetPath returns the dsset file dnssec-signzone writes for a domain
// (trailing dot included, PHP parity).
func dssetPath(cfg *getconf.DNSConfig, domain string) string {
	return containedJoin(cfg.BindKeyfilesDir, "dsset-"+domain+".")
}

// keyFiles globs the key files of one algorithm for a domain, matching
// the PHP pattern K<domain>.+<algo-id>*<suffix>.
func keyFiles(cfg *getconf.DNSConfig, domain, algo, suffix string) []string {
	pattern := containedJoin(cfg.BindKeyfilesDir, "K"+domain+".+"+dnssecAlgoIDs[algo]+"*"+suffix)
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil
	}
	return matches
}

// allKeyFiles globs every key file of a domain regardless of algorithm
// (PHP K<domain>*.key).
func allKeyFiles(cfg *getconf.DNSConfig, domain, suffix string) []string {
	matches, err := filepath.Glob(containedJoin(cfg.BindKeyfilesDir, "K"+domain+"*"+suffix))
	if err != nil {
		return nil
	}
	return matches
}

// dnssecCreateKeys generates the ZSK+KSK pair for every configured
// algorithm via dnssec-keygen, guarded against overwriting existing key
// material (glob check per algorithm, PHP parity).
func (p *Plugin) dnssecCreateKeys(ctx context.Context, cfg *getconf.DNSConfig, domain string, algos []string) error {
	for _, algo := range algos {
		if len(keyFiles(cfg, domain, algo, ".key")) > 0 {
			continue // never overwrite existing keys
		}
		var commands [][]string
		switch algo {
		case "ECDSAP256SHA256":
			commands = [][]string{
				{"dnssec-keygen", "-K", cfg.BindKeyfilesDir, "-3", "-a", "ECDSAP256SHA256", "-n", "ZONE", domain},
				{"dnssec-keygen", "-K", cfg.BindKeyfilesDir, "-f", "KSK", "-3", "-a", "ECDSAP256SHA256", "-n", "ZONE", domain},
			}
		case "NSEC3RSASHA1":
			commands = [][]string{
				{"dnssec-keygen", "-K", cfg.BindKeyfilesDir, "-a", "NSEC3RSASHA1", "-b", "2048", "-n", "ZONE", domain},
				{"dnssec-keygen", "-K", cfg.BindKeyfilesDir, "-f", "KSK", "-a", "NSEC3RSASHA1", "-b", "4096", "-n", "ZONE", domain},
			}
		}
		for _, cmd := range commands {
			if out, err := p.runner.Run(ctx, cmd[0], cmd[1:]...); err != nil {
				return fmt.Errorf("dns: %s for %s: %w: %s", cmd[0], domain, err, out)
			}
		}
		p.log.Info("dns: generated dnssec keys", "domain", domain, "algorithm", algo)
	}
	return nil
}

// dnssecCreate ports soa_dnssec_create: guarded key generation followed by
// the first signing. With an unchanged algorithm set, an existing dsset
// file falls through to update/sign and existing keys of an initialized
// zone are reused for signing (never regenerated). A zone without a
// rendered zone file skips DNSSEC entirely.
func (p *Plugin) dnssecCreate(ctx context.Context, cfg *getconf.DNSConfig, oldRow, newRow row) error {
	domain := domainOf(newRow.str("origin"))
	if !fileExists(zoneFilePath(cfg, newRow.str("origin"))) {
		return nil
	}
	if oldRow.str("dnssec_algo") == newRow.str("dnssec_algo") {
		if fileExists(dssetPath(cfg, domain)) {
			return p.dnssecUpdate(ctx, cfg, oldRow, newRow, false)
		}
		if newRow.str("dnssec_initialized") == "Y" && len(allKeyFiles(cfg, domain, ".key")) > 0 {
			// Keys were generated but the dsset file never appeared: sign.
			return p.dnssecSign(ctx, cfg, newRow)
		}
	}
	if err := p.dnssecCreateKeys(ctx, cfg, domain, dnssecAlgos(newRow.str("dnssec_algo"))); err != nil {
		return err
	}
	return p.dnssecSign(ctx, cfg, newRow)
}

// dnssecUpdate ports soa_dnssec_update: create-if-missing-dsset, a
// named-checkzone gate (a broken zone file aborts the re-sign with an
// error log, PHP parity: no datalog error), then re-sign.
func (p *Plugin) dnssecUpdate(ctx context.Context, cfg *getconf.DNSConfig, oldRow, newRow row, isNew bool) error {
	domain := domainOf(newRow.str("origin"))
	zoneFile := zoneFilePath(cfg, newRow.str("origin"))
	if !fileExists(zoneFile) {
		return nil
	}
	if !isNew && !fileExists(dssetPath(cfg, domain)) {
		return p.dnssecCreate(ctx, cfg, oldRow, newRow)
	}
	if out, err := p.runner.Run(ctx, "named-checkzone", domain, zoneFile); err != nil {
		p.log.Error("dns: dnssec re-sign aborted, zone file fails named-checkzone",
			"domain", domain, "error", err, "output", string(out))
		return nil
	}
	return p.dnssecSign(ctx, cfg, newRow)
}

// injectIncludes appends a `$INCLUDE <keyfile>` line for every key file
// not yet referenced by the zone text and returns the new text plus the
// number of key files seen.
func injectIncludes(zone string, keyfiles []string) (string, int) {
	for _, kf := range keyfiles {
		line := "$INCLUDE " + kf
		if !strings.Contains(zone, line) {
			zone += "\n" + line + "\n"
		}
	}
	return zone, len(keyfiles)
}

// dnssecSign ports soa_dnssec_sign: inject the key $INCLUDE lines, sign
// the zone for 16 days (dnssec-signzone -A -e +1382400 -3 - -N increment)
// and publish the DS + DNSKEY records into dns_soa.dnssec_info together
// with dnssec_initialized/dnssec_last_signed.
func (p *Plugin) dnssecSign(ctx context.Context, cfg *getconf.DNSConfig, newRow row) error {
	domain := domainOf(newRow.str("origin"))
	zoneFile := zoneFilePath(cfg, newRow.str("origin"))
	if !fileExists(zoneFile) {
		return nil
	}
	algos := dnssecAlgos(newRow.str("dnssec_algo"))

	content, err := os.ReadFile(zoneFile)
	if err != nil {
		return fmt.Errorf("dns: reading zone file %s: %w", zoneFile, err)
	}
	zone := string(content)
	keycount := 0
	for _, algo := range algos {
		var n int
		zone, n = injectIncludes(zone, keyFiles(cfg, domain, algo, ".key"))
		keycount += n
	}
	if want := len(algos) * 2; keycount != want {
		p.log.Warn("dns: unexpected dnssec key file count",
			"domain", domain, "found", keycount, "expected", want)
	}
	if err := os.WriteFile(zoneFile, []byte(zone), 0o644); err != nil {
		return fmt.Errorf("dns: writing zone file %s: %w", zoneFile, err)
	}

	// signzone runs with the keydir as working directory (PHP `cd
	// <keydir>` parity): the dsset file is always written into the
	// process CWD — -d would only change where existing dsset/keyset
	// files are searched, not where the new one lands.
	out, err := p.runInDir(ctx, cfg.BindKeyfilesDir, "dnssec-signzone",
		"-A", "-e", "+1382400", "-3", "-", "-N", "increment",
		"-o", domain, "-K", cfg.BindKeyfilesDir,
		"-t", zoneFile)
	if err != nil {
		return fmt.Errorf("dns: dnssec-signzone for %s: %w: %s", domain, err, out)
	}

	info, err := buildDNSSECInfo(cfg, domain, algos)
	if err != nil {
		return err
	}
	err = p.db.WithContext(ctx).Exec(
		"UPDATE dns_soa SET dnssec_info = ?, dnssec_initialized = 'Y', dnssec_last_signed = ? WHERE id = ?",
		info, time.Now().Unix(), newRow.num("id")).Error
	if err != nil {
		return fmt.Errorf("dns: storing dnssec info for %s: %w", domain, err)
	}
	p.log.Info("dns: zone signed", "domain", domain)
	return nil
}

// dnssecLifecycle ports the DNSSEC decision tree of soa_update (design
// D6): origin changed -> delete the old material then create when wanted;
// algorithm changed or wanted freshly enabled -> create; wanted turned
// off -> remove the signed file (keys stay until zone delete);
// steady-state wanted -> update (re-sign).
func (p *Plugin) dnssecLifecycle(ctx context.Context, cfg *getconf.DNSConfig, data engine.Data) error {
	oldRow, newRow := row(data.Old), row(data.New)
	switch {
	case oldRow.str("origin") != newRow.str("origin"):
		var errs []error
		if oldRow.str("dnssec_initialized") == "Y" && len(oldRow.str("origin")) > 3 {
			// Deviation from PHP (spec-mandated): the old origin's material
			// is deleted; upstream resolved the domain from new.origin and
			// leaked the old keys on a rename.
			errs = append(errs, p.dnssecDelete(ctx, cfg, oldRow.str("origin"), newRow.num("id"), true))
		}
		if newRow.str("dnssec_wanted") == "Y" {
			errs = append(errs, p.dnssecCreate(ctx, cfg, oldRow, newRow))
		}
		return errors.Join(errs...)
	case oldRow.str("dnssec_algo") != newRow.str("dnssec_algo"):
		p.log.Debug("dns: dnssec algorithm changed", "algorithm", newRow.str("dnssec_algo"))
		if newRow.str("dnssec_wanted") == "Y" {
			return p.dnssecCreate(ctx, cfg, oldRow, newRow)
		}
	case newRow.str("dnssec_wanted") == "Y" && oldRow.str("dnssec_initialized") == "N":
		return p.dnssecCreate(ctx, cfg, oldRow, newRow)
	case newRow.str("dnssec_wanted") == "N" && oldRow.str("dnssec_initialized") == "Y":
		signed := zoneFilePath(cfg, oldRow.str("origin")) + ".signed"
		if err := os.Remove(signed); err != nil && !os.IsNotExist(err) {
			p.log.Warn("dns: could not remove signed zone file", "file", signed, "error", err)
		}
	case newRow.str("dnssec_wanted") == "Y":
		return p.dnssecUpdate(ctx, cfg, oldRow, newRow, false)
	}
	return nil
}

// dnssecDelete removes all key material, the signed zone file and the
// dsset file of a domain and optionally resets the DB flags (port of
// soa_dnssec_delete; the origin is an explicit parameter, see
// dnssecLifecycle).
func (p *Plugin) dnssecDelete(ctx context.Context, cfg *getconf.DNSConfig, origin string, zoneID int64, sqlUpdate bool) error {
	if origin == "" {
		p.log.Warn("dns: dnssec delete without a domain")
		return nil
	}
	// Defense in depth on top of the handler-entry checks: a glob
	// metacharacter in the domain would widen K<domain>* to other
	// zones' key material.
	if err := checkOrigin(origin); err != nil {
		return err
	}
	domain := domainOf(origin)
	files, _ := filepath.Glob(filepath.Join(cfg.BindKeyfilesDir, "K"+domain+".+*"))
	files = append(files, zoneFilePath(cfg, origin)+".signed", dssetPath(cfg, domain))
	for _, f := range files {
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
			p.log.Warn("dns: could not remove dnssec file", "file", f, "error", err)
		}
	}
	if sqlUpdate && zoneID != 0 {
		err := p.db.WithContext(ctx).Exec(
			"UPDATE dns_soa SET dnssec_info = '', dnssec_initialized = 'N' WHERE id = ?", zoneID).Error
		if err != nil {
			return fmt.Errorf("dns: resetting dnssec state of zone %d: %w", zoneID, err)
		}
	}
	return nil
}

// runInDir runs a command in a working directory via the runner's
// DirRunner extension (ExecRunner and the test fakes implement it).
func (p *Plugin) runInDir(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	dr, ok := p.runner.(engine.DirRunner)
	if !ok {
		return nil, fmt.Errorf("dns: runner %T does not support a working directory", p.runner)
	}
	return dr.RunInDir(ctx, dir, name, args...)
}

// DefaultResignThreshold is the signature age after which the dns_resign
// job re-signs a zone — well inside the 16-day validity of
// dnssec-signzone -e +1382400. The [dns] dnssec_resign_days server
// config key overrides it.
const DefaultResignThreshold = 5 * 24 * time.Hour

// RegisterResign adds the daily dns_resign scheduler job (design D6,
// replacing ISPConfig's cron-driven re-sign): every zone of this server
// with dnssec_wanted='Y', dnssec_initialized='Y' and a signature older
// than the threshold is re-signed, followed by one delayed bind reload.
func (p *Plugin) RegisterResign(sched *engine.Scheduler) error {
	return sched.Register("dns_resign", "@daily", p.resignJob)
}

// resignJob is the dns_resign job body.
func (p *Plugin) resignJob(ctx context.Context) error {
	cfg, err := p.dnsConfig()
	if err != nil {
		return err
	}
	threshold := p.resignThreshold
	if threshold == 0 {
		threshold = DefaultResignThreshold
		if days, err := strconv.Atoi(strings.TrimSpace(cfg.DNSSECResignDays)); err == nil && days > 0 {
			threshold = time.Duration(days) * 24 * time.Hour
		}
	}
	cutoff := time.Now().Add(-threshold).Unix()
	var zones []map[string]any
	err = p.db.WithContext(ctx).Table("dns_soa").
		Where("server_id = ? AND active = 'Y' AND dnssec_wanted = 'Y' AND dnssec_initialized = 'Y' AND dnssec_last_signed < ?",
			p.serverID, cutoff).
		Order("id").Find(&zones).Error
	if err != nil {
		return fmt.Errorf("dns: loading zones for re-signing: %w", err)
	}
	if len(zones) == 0 {
		return nil
	}
	var errs []error
	for _, z := range zones {
		p.log.Info("dns: re-signing stale dnssec zone", "origin", row(z).str("origin"))
		if err := p.dnssecUpdate(ctx, cfg, z, z, false); err != nil {
			errs = append(errs, err)
		}
	}
	p.services.RestartServiceDelayed(BindService, engine.ActionReload)
	return errors.Join(errs...)
}

// buildDNSSECInfo assembles the dnssec_info text block: the DS records
// (dsset file content) followed by the DNSKEY records (.key file
// contents), byte-compatible with the PHP concatenation.
func buildDNSSECInfo(cfg *getconf.DNSConfig, domain string, algos []string) (string, error) {
	ds, err := os.ReadFile(dssetPath(cfg, domain))
	if err != nil {
		return "", fmt.Errorf("dns: reading dsset for %s: %w", domain, err)
	}
	info := "DS-Records:\n" + string(ds) +
		"\n------------------------------------\n\nDNSKEY-Records:\n"
	for _, algo := range algos {
		for _, kf := range keyFiles(cfg, domain, algo, ".key") {
			key, err := os.ReadFile(kf)
			if err != nil {
				return "", fmt.Errorf("dns: reading key file %s: %w", kf, err)
			}
			info += string(key) + "\n\n"
		}
	}
	return info, nil
}
