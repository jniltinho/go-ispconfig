package dns

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidateZoneSuccess: a valid zone stays active and a stale .err
// quarantine file is removed before the check.
func TestValidateZoneSuccess(t *testing.T) {
	base := t.TempDir()
	cfg := testDNSConfig(base)
	runner := &fakeRunner{}
	p := NewPlugin(nil, nil, runner, "", 1, nil)
	file := zoneFilePath(cfg, "example.com.")
	require.NoError(t, os.WriteFile(file, []byte("good zone\n"), 0o644))
	require.NoError(t, os.WriteFile(file+".err", []byte("stale\n"), 0o644))

	require.NoError(t, p.validateZone(context.Background(), cfg, "example.com.", file, ""))

	assert.True(t, runner.has("named-checkzone", "example.com.", file))
	assert.NoFileExists(t, file+".err", "stale quarantine removed")
	assert.FileExists(t, file)
}

// TestValidateZoneRollback covers "Invalid zone is rolled back": previous
// content restored with ownership, invalid render kept as .err, checker
// output surfaced as the returned (datalog) error.
func TestValidateZoneRollback(t *testing.T) {
	base := t.TempDir()
	cfg := testDNSConfig(base)
	runner := &fakeRunner{failCmd: "named-checkzone", failOut: "dns_rr_load: bad owner name"}
	p := NewPlugin(nil, nil, runner, "", 1, nil)
	file := zoneFilePath(cfg, "example.com.")
	require.NoError(t, os.WriteFile(file, []byte("bad zone\n"), 0o644))

	err := p.validateZone(context.Background(), cfg, "example.com.", file, "previous zone\n")
	require.ErrorContains(t, err, "named-checkzone failed")
	require.ErrorContains(t, err, "bad owner name", "checker output travels in the datalog error")

	quarantined, readErr := os.ReadFile(file + ".err")
	require.NoError(t, readErr)
	assert.Equal(t, "bad zone\n", string(quarantined))
	restored, readErr := os.ReadFile(file)
	require.NoError(t, readErr)
	assert.Equal(t, "previous zone\n", string(restored))
	assert.True(t, runner.has("chown", "bind:bind", file), "restored file re-chowned")
}

// TestValidateZoneFirstRenderQuarantineOnly covers "Invalid first render
// leaves only quarantine": no active zone file remains.
func TestValidateZoneFirstRenderQuarantineOnly(t *testing.T) {
	base := t.TempDir()
	cfg := testDNSConfig(base)
	runner := &fakeRunner{failCmd: "named-checkzone", failOut: "not loaded"}
	p := NewPlugin(nil, nil, runner, "", 1, nil)
	file := zoneFilePath(cfg, "example.com.")
	require.NoError(t, os.WriteFile(file, []byte("bad zone\n"), 0o644))

	err := p.validateZone(context.Background(), cfg, "example.com.", file, "")
	require.Error(t, err)
	assert.FileExists(t, file+".err")
	assert.NoFileExists(t, file, "no active zone file after a failed first render")
}
