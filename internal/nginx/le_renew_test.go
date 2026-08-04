package nginx

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

type stubRenewer struct{ n int }

func (s stubRenewer) RenewDue() (int, error) { return s.n, nil }

// TestRenewReloadsOnlyWhenRenewed: a renewal that changed a cert schedules a
// reload; an idle run does not.
func TestRenewReloadsOnlyWhenRenewed(t *testing.T) {
	run := func(renewed int) [][2]string {
		exec := &recordingExecutor{}
		services := engine.NewServices(exec, nil)
		require.NoError(t, web.RenewNative(context.Background(), stubRenewer{n: renewed}, services, nil, web.HttpdService))
		services.ProcessDelayedActions(context.Background())
		return exec.runs
	}

	assert.Equal(t, [][2]string{{"httpd", "reload"}}, run(1))
	assert.Empty(t, run(0))
}

// TestRegisterRenewal adds the daily job to a scheduler and refuses a
// duplicate.
func TestRegisterRenewal(t *testing.T) {
	p := NewPlugin(nil, nil, &renewRunner{}, "", nil)
	s := engine.NewScheduler(nil, nil)
	require.NoError(t, p.RegisterRenewal(s))
	assert.Error(t, p.RegisterRenewal(s), "same job registered twice must fail")
}
