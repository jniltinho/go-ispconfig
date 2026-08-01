package engine

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// mockExecutor records every Run call.
type mockExecutor struct {
	mu    sync.Mutex
	calls []string // "service/action"
}

func (m *mockExecutor) Run(_ context.Context, service, action string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, service+"/"+action)
	return nil
}

func (m *mockExecutor) Calls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.calls...)
}

func TestServicesDedupAndUpgrade(t *testing.T) {
	exec := &mockExecutor{}
	s := NewServices(exec, nil)
	s.Register("nginx")
	s.Register("bind9")

	t.Run("many reloads collapse into one", func(t *testing.T) {
		for range 10 {
			s.RestartServiceDelayed("bind9", ActionReload)
		}
		s.ProcessDelayedActions(context.Background())
		require.Equal(t, []string{"bind9/reload"}, exec.Calls())
	})

	t.Run("restart wins over reload in both orders", func(t *testing.T) {
		s.RestartServiceDelayed("nginx", ActionReload)
		s.RestartServiceDelayed("nginx", ActionRestart)
		s.RestartServiceDelayed("nginx", ActionReload) // must not downgrade
		s.ProcessDelayedActions(context.Background())
		require.Equal(t, []string{"bind9/reload", "nginx/restart"}, exec.Calls())
	})

	t.Run("queue is cleared after processing", func(t *testing.T) {
		s.ProcessDelayedActions(context.Background())
		require.Len(t, exec.Calls(), 2)
	})

	t.Run("unregistered service ignored", func(t *testing.T) {
		s.RestartServiceDelayed("postfix", ActionRestart)
		s.ProcessDelayedActions(context.Background())
		require.Len(t, exec.Calls(), 2)
	})
}
