package dns

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/getconf"
)

// fakeRunner records commands and can fail selected command names.
type fakeRunner struct {
	calls   [][]string
	failCmd string // command name that fails when non-empty
	failOut string
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	if f.failCmd == name {
		return []byte(f.failOut), errors.New(name + " failed")
	}
	return nil, nil
}

// has reports whether a call starting with the given argv prefix was made.
func (f *fakeRunner) has(prefix ...string) bool {
	for _, c := range f.calls {
		if len(c) >= len(prefix) && strings.Join(c[:len(prefix)], " ") == strings.Join(prefix, " ") {
			return true
		}
	}
	return false
}

func testDNSConfig(base string) *getconf.DNSConfig {
	return &getconf.DNSConfig{
		BindUser:               "bind",
		BindGroup:              "bind",
		BindZonefilesDir:       base,
		BindKeyfilesDir:        base,
		BindZonefilesMasterPfx: "pri.",
		BindZonefilesSlavePfx:  "slave/sec.",
		NamedConfLocalPath:     filepath.Join(base, "named.conf.local"),
		DisableBindLog:         "n",
	}
}

func TestZoneFilePaths(t *testing.T) {
	cfg := testDNSConfig("/etc/bind")
	assert.Equal(t, "/etc/bind/pri.example.com", zoneFilePath(cfg, "example.com."))
	assert.Equal(t, "/etc/bind/slave/sec.example.com", slaveZoneFilePath(cfg, "example.com."))
	// Classless reverse delegation origins contain slashes.
	assert.Equal(t, "/etc/bind/pri.0_25.2.0.192.in-addr.arpa",
		zoneFilePath(cfg, "0/25.2.0.192.in-addr.arpa."))
}

func TestWriteZoneFile(t *testing.T) {
	base := t.TempDir()
	cfg := testDNSConfig(base)
	runner := &fakeRunner{}
	p := NewPlugin(nil, nil, runner, "", 1, nil)
	file := zoneFilePath(cfg, "example.com.")

	old, err := p.writeZoneFile(context.Background(), cfg, file, "zone v1\n")
	require.NoError(t, err)
	assert.Empty(t, old, "no previous content on first write")
	content, err := os.ReadFile(file)
	require.NoError(t, err)
	assert.Equal(t, "zone v1\n", string(content))
	assert.True(t, runner.has("chown", "bind:bind", file), "zone file chowned to bind user/group")

	old, err = p.writeZoneFile(context.Background(), cfg, file, "zone v2\n")
	require.NoError(t, err)
	assert.Equal(t, "zone v1\n", old, "previous content returned for rollback")
}

func TestWriteZoneFileChownFailure(t *testing.T) {
	base := t.TempDir()
	cfg := testDNSConfig(base)
	runner := &fakeRunner{failCmd: "chown"}
	p := NewPlugin(nil, nil, runner, "", 1, nil)
	file := zoneFilePath(cfg, "example.com.")
	require.NoError(t, os.WriteFile(file, []byte("previous\n"), 0o644))

	old, err := p.writeZoneFile(context.Background(), cfg, file, "zone\n")
	require.ErrorContains(t, err, "chown")
	assert.Equal(t, "previous\n", old,
		"chown failure after the write still returns the old content for rollback")
}

func TestContainedJoin(t *testing.T) {
	assert.Equal(t, "/etc/bind/pri.example.com", containedJoin("/etc/bind", "pri.example.com"))
	assert.Equal(t, "/etc/bind/_invalid_origin_rejected", containedJoin("/etc/bind", "../../etc/passwd"))
	assert.Equal(t, "/etc/bind/_invalid_origin_rejected", containedJoin("/etc/bind", "a/../../x"))
}
