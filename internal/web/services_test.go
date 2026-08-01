package web

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/engine"
)

// fakeRunner records commands and returns scripted results per command name.
type fakeRunner struct {
	calls  [][]string
	fail   map[string]error
	output []byte
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	if err := f.fail[name]; err != nil {
		return f.output, err
	}
	return f.output, nil
}

// fakeExecutor records executed service actions.
type fakeExecutor struct {
	runs [][2]string
}

func (f *fakeExecutor) Run(_ context.Context, service, action string) error {
	f.runs = append(f.runs, [2]string{service, action})
	return nil
}

// TestGuardedExecutorReloadsAfterConfigTest covers "Reload with valid
// configuration": nginx -t exits 0, then the nginx unit is reloaded.
func TestGuardedExecutorReloadsAfterConfigTest(t *testing.T) {
	runner := &fakeRunner{}
	inner := &fakeExecutor{}
	exec := GuardedExecutor{Inner: inner, Runner: runner}

	require.NoError(t, exec.Run(context.Background(), HttpdService, engine.ActionReload))
	assert.Equal(t, [][]string{{"nginx", "-t"}}, runner.calls)
	assert.Equal(t, [][2]string{{"nginx", "reload"}}, inner.runs)
}

// TestGuardedExecutorAbortsOnBrokenConfig covers "Reload blocked by broken
// configuration": nginx -t fails, no reload runs and the nginx output is in
// the error.
func TestGuardedExecutorAbortsOnBrokenConfig(t *testing.T) {
	runner := &fakeRunner{
		fail:   map[string]error{"nginx": errors.New("exit status 1")},
		output: []byte(`nginx: [emerg] unknown directive "bogus"`),
	}
	inner := &fakeExecutor{}
	exec := GuardedExecutor{Inner: inner, Runner: runner}

	err := exec.Run(context.Background(), HttpdService, engine.ActionRestart)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown directive")
	assert.Empty(t, inner.runs, "nginx must not be restarted with a broken config")
}

// TestGuardedExecutorPassesThroughOtherServices: php-fpm units run without
// the nginx -t guard.
func TestGuardedExecutorPassesThroughOtherServices(t *testing.T) {
	runner := &fakeRunner{}
	inner := &fakeExecutor{}
	exec := GuardedExecutor{Inner: inner, Runner: runner}

	require.NoError(t, exec.Run(context.Background(), "php8.3-fpm", engine.ActionReload))
	assert.Empty(t, runner.calls, "no nginx -t for FPM services")
	assert.Equal(t, [][2]string{{"php8.3-fpm", "reload"}}, inner.runs)
}

// TestPerVersionFpmDedup covers "Only the affected FPM version is reloaded":
// two delayed reload requests for php8.3-fpm collapse into one execution and
// php8.2-fpm is never touched.
func TestPerVersionFpmDedup(t *testing.T) {
	runner := &fakeRunner{}
	inner := &fakeExecutor{}
	services := engine.NewServices(GuardedExecutor{Inner: inner, Runner: runner}, nil)
	RegisterServices(services, "php8.3-fpm", "php8.2-fpm", "")

	services.RestartServiceDelayed("php8.3-fpm", engine.ActionReload)
	services.RestartServiceDelayed("php8.3-fpm", engine.ActionReload)
	services.ProcessDelayedActions(context.Background())

	assert.Equal(t, [][2]string{{"php8.3-fpm", "reload"}}, inner.runs)
}

// TestHttpdDelayedFlushRunsGuard: a delayed httpd reload flushed at cycle end
// goes through the nginx -t guard.
func TestHttpdDelayedFlushRunsGuard(t *testing.T) {
	runner := &fakeRunner{}
	inner := &fakeExecutor{}
	services := engine.NewServices(GuardedExecutor{Inner: inner, Runner: runner}, nil)
	RegisterServices(services)

	services.RestartServiceDelayed(HttpdService, engine.ActionReload)
	services.ProcessDelayedActions(context.Background())

	assert.Equal(t, [][]string{{"nginx", "-t"}}, runner.calls)
	assert.Equal(t, [][2]string{{"nginx", "reload"}}, inner.runs)
}
