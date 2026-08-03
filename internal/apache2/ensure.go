package apache2

import (
	"context"

	"go-ispconfig/internal/getconf"
	"go-ispconfig/internal/site"
)

// ensureSite provisions the site filesystem and system user/group of a
// vhost-type web_domain before its vhost is rendered. The layout itself is
// shared with the nginx plugin (internal/site); Apache only contributes its
// own worker user — [web] user, which joins the client group at security
// level 20 so the server may read the site behind the suexec perms — and its
// own log directory root (/var/log/ispconfig/httpd).
func (p *Plugin) ensureSite(ctx context.Context, cfg *getconf.WebConfig, action string, oldRow, newRow row) error {
	if err := safeDomain(newRow.str("domain")); err != nil {
		return err
	}
	req := site.Request{
		Tag:         "apache2",
		WorkerUser:  cfg.User,
		LogBaseDir:  p.logBaseDir,
		Cfg:         cfg,
		Action:      action,
		Old:         site.Row(oldRow),
		New:         site.Row(newRow),
		ClientID:    p.clientIDOf(newRow.num("sys_groupid")),
		OldClientID: p.clientIDOf(oldRow.num("sys_groupid")),
		Runner:      p.runner,
		Log:         p.log,
	}
	// vhostsubdomain/vhostalias sites log into log/<host> below the parent's
	// document root, so the parent domain has to be resolved first.
	if newRow.str("type") != "vhost" {
		req.ParentDomain = p.domainName(newRow.num("parent_domain_id"))
	}
	return site.Ensure(ctx, req)
}

// domainName returns the domain of a web_domain row ("" when missing).
func (p *Plugin) domainName(domainID int64) string {
	if p.db == nil || domainID == 0 {
		return ""
	}
	var name string
	_ = p.db.Table("web_domain").Where("domain_id = ?", domainID).
		Pluck("domain", &name).Error
	return name
}

// clientIDOf resolves the client id owning a sys_group (0 for admin-owned).
// It feeds the [client_id] placeholder of the website_symlinks config.
func (p *Plugin) clientIDOf(groupID int64) int64 {
	if p.db == nil || groupID == 0 {
		return 0
	}
	var id int64
	_ = p.db.Table("sys_group").Where("groupid = ?", groupID).
		Pluck("client_id", &id).Error
	return id
}
