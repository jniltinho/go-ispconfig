package nginx

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/engine"
)

// renewRunner scripts detection and the renew command output.
type renewRunner struct {
	which     map[string]string
	renewOut  string
	renewFail bool
	calls     [][]string
}

func (r *renewRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, append([]string{name}, args...))
	if name == "which" {
		return []byte(r.which[args[0]]), nil
	}
	if len(args) > 0 && args[0] == "--version" {
		return []byte("v3.0.0"), nil
	}
	if r.renewFail {
		return []byte("failure"), assertErr("renew failed")
	}
	return []byte(r.renewOut), nil
}

// TestRenewReloadsOnlyWhenRenewed: a renewal that changed a cert schedules a
// reload; an idle run does not.
func TestRenewReloadsOnlyWhenRenewed(t *testing.T) {
	dir := t.TempDir()
	acme := filepath.Join(dir, "acme.sh")
	makeExecutable(t, acme)

	run := func(out string) [][2]string {
		r := &renewRunner{which: map[string]string{"acme.sh": acme}, renewOut: out}
		exec := &recordingExecutor{}
		services := engine.NewServices(exec, nil)
		p := NewPlugin(nil, services, r, "", nil)
		p.leWebroot = dir
		require.NoError(t, p.renewCertificates(context.Background()))
		services.ProcessDelayedActions(context.Background())
		return exec.runs
	}

	// The plugin schedules the "httpd" service key; the GuardedExecutor maps
	// it to the nginx unit in production.
	assert.Equal(t, [][2]string{{"httpd", "reload"}}, run("Cert success for example.com"),
		"a renewal schedules a reload")
	assert.Empty(t, run("Skipped, next renewal time is ..."), "an idle renew does not reload")
}

// TestRenewNoClientIsNoop: without a client the job is a clean no-op.
func TestRenewNoClientIsNoop(t *testing.T) {
	r := &renewRunner{which: map[string]string{}}
	p := NewPlugin(nil, nil, r, "", nil)
	assert.NoError(t, p.renewCertificates(context.Background()))
}

// The renew body itself now lives in internal/web (shared with Apache); its
// output-marker parsing is pinned by web/le_renew_test.go.

// TestRegisterRenewal adds the daily job to a scheduler and refuses a
// duplicate.
func TestRegisterRenewal(t *testing.T) {
	p := NewPlugin(nil, nil, newFakeRunner(), "", nil)
	s := engine.NewScheduler(nil, nil)
	require.NoError(t, p.RegisterRenewal(s))
	assert.Error(t, p.RegisterRenewal(s), "same job registered twice must fail")
}
