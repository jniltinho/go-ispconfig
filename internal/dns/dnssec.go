package dns

// DNSSEC lifecycle (port of the bind_plugin soa_dnssec_* functions,
// design D6). Deviations from PHP, both deliberate:
//   - the entropy_avail check is dropped (kernels >= 5.6 have an
//     always-seeded CSPRNG on every target distro);
//   - dnssec-keygen/dnssec-signzone run with -K/-d <keydir> instead of a
//     shell `cd <keydir>` (the command runner takes argv only); the key
//     and dsset files land in the same directory either way.

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

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
	return filepath.Join(cfg.BindKeyfilesDir, "dsset-"+domain+".")
}

// keyFiles globs the key files of one algorithm for a domain, matching
// the PHP pattern K<domain>.+<algo-id>*<suffix>.
func keyFiles(cfg *getconf.DNSConfig, domain, algo, suffix string) []string {
	pattern := filepath.Join(cfg.BindKeyfilesDir, "K"+domain+".+"+dnssecAlgoIDs[algo]+"*"+suffix)
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil
	}
	return matches
}

// allKeyFiles globs every key file of a domain regardless of algorithm
// (PHP K<domain>*.key).
func allKeyFiles(cfg *getconf.DNSConfig, domain, suffix string) []string {
	matches, err := filepath.Glob(filepath.Join(cfg.BindKeyfilesDir, "K"+domain+"*"+suffix))
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
