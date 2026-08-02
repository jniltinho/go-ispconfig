package firewall

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/engine"
)

// recorder subscribes to every announced firewall event and records dispatches.
type recorder struct {
	events []string
	last   engine.Data
}

func (r *recorder) Name() string { return "recorder" }

func (r *recorder) OnLoad(reg *engine.Registry) error {
	for _, a := range []string{"_insert", "_update", "_delete"} {
		if err := reg.RegisterEvent("firewall"+a, func(_ context.Context, event string, data engine.Data) error {
			r.events = append(r.events, event)
			r.last = data
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

// TestModuleRaisesNamedEvents covers the firewall-module-events spec: each
// firewall table + datalog action i/u/d maps to its named event with the
// {old,new} payload intact.
func TestModuleRaisesNamedEvents(t *testing.T) {
	reg := engine.NewRegistry(nil)
	rec := &recorder{}
	require.NoError(t, reg.Load([]engine.Module{NewModule()}, []engine.Plugin{rec}))

	data := engine.Data{
		Old: map[string]any{"tcp_port": "22", "active": "y"},
		New: map[string]any{"tcp_port": "22,80", "active": "y"},
	}
	ctx := context.Background()
	require.NoError(t, reg.RaiseTableHook(ctx, "firewall", "i", data))
	require.NoError(t, reg.RaiseTableHook(ctx, "firewall", "u", data))
	require.NoError(t, reg.RaiseTableHook(ctx, "firewall", "d", data))

	assert.Equal(t, []string{"firewall_insert", "firewall_update", "firewall_delete"}, rec.events)
	assert.Equal(t, "22,80", rec.last.New["tcp_port"])
	assert.Equal(t, "22", rec.last.Old["tcp_port"])
}

// TestUnannouncedEventRejected: subscribing to an event the firewall module
// did not announce fails registry loading (foundation contract).
func TestUnannouncedEventRejected(t *testing.T) {
	reg := engine.NewRegistry(nil)
	require.NoError(t, NewModule().OnLoad(reg))
	err := reg.RegisterEvent("firewall_bogus_event", func(context.Context, string, engine.Data) error { return nil })
	require.ErrorIs(t, err, engine.ErrEventNotAnnounced)
}

// TestModuleName identifies the module as "firewall".
func TestModuleName(t *testing.T) {
	assert.Equal(t, "firewall", NewModule().Name())
}
