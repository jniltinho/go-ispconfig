package clientdb

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/engine"
)

// execRecorder records service actions the executor would run.
type execRecorder struct{ calls []string }

func (e *execRecorder) Run(_ context.Context, service, action string) error {
	e.calls = append(e.calls, service+":"+action)
	return nil
}

// TestNoServiceRegistered covers task 2.2 / design D2: loading the module
// registers no MySQL service, so a delayed restart request for mysql or
// mariadb is ignored and nothing ever reaches systemctl — privilege
// changes take effect via FLUSH PRIVILEGES inside the plugin.
func TestNoServiceRegistered(t *testing.T) {
	rec := &execRecorder{}
	services := engine.NewServices(rec, nil)
	reg := engine.NewRegistry(nil)
	require.NoError(t, reg.Load([]engine.Module{NewModule()}, nil))

	services.RestartServiceDelayed("mysql", engine.ActionRestart)
	services.RestartServiceDelayed("mariadb", engine.ActionReload)
	services.ProcessDelayedActions(context.Background())
	assert.Empty(t, rec.calls)
}
