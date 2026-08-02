package mail

import (
	"context"
	"os"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/mastertpl"
)

// MaildeliverPlugin renders and compiles the ISPConfig sieve scripts of
// a mailbox (port of maildeliver_plugin.inc.php, design D1/D5). It
// shares the base plugin's db/runner/config plumbing.
type MaildeliverPlugin struct {
	base *Plugin
	// customTplDir is the .master override directory (conf-custom).
	customTplDir string
}

// NewMaildeliverPlugin creates the maildeliver plugin on top of the
// shared mail plugin plumbing.
func NewMaildeliverPlugin(base *Plugin, customTplDir string) *MaildeliverPlugin {
	return &MaildeliverPlugin{base: base, customTplDir: customTplDir}
}

// Name identifies the plugin in logs and the registry.
func (*MaildeliverPlugin) Name() string { return "maildeliver" }

// OnLoad subscribes to the mailbox events (PHP: insert/update → update,
// delete → delete).
func (m *MaildeliverPlugin) OnLoad(r *engine.Registry) error {
	for event, handler := range map[string]func(context.Context, engine.Data) error{
		"mail_user_insert": m.update,
		"mail_user_update": m.update,
		"mail_user_delete": m.delete,
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

// sieveArtifacts lists every sieve file the plugin manages for a
// maildir, including legacy leftovers cleaned before each write.
func sieveArtifacts(maildir string) []string {
	return []string{
		maildir + "/sieve/ispconfig.sieve", // pre-3.1 location
		maildir + "/.sieve.svbin",
		maildir + "/.ispconfig-before.sieve",
		maildir + "/.ispconfig-before.svbin",
		maildir + "/.ispconfig.sieve",
		maildir + "/.ispconfig.svbin",
	}
}

// removeSieveArtifacts unlinks the managed files plus a broken .sieve
// symlink (PHP cleanup block).
func (m *MaildeliverPlugin) removeSieveArtifacts(maildir string) {
	if maildir == "" {
		return
	}
	if fi, err := os.Lstat(maildir + "/.sieve"); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		if _, err := os.Stat(maildir + "/.sieve"); err != nil {
			_ = os.Remove(maildir + "/.sieve") // broken link
		}
	}
	for _, f := range sieveArtifacts(maildir) {
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
			m.base.log.Warn("mail: unable to delete sieve file", "file", f, "error", err)
		}
	}
}

// update re-renders both sieve scripts when a maildeliver-relevant
// field changed (port of maildeliver_plugin::update).
func (m *MaildeliverPlugin) update(ctx context.Context, data engine.Data) error {
	oldRow, newRow := row(data.Old), row(data.New)
	if !sieveRelevantChanged(oldRow, newRow) {
		return nil
	}
	cfg, err := m.base.config(ctx)
	if err != nil {
		m.base.log.Warn("mail: using default [mail] config", "error", err)
	}
	maildir := newRow.str("maildir")
	if maildir == "" {
		return nil
	}
	m.base.log.Debug("mail: mailfilter config changed, rewriting sieve", "maildir", maildir)
	m.removeSieveArtifacts(maildir)

	src, source, err := mastertpl.Load(sieveTemplate, m.customTplDir)
	if err != nil {
		return err
	}
	m.base.log.Debug("mail: sieve template loaded", "source", source)

	addresses := m.base.collectSieveAddresses(ctx, newRow.str("email"))
	vars := sieveVars(newRow, addresses)

	// PHP creates the (vestigial) sieve/ subdir on every render.
	if !isDir(maildir + "/sieve") {
		if err := m.base.mkdirOwned(ctx, maildir+"/sieve", 0o700, cfg.MailuserName+":"+cfg.MailuserGroup); err != nil {
			return err
		}
	}

	owner := cfg.MailuserName + ":" + cfg.MailuserGroup
	files := map[string][2]string{
		"before": {maildir + "/.ispconfig-before.sieve", maildir + "/.ispconfig-before.svbin"},
		"after":  {maildir + "/.ispconfig.sieve", maildir + "/.ispconfig.svbin"},
	}
	for _, script := range []string{"before", "after"} {
		out, err := renderSieve(src, script, vars)
		if err != nil {
			return err
		}
		sieveFile, svbinFile := files[script][0], files[script][1]
		if err := os.WriteFile(sieveFile, []byte(out), 0o600); err != nil {
			m.base.log.Warn("mail: unable to write sieve filter file", "file", sieveFile, "error", err)
			continue
		}
		m.chownFile(ctx, sieveFile, owner)
		if _, err := m.base.runner.Run(ctx, "sievec", sieveFile); err != nil {
			m.base.log.Error("mail: sievec failed", "file", sieveFile, "error", err)
		}
		if _, err := os.Stat(svbinFile); err == nil {
			if err := os.Chmod(svbinFile, 0o600); err != nil {
				m.base.log.Warn("mail: chmod svbin failed", "file", svbinFile, "error", err)
			}
			m.chownFile(ctx, svbinFile, owner)
		}
	}
	return nil
}

// delete removes the sieve artifacts of a deleted mailbox.
func (m *MaildeliverPlugin) delete(_ context.Context, data engine.Data) error {
	m.removeSieveArtifacts(row(data.Old).str("maildir"))
	return nil
}

// chownFile applies ownership through the command runner (root daemon).
func (m *MaildeliverPlugin) chownFile(ctx context.Context, file, owner string) {
	if _, err := m.base.runner.Run(ctx, "chown", owner, file); err != nil {
		m.base.log.Warn("mail: chown failed", "file", file, "error", err)
	}
}
