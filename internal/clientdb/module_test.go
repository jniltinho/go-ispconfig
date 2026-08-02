package clientdb

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/engine"
)

// recorder subscribes to every announced database event and records
// dispatches.
type recorder struct {
	events []string
	last   engine.Data
}

func (r *recorder) Name() string { return "recorder" }

func (r *recorder) OnLoad(reg *engine.Registry) error {
	for _, prefix := range []string{"database", "database_user"} {
		for _, a := range []string{"_insert", "_update", "_delete"} {
			if err := reg.RegisterEvent(prefix+a, func(_ context.Context, event string, data engine.Data) error {
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

// TestModuleRaisesNamedEvents covers the database-module-events spec: each
// hooked table + datalog action i/u/d maps to its named event with the
// {old,new} payload intact (web_database → database_*, web_database_user →
// database_user_*).
func TestModuleRaisesNamedEvents(t *testing.T) {
	reg := engine.NewRegistry(nil)
	rec := &recorder{}
	require.NoError(t, reg.Load([]engine.Module{NewModule()}, []engine.Plugin{rec}))

	data := engine.Data{
		Old: map[string]any{"database_name": "c1_old", "active": "y"},
		New: map[string]any{"database_name": "c1_app", "active": "y"},
	}
	ctx := context.Background()
	for _, action := range []string{"i", "u", "d"} {
		require.NoError(t, reg.RaiseTableHook(ctx, "web_database", action, data))
		require.NoError(t, reg.RaiseTableHook(ctx, "web_database_user", action, data))
	}

	assert.Equal(t, []string{
		"database_insert", "database_user_insert",
		"database_update", "database_user_update",
		"database_delete", "database_user_delete",
	}, rec.events)
	assert.Equal(t, "c1_app", rec.last.New["database_name"])
	assert.Equal(t, "c1_old", rec.last.Old["database_name"])
}

// TestUnannouncedEventRejected: subscribing to an event the database module
// did not announce fails registry loading (foundation contract).
func TestUnannouncedEventRejected(t *testing.T) {
	reg := engine.NewRegistry(nil)
	require.NoError(t, NewModule().OnLoad(reg))
	err := reg.RegisterEvent("database_bogus_event", func(context.Context, string, engine.Data) error { return nil })
	require.ErrorIs(t, err, engine.ErrEventNotAnnounced)
}

// TestModuleName identifies the module as "database".
func TestModuleName(t *testing.T) {
	assert.Equal(t, "database", NewModule().Name())
}
