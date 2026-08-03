package mail

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/getconf"
	"go-ispconfig/internal/mastertpl"
)

// getmailTemplate is the embedded rc template (copied verbatim from
// ISPConfig3 server/conf/getmail.conf.master).
const getmailTemplate = "getmail.conf.master"

// getmailFetchSpec is the interval of the fetch job; it replaces the
// "*/5 * * * * /usr/local/bin/run-getmail.sh" crontab the PHP installer
// writes for the getmail user (design D5).
const getmailFetchSpec = "*/5 * * * *"

// retrievers maps mail_get.type to the getmail retriever class
// (getmail_plugin.inc.php::update).
var retrievers = map[string]string{
	"pop3":    "SimplePOP3Retriever",
	"imap":    "SimpleIMAPRetriever",
	"pop3ssl": "SimplePOP3SSLRetriever",
	"imapssl": "SimpleIMAPSSLRetriever",
}

// GetmailPlugin writes one getmail rc file per mail_get row and runs the
// periodic fetch (port of getmail_plugin.inc.php + run-getmail.sh).
//
// The daemon only runs on master servers (mirror_server_id = 0 is
// enforced at startup), so the PHP mirror-server guard is implicit.
type GetmailPlugin struct {
	base *Plugin
	// customTplDir is the .master override directory (conf-custom).
	customTplDir string
	// fetching is the single-flight guard replacing run-getmail.sh's
	// /tmp/.getmail_lock sentinel.
	fetching sync.Mutex
	// LookupUser resolves the getmail user to uid/gid; nil means os/user.
	// Tests inject a fake.
	LookupUser func(name string) (uid, gid string, err error)
	// LoadConfig loads the [getmail] getconf section; nil means the DB
	// getconf of this plugin's server. Tests inject a fixed config.
	LoadConfig func(ctx context.Context) (getconf.GetmailConfig, error)
}

// NewGetmailPlugin creates the getmail plugin on top of the shared mail
// plugin plumbing.
func NewGetmailPlugin(base *Plugin, customTplDir string) *GetmailPlugin {
	return &GetmailPlugin{base: base, customTplDir: customTplDir}
}

// Name identifies the plugin in logs and the registry.
func (*GetmailPlugin) Name() string { return "getmail" }

// OnLoad subscribes to the mail_get events (PHP: insert/update → update,
// delete → delete).
func (g *GetmailPlugin) OnLoad(r *engine.Registry) error {
	for event, handler := range map[string]func(context.Context, engine.Data) error{
		"mail_get_insert": g.update,
		"mail_get_update": g.update,
		"mail_get_delete": g.delete,
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

// config loads the [getmail] section of this server, falling back to the
// defaults. A non-absolute config dir is refused so a blanked-out value
// can never disarm the containment guard.
func (g *GetmailPlugin) config(ctx context.Context) (getconf.GetmailConfig, error) {
	cfg := getconf.DefaultGetmailConfig()
	if g.LoadConfig != nil {
		loaded, err := g.LoadConfig(ctx)
		if err != nil {
			return cfg, err
		}
		cfg = loaded
	} else if g.base.db != nil {
		full, err := getconf.GetServerConfig(g.base.db, g.base.serverID)
		if err != nil {
			g.base.log.Warn("getmail: using default [getmail] config", "error", err)
		} else {
			cfg = full.Getmail
		}
	}
	if cfg.ConfigDir == "" {
		cfg.ConfigDir = getconf.DefaultGetmailConfig().ConfigDir
	}
	cfg.ConfigDir = strings.TrimSuffix(cfg.ConfigDir, "/")
	if !filepath.IsAbs(cfg.ConfigDir) {
		return cfg, fmt.Errorf("getmail: getmail_config_dir %q is not an absolute path", cfg.ConfigDir)
	}
	if cfg.Program == "" {
		cfg.Program = getconf.DefaultGetmailConfig().Program
	}
	if cfg.User == "" {
		cfg.User = getconf.DefaultGetmailConfig().User
	}
	return cfg, nil
}

// cleanPathRe is _clean_path in getmail_plugin.inc.php: every character
// outside [A-Za-z0-9-_] becomes an underscore. Kept byte-identical so rc
// files written by a previous PHP installation are re-used, not
// duplicated.
var cleanPathRe = regexp.MustCompile(`[^A-Za-z0-9\-_]`)

// cleanPath sanitises one path component.
func cleanPath(in string) string { return cleanPathRe.ReplaceAllString(in, "_") }

// rcPath assembles and validates the rc file path of one account.
// Guard 1 is PHP's metacharacter check on the assembled path; guard 2
// asserts the result still sits directly in the config dir (cleanPath
// already makes traversal impossible — this survives a future change to
// the naming scheme).
func rcPath(dir string, r row) (string, error) {
	path := dir + "/" + cleanPath(r.str("source_server")) + "_" + cleanPath(r.str("source_username")) + ".conf"
	if strings.ContainsAny(path, "|;$") || strings.Contains(path, "..") {
		return "", fmt.Errorf("getmail: possibly faked path for config file %q", path)
	}
	if filepath.Dir(filepath.Clean(path)) != dir {
		return "", fmt.Errorf("getmail: config file %q escapes %q", path, dir)
	}
	return path, nil
}

// render substitutes the template placeholders exactly as PHP str_replace
// does. The rendered body holds a cleartext password and is never logged.
func render(src string, r row) (string, error) {
	retriever, ok := retrievers[r.str("type")]
	if !ok {
		// PHP leaves {TYPE} unsubstituted, which makes getmail fail the
		// whole batch run; this port refuses the row instead.
		return "", fmt.Errorf("getmail: unknown retriever type %q", r.str("type"))
	}
	return strings.NewReplacer(
		"{DELETE}", phpBool(r.str("source_delete")),
		"{READ_ALL}", phpBool(r.str("source_read_all")),
		"{TYPE}", retriever,
		"{SERVER}", r.str("source_server"),
		"{USERNAME}", r.str("source_username"),
		"{PASSWORD}", r.str("source_password"),
		"{DESTINATION}", r.str("destination"),
	).Replace(src), nil
}

// phpBool maps the 'y'/'n' flags to the getmail booleans.
func phpBool(v string) string {
	if v == "y" {
		return "true"
	}
	return "false"
}

// update deletes the rc file named from {old} and, when the row is
// active, writes the one named from {new} (PHP ordering, so renaming the
// source account never leaves an orphan).
func (g *GetmailPlugin) update(ctx context.Context, data engine.Data) error {
	cfg, err := g.config(ctx)
	if err != nil {
		g.base.log.Error("getmail: bad config, event ignored", "error", err)
		return nil
	}
	// The daemon must never create the directory: without the getmail
	// ownership the rc files would be unreadable to the fetch job.
	if !isDir(cfg.ConfigDir) {
		g.base.log.Error("getmail: config directory does not exist", "dir", cfg.ConfigDir)
		return nil
	}
	if err := g.delete(ctx, data); err != nil {
		return err
	}

	newRow := row(data.New)
	path, err := rcPath(cfg.ConfigDir, newRow)
	if err != nil {
		g.base.log.Error("getmail: refusing rc path", "error", err)
		return nil
	}
	if newRow.str("active") != "y" {
		g.removeRC(path)
		return nil
	}

	src, source, err := mastertpl.Load(getmailTemplate, g.customTplDir)
	if err != nil {
		return err
	}
	out, err := render(src, newRow)
	if err != nil {
		g.base.log.Error("getmail: rc not written", "file", path, "error", err)
		return nil
	}
	if err := os.WriteFile(path, []byte(out), 0o600); err != nil {
		return fmt.Errorf("getmail: writing %s: %w", path, err)
	}
	// Ownership first, final mode second: a 0600 root-owned file is safe
	// in the meantime, the inverse order is not.
	owner := cfg.User + ":" + cfg.User
	if _, err := g.base.runner.Run(ctx, "chown", owner, path); err != nil {
		g.base.log.Warn("getmail: chown failed", "file", path, "error", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		g.base.log.Warn("getmail: chmod failed", "file", path, "error", err)
	}
	g.base.log.Debug("getmail: wrote rc file", "file", path, "template", source)
	return nil
}

// delete removes the rc file named from the {old} payload. A missing
// file is not an error.
func (g *GetmailPlugin) delete(ctx context.Context, data engine.Data) error {
	cfg, err := g.config(ctx)
	if err != nil {
		g.base.log.Error("getmail: bad config, event ignored", "error", err)
		return nil
	}
	oldRow := row(data.Old)
	if oldRow.str("source_server") == "" && oldRow.str("source_username") == "" {
		return nil
	}
	path, err := rcPath(cfg.ConfigDir, oldRow)
	if err != nil {
		g.base.log.Error("getmail: refusing rc path", "error", err)
		return nil
	}
	g.removeRC(path)
	return nil
}

// removeRC unlinks one rc file, tolerating its absence.
func (g *GetmailPlugin) removeRC(path string) {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		g.base.log.Warn("getmail: unable to delete rc file", "file", path, "error", err)
		return
	}
	g.base.log.Debug("getmail: removed rc file", "file", path)
}

// RegisterFetchJob registers the periodic fetch (replacement for the
// getmail crontab and run-getmail.sh, design D5).
func (g *GetmailPlugin) RegisterFetchJob(s *engine.Scheduler) error {
	return s.Register("getmail_fetch", getmailFetchSpec, g.fetchJob)
}

// fetchJob runs one getmail invocation over every rc file, unprivileged.
// A non-zero exit is logged but not returned: one unreachable remote
// server must not mask the accounts that did work (run-getmail.sh's
// "|| true"). Failures to start the program are real job errors.
func (g *GetmailPlugin) fetchJob(ctx context.Context) error {
	if !g.fetching.TryLock() {
		g.base.log.Info("getmail: a fetch is already running, skipping this activation")
		return nil
	}
	defer g.fetching.Unlock()

	cfg, err := g.config(ctx)
	if err != nil {
		return err
	}
	rcFiles, err := rcFiles(cfg.ConfigDir)
	if err != nil || len(rcFiles) == 0 {
		return err
	}
	uid, gid, err := g.lookupUser(cfg.User)
	if err != nil {
		return fmt.Errorf("getmail: resolving user %q: %w", cfg.User, err)
	}

	// setpriv drops to the getmail user for this one invocation; the
	// daemon's CommandRunner has no per-command credential field, and
	// util-linux ships setpriv on every supported distro.
	args := []string{"--reuid", uid, "--regid", gid, "--clear-groups",
		cfg.Program, "-g", cfg.ConfigDir}
	for _, f := range rcFiles {
		args = append(args, "-r", f)
	}
	out, err := g.base.runner.Run(ctx, "setpriv", args...)
	if err != nil {
		g.base.log.Error("getmail: fetch reported errors", "error", err, "output", string(out))
	}
	return nil
}

// rcFiles lists the *.conf files directly in dir, sorted for a
// deterministic argv. A missing directory yields no files, no error.
func rcFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("getmail: reading %s: %w", dir, err)
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".conf") {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(files)
	return files, nil
}

// lookupUser resolves the getmail user to its uid/gid strings.
func (g *GetmailPlugin) lookupUser(name string) (string, string, error) {
	if g.LookupUser != nil {
		return g.LookupUser(name)
	}
	u, err := user.Lookup(name)
	if err != nil {
		return "", "", err
	}
	return u.Uid, u.Gid, nil
}
