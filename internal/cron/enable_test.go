package cron

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/engine"
)

func TestEnabled(t *testing.T) {
	tests := []struct {
		name      string
		webServer int8
		disabled  bool
		want      bool
	}{
		{name: "web server loads", webServer: 1, want: true},
		{name: "non-web server skips", webServer: 0, want: false},
		{name: "disabled in config skips", webServer: 1, disabled: true, want: false},
		{name: "non-web and disabled", webServer: 0, disabled: true, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Enabled(tt.webServer, tt.disabled))
		})
	}
}

// TestNonWebServerRegistersNoCronHooks covers cron-module-events:
// when the gate is closed, the module is not loaded and cron datalog rows
// produce no table-hook side effects (no announced events to subscribe to).
func TestNonWebServerRegistersNoCronHooks(t *testing.T) {
	reg := engine.NewRegistry(nil)
	var modules []engine.Module
	if Enabled(0, false) {
		modules = append(modules, NewModule())
	}
	require.NoError(t, reg.Load(modules, nil))

	// No hook registered: RaiseTableHook is a no-op (warn + nil).
	require.NoError(t, reg.RaiseTableHook(context.Background(), "cron", "i", engine.Data{
		New: map[string]any{"active": "y"},
	}))

	// Events were never announced, so plugins cannot subscribe.
	err := reg.RegisterEvent("cron_insert", func(context.Context, string, engine.Data) error { return nil })
	require.ErrorIs(t, err, engine.ErrEventNotAnnounced)
}

// TestDisabledConfigRegistersNoCronHooks covers config.toml enablement off
// even when web_server = 1.
func TestDisabledConfigRegistersNoCronHooks(t *testing.T) {
	reg := engine.NewRegistry(nil)
	var modules []engine.Module
	if Enabled(1, true) {
		modules = append(modules, NewModule())
	}
	require.NoError(t, reg.Load(modules, nil))

	err := reg.RegisterEvent("cron_update", func(context.Context, string, engine.Data) error { return nil })
	require.ErrorIs(t, err, engine.ErrEventNotAnnounced)
}
