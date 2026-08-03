package monitor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadState_thresholds(t *testing.T) {
	tests := []struct {
		load float64
		want string
	}{
		{0, "ok"},
		{10, "ok"},
		{20.1, "info"},
		{50.1, "warning"},
		{55, "warning"},
		{100.1, "critical"},
		{150.1, "error"},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.want, LoadState(tc.load), "load=%v", tc.load)
	}
}

func TestCollectCPUInfo(t *testing.T) {
	data, state, err := CollectCPUInfo(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "no_state", state)
	require.NotEmpty(t, data)
	_, hasCores := data["cores"]
	assert.True(t, hasCores || data["output"] == "")
}

func TestCollectMemUsage(t *testing.T) {
	data, state, err := CollectMemUsage(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "no_state", state)
	assert.Contains(t, data, "MemTotal")
	assert.Contains(t, data, "MemAvailable")
	assert.Greater(t, data["MemTotal"].(int64), int64(0))
}

func TestCollectServerLoad(t *testing.T) {
	data, state, err := CollectServerLoad(context.Background())
	require.NoError(t, err)
	assert.Contains(t, []string{"ok", "info", "warning", "critical", "error"}, state)
	assert.Contains(t, data, "load_1")
	assert.Contains(t, data, "load_5")
	assert.Contains(t, data, "load_15")
	assert.Contains(t, data, "up_days")
}

func TestCollectOSInfo(t *testing.T) {
	data, state, err := CollectOSInfo(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "no_state", state)
	assert.NotEmpty(t, data["name"])
}

func TestCollectKernelInfo(t *testing.T) {
	data, state, err := CollectKernelInfo(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "no_state", state)
	assert.NotEmpty(t, data["kernel"])
}

func TestCollectISPCInfo(t *testing.T) {
	Version = "1.2.3-test"
	data, state, err := CollectISPCInfo(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "no_state", state)
	assert.Equal(t, "1.2.3-test", data["version"])
	assert.Equal(t, "go-ispconfig", data["name"])
}

func TestRunBasicCollectors_storesRows(t *testing.T) {
	db := testDB(t)
	require.NoError(t, RunBasicCollectors(context.Background(), db, 1))
	var n int64
	require.NoError(t, db.Table("monitor_data").Where("server_id = ?", 1).Count(&n).Error)
	assert.EqualValues(t, 6, n)
}
