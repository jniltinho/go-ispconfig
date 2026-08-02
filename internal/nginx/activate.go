package nginx

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/web"
)

// vhostFileName returns the vhost file name of a domain.
func vhostFileName(domain string) string { return domain + ".vhost" }

// copyFile duplicates src to dst with mode 0644.
func copyFile(src, dst string) error {
	return copyFileMode(src, dst, 0o644)
}

// copyFileMode duplicates src to dst with an explicit mode. SSL private keys
// must never be copied at 0644 (world-readable): the ~ backup and .err
// quarantine of a key use 0600.
func copyFileMode(src, dst string, mode os.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, mode)
}

// removeIfLink removes path when it is a symlink.
func removeIfLink(path string) error {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return nil
	}
	return os.Remove(path)
}

// enabledLinks returns the possible sites-enabled symlink paths of a domain
// (plain legacy name plus the 100-/900- ordered names).
func enabledLinks(enabledDir, domain string) (plain, prio100, prio900 string) {
	return filepath.Join(enabledDir, vhostFileName(domain)),
		filepath.Join(enabledDir, "100-"+vhostFileName(domain)),
		filepath.Join(enabledDir, "900-"+vhostFileName(domain))
}

// activateVhost writes the rendered vhost with a backup of the previous
// file, manages the sites-enabled symlink, validates with nginx -t and rolls
// back on failure (broken file quarantined as .err, previous vhost or a
// placeholder restored, SSL backups restored when the certificate changed in
// this run). On success one delayed httpd reload is scheduled (design D3).
func (p *Plugin) activateVhost(ctx context.Context, s site, content string) error {
	cfg, d := s.cfg, s.new
	domain := d.str("domain")
	vhostFile := filepath.Join(cfg.NginxVhostConfDir, vhostFileName(domain))
	backup := vhostFile + "~"

	hadPrevious := false
	if _, err := os.Stat(vhostFile); err == nil {
		if err := copyFile(vhostFile, backup); err != nil {
			return fmt.Errorf("nginx: backing up %s: %w", vhostFile, err)
		}
		hadPrevious = true
	}
	if err := os.WriteFile(vhostFile, []byte(content), 0o644); err != nil {
		return fmt.Errorf("nginx: writing %s: %w", vhostFile, err)
	}
	p.log.Info("nginx: wrote vhost file", "file", vhostFile)

	// Symlink management (port of the enabled-dir block).
	plain, prio100, prio900 := enabledLinks(cfg.NginxVhostConfEnabledDir, domain)
	if err := removeIfLink(plain); err != nil {
		return fmt.Errorf("nginx: removing legacy symlink %s: %w", plain, err)
	}
	if d.str("subdomain") != s.old.str("subdomain") || d.str("active") == "n" {
		for _, l := range []string{prio100, prio900} {
			if err := removeIfLink(l); err != nil {
				return fmt.Errorf("nginx: removing symlink %s: %w", l, err)
			}
		}
	}
	link := prio100
	if d.str("subdomain") == "*" {
		link = prio900
	}
	if d.str("active") == "y" {
		if _, err := os.Lstat(link); os.IsNotExist(err) {
			if err := os.MkdirAll(cfg.NginxVhostConfEnabledDir, 0o755); err != nil {
				return fmt.Errorf("nginx: creating %s: %w", cfg.NginxVhostConfEnabledDir, err)
			}
			if err := os.Symlink(vhostFile, link); err != nil {
				return fmt.Errorf("nginx: creating symlink %s: %w", link, err)
			}
			p.log.Info("nginx: enabled vhost", "link", link)
		}
	}

	// Rename: drop the old domain's vhost file and symlinks.
	if s.action == "update" && s.old.str("domain") != "" && s.old.str("domain") != domain {
		oldPlain, old100, old900 := enabledLinks(cfg.NginxVhostConfEnabledDir, s.old.str("domain"))
		for _, l := range []string{oldPlain, old100, old900} {
			if err := removeIfLink(l); err != nil {
				return fmt.Errorf("nginx: removing old symlink %s: %w", l, err)
			}
		}
		oldFile := filepath.Join(cfg.NginxVhostConfDir, vhostFileName(s.old.str("domain")))
		if err := os.Remove(oldFile); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("nginx: removing old vhost file %s: %w", oldFile, err)
		}
	}

	// Validate before asking for a reload.
	if out, err := p.runner.Run(ctx, "nginx", "-t"); err != nil {
		rollbackErr := p.rollbackVhost(s, vhostFile, backup, hadPrevious)
		if rollbackErr != nil {
			return fmt.Errorf("nginx: config test failed for %s AND rollback failed: %w: %s / rollback: %v",
				domain, err, out, rollbackErr)
		}
		return fmt.Errorf("nginx: config test failed for %s, vhost rolled back (broken config saved as %s.err): %w: %s",
			domain, vhostFile, err, out)
	}

	if p.services != nil {
		p.services.RestartServiceDelayed(web.HttpdService, engine.ActionReload)
	}
	if err := os.Remove(backup); err != nil && !os.IsNotExist(err) {
		p.log.Warn("nginx: could not remove vhost backup", "file", backup, "error", err)
	}
	p.cleanupSSLBackups(s)
	return nil
}

// rollbackVhost quarantines the broken vhost as .err and restores the
// previous file (or a placeholder comment when there was none), plus the SSL
// files backed up in this run when the certificate changed.
func (p *Plugin) rollbackVhost(s site, vhostFile, backup string, hadPrevious bool) error {
	if err := copyFile(vhostFile, vhostFile+".err"); err != nil {
		return fmt.Errorf("saving %s.err: %w", vhostFile, err)
	}
	if hadPrevious {
		if err := copyFile(backup, vhostFile); err != nil {
			return fmt.Errorf("restoring %s: %w", vhostFile, err)
		}
		if err := os.Remove(backup); err != nil && !os.IsNotExist(err) {
			p.log.Warn("nginx: could not remove vhost backup", "file", backup, "error", err)
		}
	} else {
		placeholder := "# nginx did not start after modifying this vhost file.\n" +
			"# Please check file " + vhostFile + ".err for syntax errors.\n"
		if err := os.WriteFile(vhostFile, []byte(placeholder), 0o644); err != nil {
			return fmt.Errorf("writing placeholder %s: %w", vhostFile, err)
		}
	}

	if s.sslChanged {
		for _, f := range sslFilePaths(s.new) {
			// Keys keep 0600 through quarantine and restore.
			mode := os.FileMode(0o644)
			if strings.HasSuffix(f, ".key") {
				mode = 0o600
			}
			if _, err := os.Stat(f); err == nil {
				if err := copyFileMode(f, f+".err", mode); err != nil {
					p.log.Warn("nginx: could not quarantine ssl file", "file", f, "error", err)
				}
			}
			if _, err := os.Stat(f + "~"); err == nil {
				if err := copyFileMode(f+"~", f, mode); err != nil {
					return fmt.Errorf("restoring ssl file %s: %w", f, err)
				}
			}
		}
	}
	return nil
}

// sslFilePaths returns the key/csr/crt files of a domain's ssl dir.
func sslFilePaths(d row) []string {
	sslDomain := d.str("ssl_domain")
	if sslDomain == "" {
		sslDomain = d.str("domain")
	}
	dir := filepath.Join(d.str("document_root"), "ssl")
	return []string{
		filepath.Join(dir, sslDomain+".key"),
		filepath.Join(dir, sslDomain+".csr"),
		filepath.Join(dir, sslDomain+".crt"),
	}
}

// cleanupSSLBackups removes the ~ backups of the SSL files after a
// successful activation.
func (p *Plugin) cleanupSSLBackups(s site) {
	if !s.sslChanged {
		return
	}
	for _, f := range sslFilePaths(s.new) {
		if err := os.Remove(f + "~"); err != nil && !os.IsNotExist(err) {
			p.log.Warn("nginx: could not remove ssl backup", "file", f+"~", "error", err)
		}
	}
}
