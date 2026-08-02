package dns

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/engine"
)

// recorder subscribes to every announced dns event and records dispatches.
type recorder struct {
	events []string
	last   engine.Data
}

func (r *recorder) Name() string { return "recorder" }

func (r *recorder) OnLoad(reg *engine.Registry) error {
	for _, t := range []string{"dns_soa", "dns_slave", "dns_rr"} {
		for _, a := range []string{"_insert", "_update", "_delete"} {
			if err := reg.RegisterEvent(t+a, func(_ context.Context, event string, data engine.Data) error {
				r.events = append(r.events, event)
				r.last = data
				return nil
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

// TestModuleRaisesNamedEvents covers the dns-module-events spec: each
// dns table + datalog action i/u/d maps to its named event with the
// {old,new} payload intact.
func TestModuleRaisesNamedEvents(t *testing.T) {
	reg := engine.NewRegistry(nil)
	rec := &recorder{}
	require.NoError(t, reg.Load([]engine.Module{NewModule()}, []engine.Plugin{rec}))

	data := engine.Data{Old: map[string]any{"origin": "old.com."}, New: map[string]any{"origin": "example.com."}}
	ctx := context.Background()
	require.NoError(t, reg.RaiseTableHook(ctx, "dns_soa", "u", data))
	require.NoError(t, reg.RaiseTableHook(ctx, "dns_rr", "i", data))
	require.NoError(t, reg.RaiseTableHook(ctx, "dns_slave", "d", data))

	assert.Equal(t, []string{"dns_soa_update", "dns_rr_insert", "dns_slave_delete"}, rec.events)
	assert.Equal(t, "example.com.", rec.last.New["origin"])
	assert.Equal(t, "old.com.", rec.last.Old["origin"])
}

// TestUnannouncedEventRejected: subscribing to an event the dns module did
// not announce fails registry loading (foundation contract).
func TestUnannouncedEventRejected(t *testing.T) {
	reg := engine.NewRegistry(nil)
	require.NoError(t, NewModule().OnLoad(reg))
	err := reg.RegisterEvent("dns_bogus_event", func(context.Context, string, engine.Data) error { return nil })
	require.ErrorIs(t, err, engine.ErrEventNotAnnounced)
}
