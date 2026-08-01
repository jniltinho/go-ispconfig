package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// tblModule is a minimal module announcing web_domain events and translating
// table changes into them.
type tblModule struct{}

func (tblModule) Name() string { return "web_module" }

func (tblModule) OnLoad(r *Registry) error {
	r.AnnounceEvents("web_module", "web_domain_insert", "web_domain_update", "web_domain_delete")
	r.RegisterTableHook("web_domain", func(ctx context.Context, table, action string, data Data) error {
		return r.RaiseEvent(ctx, EventName(table, action), data)
	})
	return nil
}

// evtPlugin subscribes to web_domain_update and records what it receives.
type evtPlugin struct {
	got   []Data
	event string
}

func (p *evtPlugin) Name() string { return "evt_plugin" }

func (p *evtPlugin) OnLoad(r *Registry) error {
	return r.RegisterEvent("web_domain_update", func(_ context.Context, event string, data Data) error {
		p.event = event
		p.got = append(p.got, data)
		return nil
	})
}

// badPlugin subscribes to an event nobody announced.
type badPlugin struct{}

func (badPlugin) Name() string { return "bad_plugin" }

func (badPlugin) OnLoad(r *Registry) error {
	return r.RegisterEvent("mail_domain_update", func(context.Context, string, Data) error { return nil })
}

func TestRegistryDispatch(t *testing.T) {
	r := NewRegistry(nil)
	p := &evtPlugin{}
	require.NoError(t, r.Load([]Module{tblModule{}}, []Plugin{p}))

	data := Data{Old: map[string]any{"active": "y"}, New: map[string]any{"active": "n"}}
	require.NoError(t, r.RaiseTableHook(context.Background(), "web_domain", "u", data))

	require.Equal(t, "web_domain_update", p.event)
	require.Len(t, p.got, 1)
	require.Equal(t, "n", p.got[0].New["active"])

	// Insert action reaches no subscriber but must not error.
	require.NoError(t, r.RaiseTableHook(context.Background(), "web_domain", "i", data))
	require.Len(t, p.got, 1)
}

func TestRegistryRejectsUnannouncedEvent(t *testing.T) {
	r := NewRegistry(nil)
	err := r.Load([]Module{tblModule{}}, []Plugin{badPlugin{}})
	require.ErrorIs(t, err, ErrEventNotAnnounced)
	require.Contains(t, err.Error(), "bad_plugin")
}

func TestRegistryHookErrorsJoined(t *testing.T) {
	r := NewRegistry(nil)
	boom := errors.New("boom")
	calls := 0
	r.RegisterTableHook("dns_soa", func(context.Context, string, string, Data) error { calls++; return boom })
	r.RegisterTableHook("dns_soa", func(context.Context, string, string, Data) error { calls++; return nil })

	err := r.RaiseTableHook(context.Background(), "dns_soa", "u", Data{})
	require.ErrorIs(t, err, boom)
	require.Equal(t, 2, calls, "later hooks still run after an error")
}

func TestRaiseActionSeverity(t *testing.T) {
	ctx := context.Background()
	r := NewRegistry(nil)
	r.RegisterAction("os_update", func(context.Context, string, string) (string, error) { return StateOK, nil })
	require.Equal(t, StateOK, r.RaiseAction(ctx, "os_update", ""))
	require.Equal(t, StateWarning, r.RaiseAction(ctx, "unknown_action", ""), "no handler must be visible, never a silent ok")

	r.RegisterAction("os_update", func(context.Context, string, string) (string, error) { return StateWarning, nil })
	require.Equal(t, StateWarning, r.RaiseAction(ctx, "os_update", ""))

	r.RegisterAction("os_update", func(context.Context, string, string) (string, error) { return "", errors.New("fail") })
	require.Equal(t, StateError, r.RaiseAction(ctx, "os_update", ""))
}

func TestEventName(t *testing.T) {
	require.Equal(t, "web_domain_insert", EventName("web_domain", "i"))
	require.Equal(t, "web_domain_update", EventName("web_domain", "u"))
	require.Equal(t, "web_domain_delete", EventName("web_domain", "d"))
}
