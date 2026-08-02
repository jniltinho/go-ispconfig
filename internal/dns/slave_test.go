package dns

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEnsureSlaveDir covers the slave directory derivation and 0770
// ownership (bind-zone-generation spec: named must be able to write
// transferred zones).
func TestEnsureSlaveDir(t *testing.T) {
	tests := []struct {
		name    string
		prefix  string
		wantDir string // relative to the zonefiles dir
	}{
		{"prefix with file part", "slave/sec.", "slave"},
		{"prefix ending in slash", "slave/", "slave"},
		{"empty prefix falls back to zonefiles dir", "", "."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := t.TempDir()
			cfg := testDNSConfig(base)
			cfg.BindZonefilesSlavePfx = tt.prefix
			runner := &fakeRunner{}
			p := NewPlugin(nil, nil, runner, "", 1, nil)

			require.NoError(t, p.ensureSlaveDir(context.Background(), cfg))

			dir := filepath.Join(base, tt.wantDir)
			info, err := os.Stat(dir)
			require.NoError(t, err)
			assert.True(t, info.IsDir())
			assert.Equal(t, os.FileMode(0o770), info.Mode().Perm())
			assert.True(t, runner.has("chown", "bind:bind", dir))
		})
	}
}
