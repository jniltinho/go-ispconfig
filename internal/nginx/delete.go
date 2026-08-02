package nginx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gorm.io/gorm"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/getconf"
	"go-ispconfig/internal/web"
)

// webDomainDelete handles web_domain_delete (port of delete()): alias and
// subdomain records re-render their parent vhost; vhost-type records are
// torn down completely.
func (p *Plugin) webDomainDelete(ctx context.Context, _ string, data engine.Data) error {
	old := row(data.Old)
	if !isVhostType(old.str("type")) && old.num("parent_domain_id") > 0 {
		return p.applyParent(ctx, old.num("parent_domain_id"))
	}

	cfg, err := p.webConfig(uint32(old.num("server_id")))
	if err != nil {
		return err
	}
	usedFolders, err := p.siblingWebFolders(old)
	if err != nil {
		return err
	}
	clientID := p.clientIDOf(old.num("sys_groupid"))
	return p.deleteSite(ctx, cfg, old, usedFolders, clientID)
}

// siblingWebFolders returns the web_folder values of other vhostsubdomain/
// vhostalias records sharing the parent domain (they keep their paths alive
// during deletion).
func (p *Plugin) siblingWebFolders(old row) ([]string, error) {
	if old.str("type") == "vhost" {
		return nil, nil
	}
	var folders []string
	err := p.db.Table("web_domain").
		Where("(type = 'vhostsubdomain' OR type = 'vhostalias') AND parent_domain_id = ? AND domain_id != ?",
			old.num("parent_domain_id"), old.num("domain_id")).
		Pluck("web_folder", &folders).Error
	if err != nil {
		return nil, fmt.Errorf("nginx: loading sibling web folders: %w", err)
	}
	return folders, nil
}

// multiSlashRe collapses repeated slashes (PHP folder normalization).
var multiSlashRe = regexp.MustCompile(`/{2,}`)

// normalizeFolder normalizes a web_folder for comparison.
func normalizeFolder(f string) string {
	return trimSlashes(multiSlashRe.ReplaceAllString(f, "/"))
}

// subdomainFolderToDelete ports the vhostsubdomain deletion folder checks:
// given the deleted record's web_folder and the folders still used by
// sibling records, it returns the deepest unused sub-path that may be
// removed ("" and false when nothing may be deleted — paths under web/ and
// still-used paths are never removed).
func subdomainFolderToDelete(webFolder string, usedFolders []string) (string, bool) {
	folder := normalizeFolder(webFolder)
	parts := strings.Split(folder, "/")
	if parts[0] == "web" || parts[0] == "" {
		return "", false
	}

	used := map[string]bool{}
	for _, u := range usedFolders {
		u = normalizeFolder(u)
		for u != "" {
			used[u] = true
			if i := strings.LastIndex(u, "/"); i >= 0 {
				u = u[:i]
			} else {
				u = ""
			}
		}
	}

	deleteFolder, ok := "", false
	for len(parts) > 0 {
		candidate := strings.Join(parts, "/")
		if used[candidate] {
			break
		}
		deleteFolder, ok = candidate, true
		parts = parts[:len(parts)-1]
	}
	return deleteFolder, ok && deleteFolder != ""
}

// deleteSite removes everything belonging to one vhost-type web_domain:
// enabled symlinks, vhost file, PHP-FPM pool file, site directories (with
// the D4 sanity guards), website symlinks, log directory and — for plain
// vhosts — the system user. A delayed httpd reload is scheduled.
func (p *Plugin) deleteSite(ctx context.Context, cfg *getconf.WebConfig, old row, usedFolders []string, clientID int64) error {
	domain := old.str("domain")
	if err := safeDomain(domain); err != nil {
		return err
	}

	// Vhost file and symlinks.
	plain, prio100, prio900 := enabledLinks(cfg.NginxVhostConfEnabledDir, domain)
	for _, l := range []string{plain, prio100, prio900} {
		if err := removeIfLink(l); err != nil {
			return fmt.Errorf("nginx: removing symlink %s: %w", l, err)
		}
	}
	vhostFile := filepath.Join(cfg.NginxVhostConfDir, vhostFileName(domain))
	for _, f := range []string{vhostFile, vhostFile + ".err", vhostFile + "~"} {
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("nginx: removing vhost file %s: %w", f, err)
		}
	}
	p.log.Info("nginx: removed vhost", "file", vhostFile)

	// PHP-FPM pool file of the site (port of php_fpm_pool_delete): remove the
	// pool from its version's dir and prune stale copies across all versions.
	if old.str("php") == "php-fpm" || old.str("php") == "fast-cgi" {
		if err := p.poolDelete(ctx, cfg, old); err != nil {
			return err
		}
	}

	// Site directories, guarded (never outside website_basedir, never the
	// basedir itself, never paths under web/ for sub/alias records).
	docroot := old.str("document_root")
	if docroot != "" && !strings.Contains(docroot, "..") {
		if err := safeSitePath(docroot, cfg.WebsiteBasedir); err != nil {
			return fmt.Errorf("nginx: refusing to delete site directories: %w", err)
		}
		switch old.str("type") {
		case "vhost":
			if err := os.RemoveAll(docroot); err != nil {
				return fmt.Errorf("nginx: removing %s: %w", docroot, err)
			}
			p.log.Info("nginx: removed website directory", "docroot", docroot)
		default: // vhostsubdomain / vhostalias
			if !strings.Contains(old.str("web_folder"), "..") {
				if folder, ok := subdomainFolderToDelete(old.str("web_folder"), usedFolders); ok {
					target := filepath.Join(docroot, folder)
					if err := safeSitePath(target, cfg.WebsiteBasedir); err == nil {
						if err := os.RemoveAll(target); err != nil {
							return fmt.Errorf("nginx: removing %s: %w", target, err)
						}
						p.log.Info("nginx: removed web folder", "path", target)
					}
				}
			}
		}
	} else if docroot != "" {
		return fmt.Errorf("nginx: refusing to delete unsafe document_root %q", docroot)
	}

	// Website symlinks of the site.
	for _, link := range symlinkTargets(cfg, domain, clientID) {
		if err := removeIfLink(link); err != nil {
			return fmt.Errorf("nginx: removing symlink %s: %w", link, err)
		}
	}

	// Log directory.
	if !strings.Contains(domain, "..") && !strings.Contains(domain, "/") {
		logDir := filepath.Join(p.logBaseDir, domain)
		if err := os.RemoveAll(logDir); err != nil {
			return fmt.Errorf("nginx: removing log dir %s: %w", logDir, err)
		}
	}

	// System user (plain vhosts own their user; the group is shared per
	// client and removed by client_delete).
	if old.str("type") == "vhost" {
		if user := old.str("system_user"); allowedSystemName(user) {
			if _, err := p.runner.Run(ctx, "getent", "passwd", user); err == nil {
				if out, err := p.runner.Run(ctx, "userdel", user); err != nil {
					return fmt.Errorf("nginx: userdel %s: %w: %s", user, err, out)
				}
				p.log.Info("nginx: removed user", "user", user)
			}
		}
	}

	if p.services != nil {
		p.services.RestartServiceDelayed(web.HttpdService, engine.ActionReload)
	}
	return nil
}

// loadServerPHP fetches one server_php row (nil when id is 0 or the row is
// gone — a deleted pinned version falls back to the server default). A real
// DB error (connection, etc.) is returned so the caller never silently
// renders the wrong pool/socket for a live database.
func (p *Plugin) loadServerPHP(id int64) (row, error) {
	if id == 0 || p.db == nil {
		return nil, nil
	}
	var rec map[string]any
	err := p.db.Table("server_php").Where("server_php_id = ?", id).Take(&rec).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("nginx: loading server_php %d: %w", id, err)
	}
	return rec, nil
}

// clientDelete tears down everything of a deleted client (web-module-events
// spec): every site owned by the client's groups is removed as in site
// deletion, then the client directory and system group are dropped. The
// event is emitted by the future client module.
func (p *Plugin) clientDelete(ctx context.Context, _ string, data engine.Data) error {
	old := row(data.Old)
	clientID := old.num("client_id")
	if clientID <= 0 {
		return nil
	}

	var groupID int64
	_ = p.db.Table("sys_group").Where("client_id = ?", clientID).
		Pluck("groupid", &groupID).Error

	var sites []map[string]any
	if groupID > 0 {
		err := p.db.Table("web_domain").
			Where("sys_groupid = ? AND type IN ('vhost','vhostsubdomain','vhostalias')", groupID).
			Order("type DESC"). // sub/alias before their parent vhosts
			Find(&sites).Error
		if err != nil {
			return fmt.Errorf("nginx: loading sites of client %d: %w", clientID, err)
		}
	}
	for _, sr := range sites {
		sr := row(sr)
		cfg, err := p.webConfig(uint32(sr.num("server_id")))
		if err != nil {
			return err
		}
		usedFolders, err := p.siblingWebFolders(sr)
		if err != nil {
			return err
		}
		if err := p.deleteSite(ctx, cfg, sr, usedFolders, clientID); err != nil {
			return err
		}
	}

	// Client directory: remove leftover symlinks, then the dir when empty.
	cfg, err := getconf.GetServerConfig(p.db, uint32(old.num("server_id")))
	if err == nil {
		clientDir := filepath.Join(cfg.Web.WebsiteBasedir, "clients", fmt.Sprintf("client%d", clientID))
		if entries, err := os.ReadDir(clientDir); err == nil {
			for _, e := range entries {
				_ = removeIfLink(filepath.Join(clientDir, e.Name()))
			}
			_ = os.Remove(clientDir) // only removed when empty, like @rmdir
		}
	}

	group := fmt.Sprintf("client%d", clientID)
	if _, err := p.runner.Run(ctx, "getent", "group", group); err == nil {
		if out, err := p.runner.Run(ctx, "groupdel", group); err != nil {
			return fmt.Errorf("nginx: groupdel %s: %w: %s", group, err, out)
		}
		p.log.Info("nginx: removed group", "group", group)
	}
	return nil
}
