package mail

import (
	"context"
	"strings"
	"time"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/getconf"
)

// safeMailPath ports the mail_plugin delete guards: the path must live
// under homedir (strictly longer), be at least 10 chars, and contain no
// '//', '..', '*' or '&'. Everything else is a possible security
// violation and must never be deleted.
func safeMailPath(path, homedir string) bool {
	return path != homedir &&
		len(path) > len(homedir) &&
		strings.HasPrefix(path, homedir+"/") &&
		len(path) >= 10 &&
		!strings.Contains(path, "//") &&
		!strings.Contains(path, "..") &&
		!strings.Contains(path, "*") &&
		!strings.Contains(path, "&")
}

// softDeleteEnabled interprets mailbox_soft_delete: PHP's checkbox 'y'
// or this port's numeric retention days both enable it; ”, '0' and 'n'
// disable.
func softDeleteEnabled(cfg getconf.MailConfig) bool {
	switch cfg.MailboxSoftDelete {
	case "", "0", "n":
		return false
	default:
		return true
	}
}

// softDeleteSuffix stamps the rename (PHP date("YmdHis")).
func softDeleteSuffix() string { return time.Now().Format("20060102150405") }

// removeTree deletes or soft-renames one guarded path. Returns whether
// anything was done.
func (p *Plugin) removeTree(ctx context.Context, cfg getconf.MailConfig, path string) bool {
	if !safeMailPath(path, cfg.HomedirPath) {
		p.log.Error("mail: possible security violation when deleting, path refused", "path", path)
		return false
	}
	if softDeleteEnabled(cfg) && isDir(path) {
		trash := path + "-deleted-" + softDeleteSuffix()
		if _, err := p.runner.Run(ctx, "mv", path, trash); err != nil {
			p.log.Error("mail: soft-delete rename failed", "path", path, "error", err)
			return false
		}
		// Fresh mtime so the purge job can filter on age.
		if _, err := p.runner.Run(ctx, "touch", trash); err != nil {
			p.log.Warn("mail: touch after soft-delete failed", "path", trash, "error", err)
		}
		p.log.Debug("mail: soft-deleted", "from", path, "to", trash)
		return true
	}
	if _, err := p.runner.Run(ctx, "rm", "-rf", path); err != nil {
		p.log.Error("mail: delete failed", "path", path, "error", err)
		return false
	}
	p.log.Debug("mail: deleted", "path", path)
	return true
}

// userDelete removes the mailbox tree (port of mail_plugin::user_delete;
// mail backups are a non-goal of this change).
func (p *Plugin) userDelete(ctx context.Context, data engine.Data) error {
	cfg, err := p.config(ctx)
	if err != nil {
		p.log.Warn("mail: using default [mail] config", "error", err)
	}
	p.removeTree(ctx, cfg, row(data.Old).str("maildir"))
	return nil
}

// domainDelete removes homedir_path/<domain> and the mailfilters tree
// (port of mail_plugin::domain_delete). The mailfilter path is always a
// hard delete in PHP — soft delete only applies to the mailbox tree.
func (p *Plugin) domainDelete(ctx context.Context, data engine.Data) error {
	cfg, err := p.config(ctx)
	if err != nil {
		p.log.Warn("mail: using default [mail] config", "error", err)
	}
	domain := row(data.Old).str("domain")
	if domain == "" {
		p.log.Error("mail: domain delete without a domain name, refused")
		return nil
	}
	p.removeTree(ctx, cfg, cfg.HomedirPath+"/"+domain)

	filterPath := cfg.HomedirPath + "/mailfilters/" + domain
	if safeMailPath(filterPath, cfg.HomedirPath+"/mailfilters") && strings.HasPrefix(filterPath, cfg.HomedirPath) {
		if _, err := p.runner.Run(ctx, "rm", "-rf", filterPath); err != nil {
			p.log.Error("mail: mailfilter delete failed", "path", filterPath, "error", err)
		}
	} else {
		p.log.Error("mail: possible security violation when deleting the mailfilter directory", "path", filterPath)
	}
	return nil
}
