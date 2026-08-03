package fail2ban

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/engine"
)

// fakeRunner records the commands the plugin would run.
type fakeRunner struct{ calls [][]string }

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	return nil, nil
}

// serverRow is a datalog payload carrying a server.config INI.
func serverRow(serverType string) engine.Data {
	return engine.Data{New: map[string]any{
		"server_id": 1,
		"config":    "[web]\nserver_type=" + serverType + "\n",
	}}
}

// TestPluginReappliesOnServerConfigChange: an admin switching [web]
// server_type re-renders the jails (orphan pruned, new one written) and
// reloads fail2ban, while a config change that keeps the web server does
// nothing at all.
func TestPluginReappliesOnServerConfigChange(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "jail.d")
	runner := &fakeRunner{}
	p := NewPlugin(runner, nil)
	p.dir = dir

	reg := engine.NewRegistry(nil)
	require.NoError(t, reg.Load(nil, []engine.Plugin{p}))

	ctx := context.Background()
	require.NoError(t, reg.RaiseTableHook(ctx, "server", "u", serverRow("nginx")))
	require.FileExists(t, filepath.Join(dir, "ispconfig-nginx-http-auth.local"))
	assert.Len(t, runner.calls, 1)

	require.NoError(t, reg.RaiseTableHook(ctx, "server", "u", serverRow("apache")))
	assert.NoFileExists(t, filepath.Join(dir, "ispconfig-nginx-http-auth.local"))
	assert.FileExists(t, filepath.Join(dir, "ispconfig-apache-auth.local"))
	require.Len(t, runner.calls, 2)
	assert.Equal(t, []string{"fail2ban-client", "reload"}, runner.calls[1])

	// Unchanged web server: nothing rendered, no reload.
	require.NoError(t, reg.RaiseTableHook(ctx, "server", "u", serverRow("apache")))
	assert.Len(t, runner.calls, 2)
}
