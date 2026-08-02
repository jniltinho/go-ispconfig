package dns

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCAASupportedByVersion(t *testing.T) {
	tests := []struct {
		out  string
		want bool
	}{
		{"BIND 9.18.28-0ubuntu0.24.04.1-Ubuntu (Extended Support Version) <id:>", true},
		{"BIND 9.9.6", true},
		{"BIND 9.9.5-9+deb8u15-Debian", false},
		{"BIND 9.10.3-P4", true},
		{"BIND 9.8.1", false},
		{"BIND 10.0.0", true},
		{"garbage output", false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, caaSupportedByVersion(tt.out), "output %q", tt.out)
	}
}

// probeRunner answers `named -v` with a fixed output and counts calls.
type probeRunner struct {
	out   string
	err   error
	calls int
}

func (r *probeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.calls++
	return []byte(r.out), r.err
}

func TestBindCAASupportedProbedOncePerRun(t *testing.T) {
	runner := &probeRunner{out: "BIND 9.18.28-0ubuntu0.24.04.1-Ubuntu\n"}
	p := NewPlugin(nil, nil, runner, "", 1, nil)
	ctx := context.Background()

	assert.True(t, p.bindCAASupported(ctx))
	assert.True(t, p.bindCAASupported(ctx))
	assert.Equal(t, 1, runner.calls, "named -v probed once per daemon run")
}

func TestBindCAASupportedProbeFailure(t *testing.T) {
	runner := &probeRunner{err: errors.New("exec: named: not found")}
	p := NewPlugin(nil, nil, runner, "", 1, nil)
	assert.False(t, p.bindCAASupported(context.Background()), "missing named means no CAA support")
}
