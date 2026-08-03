package fail2ban

import (
	"context"
	"log/slog"
	"strings"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/getconf"
)

// Plugin re-renders the panel-owned jail drop-ins when this node's
// server.config changes. The only config-dependent jail is the HTTP one,
// so in practice this reacts to an admin switching [web] server_type
// between nginx and apache: the stale drop-in is pruned and the new one
// written, then fail2ban is reloaded.
type Plugin struct {
	runner engine.CommandRunner
	dir    string
	log    *slog.Logger
}

// NewPlugin creates the fail2ban plugin writing into JailDir; log nil
// means slog.Default.
func NewPlugin(runner engine.CommandRunner, log *slog.Logger) *Plugin {
	if log == nil {
		log = slog.Default()
	}
	return &Plugin{runner: runner, dir: JailDir, log: log}
}

// Name identifies the plugin in logs.
func (*Plugin) Name() string { return "fail2ban" }

// OnLoad hooks the server table directly instead of subscribing to a
// server_update event: server.config changes are journaled as `server`
// datalog rows (datalog.LogServerConfig), and the only module announcing
// server_* is the mail module — which is not loaded on a node without
// mail_server = 1.
func (p *Plugin) OnLoad(r *engine.Registry) error {
	r.RegisterTableHook("server", p.serverChanged)
	return nil
}

// serverChanged re-applies the jail set for the web server named in the
// new config. Apply is idempotent, so an unrelated config change (any
// other key) writes nothing and never reloads fail2ban.
func (p *Plugin) serverChanged(ctx context.Context, _, _ string, data engine.Data) error {
	ini, _ := data.New["config"].(string)
	if ini == "" {
		return nil
	}
	webServer := strings.TrimSpace(getconf.ParseINI(getconf.StripSlashes(ini))["web"]["server_type"])
	if err := Apply(ctx, p.runner, p.dir, webServer); err != nil {
		return err
	}
	p.log.Debug("fail2ban: jails applied", "web_server", webServer)
	return nil
}
