package nginx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"regexp"
)

// applyVhost renders, merges and activates the vhost of one vhost-type
// web_domain (design D3 pipeline: build → render → merge/blacklist → write
// with backup → nginx -t → symlink → delayed reload). Blacklist rejections
// are non-fatal: the vhost goes live without the offending lines and the
// joined warnings become the datalog error of the row.
func (p *Plugin) applyVhost(ctx context.Context, s site) error {
	in, err := p.loadVhostInput(ctx, s)
	if err != nil {
		return err
	}
	content, warnings, err := renderVhost(in, p.customTplDir)
	if err != nil {
		return err
	}
	merged, mergeWarnings := mergeLocations(content)
	warnings = append(warnings, mergeWarnings...)

	if err := p.activateVhost(ctx, s, merged+"\n"); err != nil {
		return errors.Join(append(warnings, err)...)
	}
	return errors.Join(warnings...)
}

// loadVhostInput resolves the DB-backed parts of the vhost vector: alias
// rows, protected folders, the pinned server_php row and the on-disk SSL
// state.
func (p *Plugin) loadVhostInput(ctx context.Context, s site) (vhostInput, error) {
	d := s.new
	in := vhostInput{
		cfg:          s.cfg,
		d:            d,
		nginxVersion: p.probeNginxVersion(ctx),
		dummyFile:    p.randomDummyFile(),
	}

	var aliases []map[string]any
	err := p.db.Table("web_domain").
		Where("parent_domain_id = ? AND active = 'y' AND type != 'vhostsubdomain' AND type != 'vhostalias'",
			d.num("domain_id")).
		Find(&aliases).Error
	if err != nil {
		return in, fmt.Errorf("nginx: loading alias domains of %s: %w", d.str("domain"), err)
	}
	for _, a := range aliases {
		in.aliases = append(in.aliases, row(a))
	}

	var folders []map[string]any
	err = p.db.Table("web_folder").
		Where("active = 'y' AND parent_domain_id = ?", d.num("domain_id")).
		Find(&folders).Error
	if err != nil {
		return in, fmt.Errorf("nginx: loading web folders of %s: %w", d.str("domain"), err)
	}
	for _, f := range folders {
		in.folders = append(in.folders, row(f))
	}

	if d.num("server_php_id") != 0 && (d.str("php") == "php-fpm" || d.str("php") == "fast-cgi") {
		php, err := p.loadServerPHP(d.num("server_php_id"))
		if err != nil {
			return in, err
		}
		in.serverPHP = php
	}

	// SSL is enabled only when both cert files exist non-empty (PHP parity).
	paths := sslFilePaths(d)
	key, crt := paths[0], paths[2]
	if ki, err := os.Stat(key); err == nil && ki.Size() > 0 {
		if ci, err := os.Stat(crt); err == nil && ci.Size() > 0 {
			in.sslFilesExist = true
		}
	}
	// ponytail: urlIsLocal stays the inline host comparison of the builder;
	// the full DB scan of url_is_local() only refines wildcard-subdomain
	// proxy targets — add it if a migrated site ever needs it.
	return in, nil
}

// nginxVersionRe extracts the version from `nginx -v` output
// ("nginx version: nginx/1.24.0 (Ubuntu)").
var nginxVersionRe = regexp.MustCompile(`nginx/(\d+(?:\.\d+)*)`)

// probeNginxVersion returns the running nginx version, probed once per
// plugin lifetime (tests preset nginxVersion).
func (p *Plugin) probeNginxVersion(ctx context.Context) string {
	if p.nginxVersion != "" {
		return p.nginxVersion
	}
	out, err := p.runner.Run(ctx, "nginx", "-v")
	if err != nil {
		p.log.Warn("nginx: version probe failed", "error", err)
		return ""
	}
	if m := nginxVersionRe.FindSubmatch(out); m != nil {
		p.nginxVersion = string(m[1])
	}
	return p.nginxVersion
}

// randomDummyFile returns the per-render random "/<hex>.htm" try_files dummy
// (PHP parity: an unguessable name so `location ~ \.php$` never serves a
// real file).
func (p *Plugin) randomDummyFile() string {
	if p.dummyFile != "" {
		return p.dummyFile
	}
	var b [16]byte
	_, _ = rand.Read(b[:])
	return "/" + hex.EncodeToString(b[:]) + ".htm"
}
