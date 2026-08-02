package dns

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/engine"
)

// recordingExecutor captures service actions instead of calling systemctl.
type recordingExecutor struct{ runs [][2]string }

func (f *recordingExecutor) Run(_ context.Context, service, action string) error {
	f.runs = append(f.runs, [2]string{service, action})
	return nil
}

func TestBindExecutorResolvesUnit(t *testing.T) {
	tests := []struct {
		name     string
		hasBind9 bool
		want     string
	}{
		{"debian-style bind9 unit", true, "bind9"},
		{"redhat-style named unit", false, "named"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inner := &recordingExecutor{}
			exec := &BindExecutor{Inner: inner, UnitExists: func(unit string) bool {
				return tt.hasBind9 && unit == "bind9"
			}}
			require.NoError(t, exec.Run(context.Background(), BindService, engine.ActionReload))
			assert.Equal(t, [][2]string{{tt.want, "reload"}}, inner.runs)
		})
	}
}

func TestBindExecutorPassesThroughOtherServices(t *testing.T) {
	inner := &recordingExecutor{}
	exec := &BindExecutor{Inner: inner, UnitExists: func(string) bool {
		t.Fatal("unit resolution must not run for non-bind services")
		return false
	}}
	require.NoError(t, exec.Run(context.Background(), "httpd", engine.ActionRestart))
	assert.Equal(t, [][2]string{{"httpd", "restart"}}, inner.runs)
}

// TestRestartWinsOverReload covers the dns-module-events scenario: a later
// restart request upgrades a queued reload and exactly one restart runs.
func TestRestartWinsOverReload(t *testing.T) {
	inner := &recordingExecutor{}
	services := engine.NewServices(&BindExecutor{Inner: inner, UnitExists: func(string) bool { return true }}, nil)
	RegisterServices(services)

	services.RestartServiceDelayed(BindService, engine.ActionReload)
	services.RestartServiceDelayed(BindService, engine.ActionRestart)
	services.RestartServiceDelayed(BindService, engine.ActionReload)
	services.ProcessDelayedActions(context.Background())

	assert.Equal(t, [][2]string{{"bind9", "restart"}}, inner.runs)
}
