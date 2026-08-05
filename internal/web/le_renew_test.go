package web

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/engine"
)

type renewFakeExecutor struct{ runs [][2]string }

func (e *renewFakeExecutor) Run(_ context.Context, service, action string) error {
	e.runs = append(e.runs, [2]string{service, action})
	return nil
}

type fakeRenewer struct {
	n   int
	err error
}

func (f fakeRenewer) RenewDue() (int, error) { return f.n, f.err }

// TestRenewNativeReloadsTheGivenService: the same renewal body serves nginx
// and Apache, reloading whichever service key the caller passed.
func TestRenewNativeReloadsTheGivenService(t *testing.T) {
	run := func(serviceKey string, renewed int) [][2]string {
		exec := &renewFakeExecutor{}
		services := engine.NewServices(exec, nil)
		require.NoError(t, RenewNative(context.Background(), fakeRenewer{n: renewed}, services, nil, serviceKey))
		services.ProcessDelayedActions(context.Background())
		return exec.runs
	}

	assert.Equal(t, [][2]string{{HttpdService, "reload"}}, run(HttpdService, 1))
	assert.Equal(t, [][2]string{{"apache2", "reload"}}, run("apache2", 2))
	assert.Empty(t, run("apache2", 0), "an idle renew does not reload")
}

// TestRenewNativeNoManagerIsNoop: without a manager the job is a clean no-op.
func TestRenewNativeNoManagerIsNoop(t *testing.T) {
	assert.NoError(t, RenewNative(context.Background(), nil, nil, nil, HttpdService))
}
