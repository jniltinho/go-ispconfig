package mail

import (
	"context"
	"os"
	"os/user"
	"strings"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/getconf"
)

// DkimPlugin writes DKIM key files and the Rspamd signing maps (port of
// mail_plugin_dkim.inc.php, Rspamd branch only — amavis is not ported).
type DkimPlugin struct {
	base *Plugin
	// RspamdLocalDir is where dkim_domains.map / dkim_selectors.map live
	// (default /etc/rspamd/local.d; tests point it at a temp dir).
	RspamdLocalDir string
	// UserExists reports whether a system user exists (rspamd owner
	// probing); nil means os/user lookup.
	UserExists func(name string) bool
}

// NewDkimPlugin creates the dkim plugin on top of the shared plumbing.
func NewDkimPlugin(base *Plugin) *DkimPlugin {
	return &DkimPlugin{base: base, RspamdLocalDir: "/etc/rspamd/local.d"}
}

// Name identifies the plugin in logs and the registry.
func (*DkimPlugin) Name() string { return "dkim" }

// OnLoad subscribes to the mail domain events.
func (d *DkimPlugin) OnLoad(r *engine.Registry) error {
	for event, handler := range map[string]func(context.Context, engine.Data) error{
		"mail_domain_insert": d.domainInsert,
		"mail_domain_update": d.domainUpdate,
		"mail_domain_delete": d.domainDelete,
	} {
		h := handler
		if err := r.RegisterEvent(event, func(ctx context.Context, _ string, data engine.Data) error {
			return h(ctx, data)
		}); err != nil {
			return err
		}
	}
	return nil
}

// rspamdOwner probes the rspamd system user name (_rspamd on Debian,
// rspamd elsewhere); empty means root.
func (d *DkimPlugin) rspamdOwner() string {
	exists := d.UserExists
	if exists == nil {
		exists = func(name string) bool { _, err := user.Lookup(name); return err == nil }
	}
	for _, name := range []string{"_rspamd", "rspamd"} {
		if exists(name) {
			return name
		}
	}
	return ""
}

// checkSystem validates dkim_path and ensures it exists (0750 owned by
// the rspamd user when present, PHP check_system trimmed to the rspamd
// branch). A symlinked or unusable path disables DKIM handling.
func (d *DkimPlugin) checkSystem(ctx context.Context, cfg getconf.MailConfig) bool {
	if cfg.ContentFilter != "rspamd" {
		d.base.log.Warn("mail: content_filter is not rspamd, dkim plugin skipping", "content_filter", cfg.ContentFilter)
		return false
	}
	path := strings.TrimSuffix(cfg.DKIMPath, "/")
	if path == "" {
		d.base.log.Error("mail: no or invalid dkim_path defined")
		return false
	}
	if fi, err := os.Lstat(path); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			d.base.log.Error("mail: dkim_path is a symlink, refusing", "path", path)
			return false
		}
		if !fi.IsDir() {
			d.base.log.Error("mail: dkim_path exists but is not a directory", "path", path)
			return false
		}
		return true
	}
	owner := d.rspamdOwner()
	if owner == "" {
		if err := os.MkdirAll(path, 0o755); err != nil {
			d.base.log.Error("mail: could not create dkim_path", "path", path, "error", err)
			return false
		}
		d.base.log.Warn("mail: no rspamd user found - using root for dkim_path", "path", path)
		return true
	}
	if err := d.base.mkdirOwned(ctx, path, 0o750, owner+":"+owner); err != nil {
		d.base.log.Error("mail: could not create dkim_path", "path", path, "error", err)
		return false
	}
	return true
}

// keyBase returns <dkim_path>/<domain> (no extension).
func keyBase(cfg getconf.MailConfig, domain string) string {
	return strings.TrimSuffix(cfg.DKIMPath, "/") + "/" + domain
}

// writeDKIMKey writes the private and derived public key files (0640,
// rspamd-owned; design D6 tightens PHP's default modes).
func (d *DkimPlugin) writeDKIMKey(ctx context.Context, cfg getconf.MailConfig, domain, privateKey string) bool {
	if domain == "" || privateKey == "" {
		d.base.log.Error("mail: DKIM internal error, empty domain or key", "domain", domain)
		return false
	}
	base := keyBase(cfg, domain)
	if err := os.WriteFile(base+".private", []byte(privateKey), 0o640); err != nil {
		d.base.log.Error("mail: unable to save DKIM private key", "file", base+".private", "error", err)
		return false
	}
	public, err := DeriveDKIMPublic(privateKey)
	if err != nil {
		d.base.log.Error("mail: unable to derive DKIM public key", "domain", domain, "error", err)
	} else if err := os.WriteFile(base+".public", []byte(public), 0o640); err != nil {
		d.base.log.Error("mail: unable to save DKIM public key", "file", base+".public", "error", err)
	}
	if owner := d.rspamdOwner(); owner != "" {
		for _, f := range []string{base + ".private", base + ".public"} {
			if _, err := d.base.runner.Run(ctx, "chown", owner+":"+owner, f); err != nil {
				d.base.log.Warn("mail: chown dkim key failed", "file", f, "error", err)
			}
		}
	}
	return true
}

// removeDKIMKey deletes both key files.
func (d *DkimPlugin) removeDKIMKey(cfg getconf.MailConfig, domain string) {
	base := keyBase(cfg, domain)
	for _, f := range []string{base + ".private", base + ".public"} {
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
			d.base.log.Warn("mail: unable to delete DKIM key file", "file", f, "error", err)
		}
	}
}

// mapSetLine replaces the `<domain> ...` line in an Rspamd map (append
// when absent — system.replaceLine parity).
func (d *DkimPlugin) mapSetLine(file, domain, line string) {
	raw, err := os.ReadFile(file)
	if err != nil && !os.IsNotExist(err) {
		d.base.log.Error("mail: reading rspamd map failed", "file", file, "error", err)
		return
	}
	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	replaced := false
	var out []string
	for _, l := range lines {
		if l == "" {
			continue
		}
		if strings.HasPrefix(l, domain+" ") {
			if !replaced {
				out = append(out, line)
				replaced = true
			}
			continue
		}
		out = append(out, l)
	}
	if !replaced {
		out = append(out, line)
	}
	if err := os.WriteFile(file, []byte(strings.Join(out, "\n")+"\n"), 0o644); err != nil {
		d.base.log.Error("mail: writing rspamd map failed", "file", file, "error", err)
	}
}

// mapRemoveLine drops the `<domain> ...` line from an Rspamd map.
func (d *DkimPlugin) mapRemoveLine(file, domain string) {
	raw, err := os.ReadFile(file)
	if err != nil {
		return
	}
	var out []string
	for _, l := range strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n") {
		if l == "" || strings.HasPrefix(l, domain+" ") {
			continue
		}
		out = append(out, l)
	}
	content := ""
	if len(out) > 0 {
		content = strings.Join(out, "\n") + "\n"
	}
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		d.base.log.Error("mail: writing rspamd map failed", "file", file, "error", err)
	}
}

// addDKIM writes key files and map lines for an active domain (port of
// add_dkim, rspamd branch).
func (d *DkimPlugin) addDKIM(ctx context.Context, cfg getconf.MailConfig, newRow row) {
	if newRow.str("active") != "y" {
		return
	}
	domain := newRow.str("domain")
	if !d.writeDKIMKey(ctx, cfg, domain, newRow.str("dkim_private")) {
		d.base.log.Debug("mail: DKIM not enabled for domain, key save failed", "domain", domain)
		return
	}
	d.mapSetLine(d.RspamdLocalDir+"/dkim_domains.map", domain, domain+" "+keyBase(cfg, domain)+".private")
	d.mapSetLine(d.RspamdLocalDir+"/dkim_selectors.map", domain, domain+" "+newRow.str("dkim_selector"))
	d.base.services.RestartServiceDelayed(RspamdService, engine.ActionReload)
}

// removeDKIM deletes key files and map lines (port of remove_dkim).
func (d *DkimPlugin) removeDKIM(cfg getconf.MailConfig, domainRow row) {
	domain := domainRow.str("domain")
	d.removeDKIMKey(cfg, domain)
	d.mapRemoveLine(d.RspamdLocalDir+"/dkim_domains.map", domain)
	d.mapRemoveLine(d.RspamdLocalDir+"/dkim_selectors.map", domain)
	d.base.services.RestartServiceDelayed(RspamdService, engine.ActionReload)
}

// domainInsert handles mail_domain_insert.
func (d *DkimPlugin) domainInsert(ctx context.Context, data engine.Data) error {
	newRow := row(data.New)
	cfg, _ := d.base.config(ctx)
	if newRow.str("dkim") == "y" && d.checkSystem(ctx, cfg) {
		d.addDKIM(ctx, cfg, newRow)
	}
	return nil
}

// domainDelete handles mail_domain_delete.
func (d *DkimPlugin) domainDelete(ctx context.Context, data engine.Data) error {
	oldRow := row(data.Old)
	cfg, _ := d.base.config(ctx)
	if oldRow.str("dkim") == "y" && oldRow.str("active") == "y" {
		d.removeDKIM(cfg, oldRow)
	}
	return nil
}

// domainUpdate ports the domain_dkim_update transition table: domain
// deactivate/reactivate, dkim toggle, key/selector change, rename
// (old materials removed first) and same-payload resync.
func (d *DkimPlugin) domainUpdate(ctx context.Context, data engine.Data) error {
	oldRow, newRow := row(data.Old), row(data.New)
	if newRow.str("dkim") != "y" && oldRow.str("dkim") != "y" {
		return nil
	}
	cfg, _ := d.base.config(ctx)
	if !d.checkSystem(ctx, cfg) {
		return nil
	}
	newActive, oldActive := newRow.str("active"), oldRow.str("active")
	newDKIM, oldDKIM := newRow.str("dkim"), oldRow.str("dkim")

	switch {
	case newActive == "n" && oldActive == "y" && newDKIM == "y":
		d.base.log.Debug("mail: domain disabled, removing DKIM settings", "domain", newRow.str("domain"))
		d.removeDKIM(cfg, newRow)
	case newActive == "y" && oldActive == "n" && newDKIM == "y":
		d.addDKIM(ctx, cfg, newRow)
	case newActive == "y" && oldActive == "y":
		changed := newDKIM != oldDKIM ||
			newRow.str("dkim_private") != oldRow.str("dkim_private") ||
			newRow.str("dkim_selector") != oldRow.str("dkim_selector") ||
			newRow.str("domain") != oldRow.str("domain")
		switch {
		case newDKIM == "n" && newDKIM != oldDKIM:
			d.removeDKIM(cfg, newRow)
		case changed && newDKIM == "y":
			if newRow.str("domain") != oldRow.str("domain") {
				d.removeDKIM(cfg, oldRow)
			}
			d.addDKIM(ctx, cfg, newRow)
		case !changed && newDKIM == "y":
			// Resync touch: rewrite materials from the DB payload.
			d.addDKIM(ctx, cfg, newRow)
		}
	}
	return nil
}
