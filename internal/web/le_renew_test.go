package web

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/engine"
)

// renewRunner scripts client detection and the renew command output.
type renewRunner struct {
	which    map[string]string
	renewOut string
	calls    [][]string
}

func (r *renewRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, append([]string{name}, args...))
	if name == "which" {
		return []byte(r.which[args[0]]), nil
	}
	return []byte(r.renewOut), nil
}

// makeExecutable creates an executable stub so os.Stat sees the 0111 bit
// WhichExecutable checks.
func makeExecutable(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755))
}

// TestRenewReloadsTheGivenService: the same renewal body serves nginx and
// Apache, reloading whichever service key the caller passed.
func TestRenewReloadsTheGivenService(t *testing.T) {
	dir := t.TempDir()
	acme := filepath.Join(dir, "acme.sh")
	makeExecutable(t, acme)

	run := func(serviceKey, out string) [][2]string {
		r := &renewRunner{which: map[string]string{"acme.sh": acme}, renewOut: out}
		exec := &fakeExecutor{}
		services := engine.NewServices(exec, nil)
		require.NoError(t, RenewCertificates(context.Background(), r, services, nil, serviceKey))
		services.ProcessDelayedActions(context.Background())
		return exec.runs
	}

	assert.Equal(t, [][2]string{{HttpdService, "reload"}},
		run(HttpdService, "Cert success for example.com"))
	assert.Equal(t, [][2]string{{"apache2", "reload"}},
		run("apache2", "Cert success for example.com"),
		"an Apache server reloads its own unit after a renewal")
	assert.Empty(t, run("apache2", "Skipped, next renewal time is ..."),
		"an idle renew does not reload")
}

// TestRenewNoClientIsNoop: without a client the job is a clean no-op.
func TestRenewNoClientIsNoop(t *testing.T) {
	r := &renewRunner{which: map[string]string{}}
	assert.NoError(t, RenewCertificates(context.Background(), r, nil, nil, HttpdService))
}

// TestRenewalHappenedCertbot pins the certbot marker parsing.
func TestRenewalHappenedCertbot(t *testing.T) {
	assert.False(t, RenewalHappened(ACMECertbot, "No renewals were attempted."))
	assert.True(t, RenewalHappened(ACMECertbot, "Congratulations, all renewals succeeded"))
}

// TestDetectACMEPrefersAcmeSh: acme.sh wins over certbot, and a missing
// client yields ACMENone.
func TestDetectACMEPrefersAcmeSh(t *testing.T) {
	dir := t.TempDir()
	acme, certbot := filepath.Join(dir, "acme.sh"), filepath.Join(dir, "certbot")
	makeExecutable(t, acme)
	makeExecutable(t, certbot)

	kind, script := DetectACME(context.Background(),
		&renewRunner{which: map[string]string{"acme.sh": acme, "certbot": certbot}})
	assert.Equal(t, ACMEAcme, kind)
	assert.Equal(t, acme, script)

	kind, _ = DetectACME(context.Background(),
		&renewRunner{which: map[string]string{"certbot": certbot}})
	assert.Equal(t, ACMECertbot, kind)

	kind, _ = DetectACME(context.Background(), &renewRunner{which: map[string]string{}})
	assert.Equal(t, ACMENone, kind)
}
