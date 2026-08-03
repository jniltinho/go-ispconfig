package powerdns

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/engine"
)

// recordingExecutor captures Run calls.
type recordingExecutor struct {
	calls [][2]string
}

func (r *recordingExecutor) Run(_ context.Context, service, action string) error {
	r.calls = append(r.calls, [2]string{service, action})
	return nil
}

func TestExecutorUnitResolution(t *testing.T) {
	tests := []struct {
		name     string
		existing map[string]bool
		want     string
	}{
		{"powerdns unit exists", map[string]bool{"powerdns": true}, "powerdns"},
		{"fallback to pdns", map[string]bool{}, "pdns"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inner := &recordingExecutor{}
			e := &Executor{
				Inner:      inner,
				UnitExists: func(unit string) bool { return tt.existing[unit] },
				WriteFile:  func(string, []byte, os.FileMode) error { return nil },
			}
			require.NoError(t, e.Run(context.Background(), ServiceName, engine.ActionRestart))
			require.Len(t, inner.calls, 1)
			assert.Equal(t, tt.want, inner.calls[0][0])
			assert.Equal(t, engine.ActionRestart, inner.calls[0][1])
		})
	}
}

func TestExecutorPassthroughOtherServices(t *testing.T) {
	inner := &recordingExecutor{}
	e := &Executor{Inner: inner}
	require.NoError(t, e.Run(context.Background(), "bind", engine.ActionReload))
	require.Len(t, inner.calls, 1)
	assert.Equal(t, [2]string{"bind", engine.ActionReload}, inner.calls[0])
}

func TestExecutorRestartRewritesAXFR(t *testing.T) {
	inner := &recordingExecutor{}
	var gotPath string
	var gotData []byte
	e := &Executor{
		Inner:      inner,
		AXFRPath:   "/tmp/axfr.conf",
		UnitExists: func(string) bool { return false },
		WriteFile: func(path string, data []byte, _ os.FileMode) error {
			gotPath, gotData = path, data
			return nil
		},
	}
	require.NoError(t, e.Run(context.Background(), ServiceName, engine.ActionRestart))
	assert.Equal(t, "/tmp/axfr.conf", gotPath)
	// No panel DB: localhost only.
	assert.Equal(t, "allow-axfr-ips=127.0.0.1\n", string(gotData))
}

func TestExecutorReloadSkipsAXFRRewrite(t *testing.T) {
	inner := &recordingExecutor{}
	called := false
	e := &Executor{
		Inner:      inner,
		UnitExists: func(string) bool { return false },
		WriteFile: func(string, []byte, os.FileMode) error {
			called = true
			return nil
		},
	}
	require.NoError(t, e.Run(context.Background(), ServiceName, engine.ActionReload))
	assert.False(t, called)
}

func TestBuildAXFRLine(t *testing.T) {
	tests := []struct {
		name  string
		lists []string
		want  string
	}{
		{"empty", nil, "allow-axfr-ips=127.0.0.1"},
		{"single", []string{"10.0.0.1"}, "allow-axfr-ips=127.0.0.1,10.0.0.1"},
		{
			"merge dedup mixed separators",
			[]string{"10.0.0.1,10.0.0.2", "10.0.0.2 10.0.0.3", "127.0.0.1;10.0.0.1"},
			"allow-axfr-ips=127.0.0.1,10.0.0.1,10.0.0.2,10.0.0.3",
		},
		{"blank entries ignored", []string{" , ; "}, "allow-axfr-ips=127.0.0.1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, BuildAXFRLine(tt.lists...))
		})
	}
}

func TestRegisterServices(t *testing.T) {
	inner := &recordingExecutor{}
	s := engine.NewServices(inner, nil)
	RegisterServices(s)
	s.RestartServiceDelayed(ServiceName, engine.ActionRestart)
	s.ProcessDelayedActions(context.Background())
	require.Len(t, inner.calls, 1)
	assert.Equal(t, ServiceName, inner.calls[0][0])
}
