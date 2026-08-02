package firewall

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/engine"
)

// TestPluginWiresThreeEvents loads module+plugin and drives the table
// hook so each datalog action reaches the UFW plugin handlers.
func TestPluginWiresThreeEvents(t *testing.T) {
	r := &recordingRunner{}
	reg := engine.NewRegistry(nil)
	p := testPlugin(r, 1)
	require.NoError(t, reg.Load([]engine.Module{NewModule()}, []engine.Plugin{p}))

	ctx := context.Background()
	// insert
	require.NoError(t, reg.RaiseTableHook(ctx, "firewall", "i", fwData(1, "", "22", "", "", "", "y")))
	assert.Contains(t, r.joined(), "ufw --force enable")
	assert.Contains(t, r.joined(), "ufw --force reset")

	// update (no second reset)
	r.calls = nil
	require.NoError(t, reg.RaiseTableHook(ctx, "firewall", "u", fwData(1, "22", "22,80", "", "", "y", "y")))
	assert.Contains(t, r.joined(), "ufw allow 80/tcp")
	assert.Contains(t, r.joined(), "ufw reload")
	assert.False(t, r.hasPrefix("ufw --force reset"))

	// delete
	r.calls = nil
	require.NoError(t, reg.RaiseTableHook(ctx, "firewall", "d", engine.Data{
		Old: map[string]any{"server_id": float64(1), "tcp_port": "22", "active": "y"},
		New: map[string]any{},
	}))
	got := r.joined()
	assert.Equal(t, []string{
		"ufw --version",
		"ufw --force reset",
		"ufw disable",
	}, got)
}

func TestUFWDeleteForceResetAndDisable(t *testing.T) {
	r := &recordingRunner{}
	p := testPlugin(r, 1)
	data := engine.Data{
		Old: map[string]any{"server_id": float64(1), "tcp_port": "22,80", "active": "y"},
	}
	require.NoError(t, p.ufwDelete(context.Background(), "firewall_delete", data))
	assert.Equal(t, []string{
		"ufw --version",
		"ufw --force reset",
		"ufw disable",
	}, r.joined())
}

func TestUFWDeleteSkipNonLocal(t *testing.T) {
	r := &recordingRunner{}
	p := testPlugin(r, 1)
	data := engine.Data{
		Old: map[string]any{"server_id": float64(9), "tcp_port": "22", "active": "y"},
	}
	require.NoError(t, p.ufwDelete(context.Background(), "firewall_delete", data))
	assert.Empty(t, r.calls)
}

func TestUFWDeleteMissingSkips(t *testing.T) {
	r := &recordingRunner{missing: true}
	p := testPlugin(r, 1)
	data := engine.Data{
		Old: map[string]any{"server_id": float64(1), "tcp_port": "22", "active": "y"},
	}
	require.NoError(t, p.ufwDelete(context.Background(), "firewall_delete", data))
	assert.Equal(t, []string{"ufw --version"}, r.joined())
}

func TestPluginName(t *testing.T) {
	assert.Equal(t, "ufw", NewPlugin(&recordingRunner{}, 1, 0, nil).Name())
}
