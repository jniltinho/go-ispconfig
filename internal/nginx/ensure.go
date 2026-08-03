package nginx

import (
	"context"

	sitepkg "go-ispconfig/internal/site"

	"go-ispconfig/internal/getconf"
)

// site carries everything one web_domain event handler run needs: the server
// web config, the decoded payload and the parent domain names resolved for
// vhostsubdomain/vhostalias records. It is assembled by the event handlers
// (DB lookups) and consumed by the pure-ish worker functions, keeping those
// testable with fake runners and temp dirs.
type site struct {
	cfg             *getconf.WebConfig
	action          string // "insert" or "update"
	old, new        row
	parentDomain    string // parent web_domain.domain for sub/alias types
	oldParentDomain string
	clientID        int64 // client owning the site (sys_group lookup)
	oldClientID     int64 // previous owner when the site changed clients
	// sslChanged marks that certificate files were replaced in this run
	// (set by the ssl handler) so a failed nginx -t also restores them.
	sslChanged bool
}

// allowedSystemName reports whether name may own a website.
func allowedSystemName(name string) bool { return sitepkg.AllowedSystemName(name) }

// webFolderOf returns the folder below document_root serving the site.
func webFolderOf(d row) string { return sitepkg.WebFolder(sitepkg.Row(d)) }

// symlinkTargets expands the website_symlinks templates for one domain.
func symlinkTargets(cfg *getconf.WebConfig, domain string, clientID int64) []string {
	return sitepkg.SymlinkTargets(cfg, domain, clientID)
}

// ensureSite provisions the site filesystem and system user/group of a
// vhost-type web_domain. The layout itself is shared with the Apache plugin
// (internal/site); nginx only contributes its worker user — which joins the
// client group at security level 20 — and its own log directory root.
func (p *Plugin) ensureSite(ctx context.Context, s site) error {
	if err := safeDomain(s.new.str("domain")); err != nil {
		return err
	}
	return sitepkg.Ensure(ctx, sitepkg.Request{
		Tag:          "nginx",
		WorkerUser:   s.cfg.NginxUser,
		LogBaseDir:   p.logBaseDir,
		Cfg:          s.cfg,
		Action:       s.action,
		Old:          sitepkg.Row(s.old),
		New:          sitepkg.Row(s.new),
		ParentDomain: s.parentDomain,
		ClientID:     s.clientID,
		OldClientID:  s.oldClientID,
		Runner:       p.runner,
		Log:          p.log,
	})
}

// chown changes ownership through the command runner (the daemon runs as
// root in production; tests fake the runner).
func (p *Plugin) chown(ctx context.Context, path, user, group string, recursive bool) error {
	return sitepkg.Chown(ctx, p.runner, path, user, group, recursive)
}
