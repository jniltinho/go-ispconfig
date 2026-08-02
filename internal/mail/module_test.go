package mail

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/engine"
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

func TestMailModuleEvents(t *testing.T) {
	tests := []struct {
		table  string
		action string
		event  string
	}{
		{"mail_domain", "u", "mail_domain_update"},
		{"mail_user", "i", "mail_user_insert"},
		{"mail_forwarding", "d", "mail_forwarding_delete"},
		{"mail_transport", "u", "mail_transport_update"},
		{"mail_access", "i", "mail_access_insert"},
		{"spamfilter_users", "u", "spamfilter_users_update"},
		{"spamfilter_wblist", "d", "spamfilter_wblist_delete"},
	}
	for _, tt := range tests {
		t.Run(tt.event, func(t *testing.T) {
			reg := engine.NewRegistry(nil)
			plugin := &capturePlugin{subscribe: []string{tt.event}}
			require.NoError(t, reg.Load([]engine.Module{NewModule()}, []engine.Plugin{plugin}))

			data := engine.Data{New: map[string]any{"id": float64(7)}}
			require.NoError(t, reg.RaiseTableHook(context.Background(), tt.table, tt.action, data))
			require.Equal(t, []string{tt.event}, plugin.got)
			require.Equal(t, data, plugin.payloads[0], "payload is the decoded {old,new} datalog data")
		})
	}
}

func TestMailModuleOutOfScopeTablesNotHooked(t *testing.T) {
	reg := engine.NewRegistry(nil)
	require.NoError(t, reg.Load([]engine.Module{NewModule()}, nil))
	for _, table := range []string{"mail_get", "mail_mailinglist", "mail_content_filter", "spamfilter_policy"} {
		// No hook registered: the raise is a silent no-op for the table.
		require.NoError(t, reg.RaiseTableHook(context.Background(), table, "u", engine.Data{}),
			"unhooked table %s must not error", table)
	}
}

func TestMailModuleRejectsUnannouncedSubscription(t *testing.T) {
	reg := engine.NewRegistry(nil)
	plugin := &capturePlugin{subscribe: []string{"mail_mailinglist_insert"}}
	require.Error(t, reg.Load([]engine.Module{NewModule()}, []engine.Plugin{plugin}),
		"subscribing an event the mail module did not announce must fail (registry contract)")
}

// recordExec captures service actions.
type recordExec struct {
	mu   sync.Mutex
	runs []string
}

func (e *recordExec) Run(_ context.Context, service, action string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.runs = append(e.runs, service+"/"+action)
	return nil
}

func TestMailServicesRegistration(t *testing.T) {
	exec := &recordExec{}
	services := engine.NewServices(exec, nil)
	RegisterServices(services)

	// Dedup: many reload requests collapse to one execution.
	services.RestartServiceDelayed(PostfixService, engine.ActionReload)
	services.RestartServiceDelayed(PostfixService, engine.ActionReload)
	// Restart wins over reload within the same run.
	services.RestartServiceDelayed(RspamdService, engine.ActionReload)
	services.RestartServiceDelayed(RspamdService, engine.ActionRestart)
	services.RestartServiceDelayed(RspamdService, engine.ActionReload)
	services.RestartServiceDelayed(DovecotService, engine.ActionReload)
	// Amavis is not registered: the request is ignored.
	services.RestartServiceDelayed("amavis", engine.ActionRestart)

	services.ProcessDelayedActions(context.Background())
	assert.ElementsMatch(t,
		[]string{"postfix/reload", "rspamd/restart", "dovecot/reload"},
		exec.runs)
}
