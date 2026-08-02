package firewall

import (
	"context"
	"log/slog"

	"go-ispconfig/internal/engine"
)

// Plugin is the UFW firewall plugin (port of firewall_plugin.inc.php UFW
// path only — Bastille is a non-goal, design D2). It subscribes to the
// events announced by the firewall module and applies them via the
// foundation CommandRunner.
type Plugin struct {
	runner   engine.CommandRunner
	serverID uint32
	log      *slog.Logger

	// panelPort is the panel listen port from config.toml server.port
	// (fallback DefaultPanelPort). Used by the lock-out guard (task 2.4).
	panelPort int

	// sshPort overrides the SSH protected port for tests. Zero means
	// resolve from server.config [server] ssh_port (fallback DefaultSSHPort)
	// via LoadSSHPort when set, else DefaultSSHPort.
	sshPort int

	// LoadSSHPort optionally resolves the live SSH port from getconf.
	// Nil means DefaultSSHPort (or sshPort when non-zero).
	LoadSSHPort func(ctx context.Context) int
}

// DefaultPanelPort is the config.toml server.port fallback (design D6).
const DefaultPanelPort = 8080

// DefaultSSHPort is the server.config [server] ssh_port fallback (design D6).
const DefaultSSHPort = 22

// NewPlugin creates the UFW plugin for one server. panelPort <= 0 falls
// back to DefaultPanelPort. log nil means slog.Default.
func NewPlugin(runner engine.CommandRunner, serverID uint32, panelPort int, log *slog.Logger) *Plugin {
	if log == nil {
		log = slog.Default()
	}
	if panelPort <= 0 {
		panelPort = DefaultPanelPort
	}
	return &Plugin{
		runner:    runner,
		serverID:  serverID,
		panelPort: panelPort,
		log:       log,
	}
}

// Name identifies the plugin in logs and the registry.
func (*Plugin) Name() string { return "ufw" }

// OnLoad subscribes the plugin to the three firewall module events
// (port of firewall_plugin.inc.php onLoad). Insert shares the update
// path (PHP insert → update).
func (p *Plugin) OnLoad(r *engine.Registry) error {
	subs := []struct {
		event string
		fn    engine.EventFunc
	}{
		{"firewall_insert", p.onInsert},
		{"firewall_update", p.onUpdate},
		{"firewall_delete", p.onDelete},
	}
	for _, s := range subs {
		if err := r.RegisterEvent(s.event, s.fn); err != nil {
			return err
		}
	}
	return nil
}

// onInsert handles firewall_insert (PHP parity: same path as update,
// with the insert baseline branch inside ufwUpdate).
func (p *Plugin) onInsert(ctx context.Context, event string, data engine.Data) error {
	return p.ufwUpdate(ctx, event, data)
}

// onUpdate handles firewall_update.
func (p *Plugin) onUpdate(ctx context.Context, event string, data engine.Data) error {
	return p.ufwUpdate(ctx, event, data)
}

// onDelete handles firewall_delete.
func (p *Plugin) onDelete(ctx context.Context, event string, data engine.Data) error {
	return p.ufwDelete(ctx, event, data)
}

// payloadServerID returns the server_id from new (preferred) or old
// (delete path) payload fields.
func payloadServerID(data engine.Data) uint32 {
	if n := row(data.New).num("server_id"); n > 0 {
		return uint32(n)
	}
	if n := row(data.Old).num("server_id"); n > 0 {
		return uint32(n)
	}
	return 0
}

// isLocal reports whether the event targets this daemon's server.
func (p *Plugin) isLocal(data engine.Data) bool {
	sid := payloadServerID(data)
	return sid == 0 || sid == p.serverID
}
