package clients

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/web"
)

// capturePlugin records the events it receives.
type capturePlugin struct {
	subscribe []string
	got       []string
	payloads  []engine.Data
}

func (*capturePlugin) Name() string { return "capture" }

func (p *capturePlugin) OnLoad(r *engine.Registry) error {
	for _, ev := range p.subscribe {
		if err := r.RegisterEvent(ev, func(_ context.Context, event string, data engine.Data) error {
			p.got = append(p.got, event)
			p.payloads = append(p.payloads, data)
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

func TestClientModuleEvents(t *testing.T) {
	tests := []struct {
		action string
		event  string
	}{
		{"i", "client_insert"},
		{"u", "client_update"},
		{"d", "client_delete"},
	}
	for _, tt := range tests {
		t.Run(tt.event, func(t *testing.T) {
			reg := engine.NewRegistry(nil)
			plugin := &capturePlugin{subscribe: []string{tt.event}}
			require.NoError(t, reg.Load([]engine.Module{NewModule()}, []engine.Plugin{plugin}))

			data := engine.Data{Old: map[string]any{"client_id": float64(3)}, New: map[string]any{"client_id": float64(3)}}
			require.NoError(t, reg.RaiseTableHook(context.Background(), "client", tt.action, data))
			require.Equal(t, []string{tt.event}, plugin.got)
			require.Equal(t, data, plugin.payloads[0], "payload is the decoded {old,new} datalog data")
		})
	}
}

func TestClientDeleteReachesWebStackSubscribers(t *testing.T) {
	// Daemon-shaped load: web + client modules together; a plugin
	// subscribing client_delete (like nginx's teardown handler) must
	// resolve its announcement via the client module — the web module no
	// longer carries the stopgap announcement.
	reg := engine.NewRegistry(nil)
	plugin := &capturePlugin{subscribe: []string{"client_delete", "web_domain_delete"}}
	require.NoError(t, reg.Load([]engine.Module{web.NewModule(), NewModule()}, []engine.Plugin{plugin}))

	require.NoError(t, reg.RaiseTableHook(context.Background(), "client", "d",
		engine.Data{Old: map[string]any{"client_id": float64(9)}, New: map[string]any{}}))
	require.Equal(t, []string{"client_delete"}, plugin.got)
}

func TestClientModuleDisableHook(t *testing.T) {
	// The emergency switch keeps announcements (subscriptions Load) but
	// raises nothing.
	reg := engine.NewRegistry(nil)
	plugin := &capturePlugin{subscribe: []string{"client_delete"}}
	mod := NewModule()
	mod.DisableHook = true
	require.NoError(t, reg.Load([]engine.Module{mod}, []engine.Plugin{plugin}))

	require.NoError(t, reg.RaiseTableHook(context.Background(), "client", "d", engine.Data{}))
	require.Empty(t, plugin.got)
}

func TestClientModuleAnnouncesOnly(t *testing.T) {
	// Subscribing to an unannounced event fails Load (registry contract).
	reg := engine.NewRegistry(nil)
	plugin := &capturePlugin{subscribe: []string{"client_reboot"}}
	err := reg.Load([]engine.Module{NewModule()}, []engine.Plugin{plugin})
	require.ErrorIs(t, err, engine.ErrEventNotAnnounced)
}
