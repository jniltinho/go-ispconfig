package monitor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiskFillState_bands(t *testing.T) {
	// free MiB as bytes: freeMiB * 1024 * 1024
	mib := func(n float64) uint64 { return uint64(n * 1024 * 1024) }

	tests := []struct {
		name    string
		pct     float64
		freeMiB float64
		want    string
	}{
		{"ok low fill", 50, 5000, "ok"},
		{"ok high free despite 80%", 82, 5000, "ok"},
		{"info 76% free under 2000", 76, 1500, "info"},
		{"warning 81% free under 1000", 81, 900, "warning"},
		{"critical 91% free under 500", 91, 400, "critical"},
		{"error 96% free under 100", 96, 50, "error"},
		{"critical not error when free 200", 96, 200, "critical"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, DiskFillState(tc.pct, mib(tc.freeMiB)))
		})
	}
}

func TestCollectDiskUsage_live(t *testing.T) {
	data, state, err := CollectDiskUsage(context.Background())
	require.NoError(t, err)
	assert.Contains(t, []string{"ok", "info", "warning", "critical", "error"}, state)
	// At least one mount on a normal Linux host.
	assert.NotEmpty(t, data)
}
