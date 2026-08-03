package apache2

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/engine"
)

// renewRunner scripts client detection and the renew output.
type renewRunner struct {
	which    map[string]string
	renewOut string
}

func (r *renewRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	if name == "which" {
		return []byte(r.which[args[0]]), nil
	}
	return []byte(r.renewOut), nil
}

// recordingExecutor records the service actions the scheduler triggers.
type recordingExecutor struct{ runs [][2]string }

func (e *recordingExecutor) Run(_ context.Context, service, action string) error {
	e.runs = append(e.runs, [2]string{service, action})
	return nil
}

// TestApacheRenewReloadsApache: an Apache-only server runs the ACME renewal
// itself (no nginx plugin exists to do it) and reloads the Apache unit when a
// certificate actually changed.
func TestApacheRenewReloadsApache(t *testing.T) {
	dir := t.TempDir()
	acme := filepath.Join(dir, "acme.sh")
	require.NoError(t, os.WriteFile(acme, []byte("#!/bin/sh\n"), 0o755))

	exec := &recordingExecutor{}
	services := engine.NewServices(exec, nil)
	r := &renewRunner{which: map[string]string{"acme.sh": acme}, renewOut: "Cert success for example.com"}
	p := NewPlugin(nil, services, r, "", nil)

	require.NoError(t, p.renewCertificates(context.Background()))
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
