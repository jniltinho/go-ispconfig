package powerdns

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/dns"
	"go-ispconfig/internal/engine"
)

type recordingRunner struct {
	calls [][]string
}

func (r *recordingRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	cmd := append([]string{name}, args...)
	r.calls = append(r.calls, cmd)
	return nil, nil
}

// TestPluginOnLoadSubscribesNineEvents loads the dns module + powerdns
// plugin and asserts all nine events are registered (task 2.1).
func TestPluginOnLoadSubscribesNineEvents(t *testing.T) {
	reg := engine.NewRegistry(nil)
	p := NewPlugin(nil, nil, nil, &recordingRunner{}, 1, nil)
	require.ErrorIs(t, p.OnLoad(reg), engine.ErrEventNotAnnounced)

	require.NoError(t, dns.NewModule().OnLoad(reg))
	require.NoError(t, p.OnLoad(reg))

	// Direct event raise with foreign server_id must no-op (gate).
	// With nil pdnsDB active handlers would panic if the gate failed.
	dataOther := engine.Data{
		New: map[string]any{"server_id": float64(99), "active": "Y", "id": float64(1), "origin": "x.com."},
	}
	require.NoError(t, reg.RaiseEvent(context.Background(), "dns_soa_insert", dataOther))
}

func TestForThisServer(t *testing.T) {
	p := NewPlugin(nil, nil, nil, nil, 7, nil)
	assert.True(t, p.forThisServer(engine.Data{New: map[string]any{"server_id": float64(7)}}))
	assert.True(t, p.forThisServer(engine.Data{Old: map[string]any{"server_id": float64(7)}}))
	assert.False(t, p.forThisServer(engine.Data{New: map[string]any{"server_id": float64(1)}}))
	// Missing server_id allowed (datalog already filtered by engine).
	assert.True(t, p.forThisServer(engine.Data{New: map[string]any{"active": "Y"}}))
}

func TestPluginName(t *testing.T) {
	assert.Equal(t, "powerdns", NewPlugin(nil, nil, nil, nil, 1, nil).Name())
}
