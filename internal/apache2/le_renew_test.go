package apache2

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/web"
)

type renewRunner struct{ calls [][]string }

func (r *renewRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, append([]string{name}, args...))
	return nil, nil
}

type recordingExecutor struct{ runs [][2]string }

func (e *recordingExecutor) Run(_ context.Context, service, action string) error {
	e.runs = append(e.runs, [2]string{service, action})
	return nil
}

type stubRenewer struct{ n int }

func (s stubRenewer) RenewDue() (int, error) { return s.n, nil }

// TestApacheRenewReloadsApache: an Apache-only server runs the native ACME
// renewal and reloads the Apache unit when a certificate actually changed.
func TestApacheRenewReloadsApache(t *testing.T) {
	exec := &recordingExecutor{}
	services := engine.NewServices(exec, nil)
	require.NoError(t, web.RenewNative(context.Background(), stubRenewer{n: 1}, services, nil, ServiceName))
	services.ProcessDelayedActions(context.Background())
	assert.Equal(t, [][2]string{{ServiceName, "reload"}}, exec.runs)
}

// TestApacheRegisterRenewal adds the daily job and refuses a duplicate.
func TestApacheRegisterRenewal(t *testing.T) {
	p := NewPlugin(nil, nil, &renewRunner{}, "", nil)
	s := engine.NewScheduler(nil, nil)
	require.NoError(t, p.RegisterRenewal(s))
	assert.Error(t, p.RegisterRenewal(s), "same job registered twice must fail")
}
