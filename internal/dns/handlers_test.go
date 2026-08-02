package dns

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/engine"
)

// TestBindAction covers design D5: update_acl forces a restart, anything
// else reloads.
func TestBindAction(t *testing.T) {
	assert.Equal(t, engine.ActionRestart, bindAction(row{"update_acl": "1.2.3.4"}))
	assert.Equal(t, engine.ActionReload, bindAction(row{"update_acl": ""}))
	assert.Equal(t, engine.ActionReload, bindAction(row{}))
}

// TestCleanupZoneFiles: the zone file and its .err/.signed variants are
// removed; missing files are not an error.
func TestCleanupZoneFiles(t *testing.T) {
	base := t.TempDir()
	cfg := testDNSConfig(base)
	p := NewPlugin(nil, nil, &fakeRunner{}, "", 1, nil)
	file := zoneFilePath(cfg, "old.com.")
	require.NoError(t, os.WriteFile(file, []byte("zone"), 0o644))
	require.NoError(t, os.WriteFile(file+".err", []byte("bad"), 0o644))
	require.NoError(t, os.WriteFile(file+".signed", []byte("signed"), 0o644))

	p.cleanupZoneFiles(file)
	assert.NoFileExists(t, file)
	assert.NoFileExists(t, file+".err")
	assert.NoFileExists(t, file+".signed")

	p.cleanupZoneFiles(file) // second run on missing files is a no-op
}

// TestOnLoadRejectsWithoutModule: the plugin can only load after the dns
// module announced its events.
func TestOnLoadRejectsWithoutModule(t *testing.T) {
	reg := engine.NewRegistry(nil)
	p := NewPlugin(nil, nil, &fakeRunner{}, "", 1, nil)
	require.ErrorIs(t, p.OnLoad(reg), engine.ErrEventNotAnnounced)

	require.NoError(t, NewModule().OnLoad(reg))
	require.NoError(t, p.OnLoad(reg))
}
