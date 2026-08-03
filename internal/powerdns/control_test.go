package powerdns

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/engine"
)

// pathRunner returns canned output for known binaries.
type pathRunner struct {
	bins map[string]string // base name → version/output
	log  []string
}

func (r *pathRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	base := name
	if i := strings.LastIndex(name, "/"); i >= 0 {
		base = name[i+1:]
	}
	r.log = append(r.log, strings.Join(append([]string{name}, args...), " "))
	out, ok := r.bins[base]
	if !ok {
		return nil, fmt.Errorf("exec: %q: executable file not found", name)
	}
	return []byte(out), nil
}

func TestVersionSupported(t *testing.T) {
	assert.True(t, VersionSupported("4.9.1"))
	assert.True(t, VersionSupported("3.4.11"))
	assert.False(t, VersionSupported("2.9.22"))
	assert.False(t, VersionSupported(""))
	assert.False(t, VersionSupported("5.0.0"))
}

func TestControlWrappersStubbed(t *testing.T) {
	r := &pathRunner{bins: map[string]string{
		"pdns_control": "4.8.0",
		"pdnsutil":     "ok",
	}}
	p := NewPlugin(nil, nil, nil, r, 1, nil)
	// Discovery via version probe.
	_ = p.ensureTools(context.Background())
	// Force known tools after discovery attempts.
	p.SetToolsForTest("pdns_control", "pdnsutil", "4.8.0")
	r.log = nil

	p.doRediscover(context.Background())
	p.doNotify(context.Background(), engine.Data{New: map[string]any{"origin": "example.com."}})
	p.doRetrieve(context.Background(), engine.Data{New: map[string]any{"origin": "slave.com."}})
	p.doRectify(context.Background(), engine.Data{New: map[string]any{"origin": "example.com."}})

	joined := strings.Join(r.log, "\n")
	assert.Contains(t, joined, "pdns_control rediscover")
	assert.Contains(t, joined, "pdns_control notify example.com")
	assert.Contains(t, joined, "pdns_control retrieve slave.com")
	assert.Contains(t, joined, "pdnsutil rectify-zone example.com")
}

func TestControlMissingBinaryNonFatal(t *testing.T) {
	r := &pathRunner{bins: map[string]string{}}
	p := NewPlugin(nil, nil, nil, r, 1, nil)
	p.SetToolsForTest("", "", "")
	// Must not panic or error.
	p.doRediscover(context.Background())
	p.doNotify(context.Background(), engine.Data{New: map[string]any{"origin": "x.com."}})
	p.doRetrieve(context.Background(), engine.Data{New: map[string]any{"origin": "x.com."}})
	p.doRectify(context.Background(), engine.Data{New: map[string]any{"origin": "x.com."}})
	assert.Empty(t, r.log)
}

func TestFindBinaryPrefersExisting(t *testing.T) {
	r := &pathRunner{bins: map[string]string{"pdns_control": "4.0.0"}}
	got := findBinary(context.Background(), r, "pdns_control")
	require.NotEmpty(t, got)
	assert.Contains(t, got, "pdns_control")
}
