package cron

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/engine"
)

// recorder subscribes to every announced cron event and records dispatches.
type recorder struct {
	events []string
	last   engine.Data
}

func (r *recorder) Name() string { return "recorder" }

func (r *recorder) OnLoad(reg *engine.Registry) error {
	for _, a := range []string{"_insert", "_update", "_delete"} {
		if err := reg.RegisterEvent("cron"+a, func(_ context.Context, event string, data engine.Data) error {
			r.events = append(r.events, event)
			r.last = data
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

// TestModuleRaisesNamedEvents covers the cron-module-events spec: each cron
// table + datalog action i/u/d maps to its named event with the {old,new}
// payload intact.
func TestModuleRaisesNamedEvents(t *testing.T) {
	reg := engine.NewRegistry(nil)
	rec := &recorder{}
	require.NoError(t, reg.Load([]engine.Module{NewModule()}, []engine.Plugin{rec}))

	data := engine.Data{
		Old: map[string]any{"active": "n"},
		New: map[string]any{"active": "y"},
	}
	ctx := context.Background()
	require.NoError(t, reg.RaiseTableHook(ctx, "cron", "i", data))
	require.NoError(t, reg.RaiseTableHook(ctx, "cron", "u", data))
	require.NoError(t, reg.RaiseTableHook(ctx, "cron", "d", data))

	assert.Equal(t, []string{"cron_insert", "cron_update", "cron_delete"}, rec.events)
	assert.Equal(t, "n", rec.last.Old["active"])
	assert.Equal(t, "y", rec.last.New["active"])
}

// TestUnannouncedEventRejected: subscribing to an event the cron module
// did not announce fails (foundation registry contract).
func TestUnannouncedEventRejected(t *testing.T) {
	reg := engine.NewRegistry(nil)
	require.NoError(t, NewModule().OnLoad(reg))
	err := reg.RegisterEvent("cron_bogus", func(context.Context, string, engine.Data) error { return nil })
	require.ErrorIs(t, err, engine.ErrEventNotAnnounced)
}

// TestModuleName identifies the module as "cron".
func TestModuleName(t *testing.T) {
	assert.Equal(t, "cron", NewModule().Name())
}
