package monitor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAppendCapped(t *testing.T) {
	var s []int
	for i := 1; i <= 20; i++ {
		s = AppendCapped(s, i, 15)
	}
	assert.Len(t, s, 15)
	assert.Equal(t, 6, s[0])
	assert.Equal(t, 20, s[14])
}

func TestCollectSysUsage_capsSeries(t *testing.T) {
	ctx := context.Background()
	var prev *SysUsagePayload
	for i := 0; i < 18; i++ {
		p, err := CollectSysUsage(ctx, prev)
		require.NoError(t, err)
		// Force previous tstamp so interval math works.
		p.Tstamp = p.Tstamp - 1
		prev = p
	}
	assert.LessOrEqual(t, len(prev.Load), MaxSysUsagePoints)
	assert.LessOrEqual(t, len(prev.Mem), MaxSysUsagePoints)
	assert.LessOrEqual(t, len(prev.Time), MaxSysUsagePoints)
}

func TestRunSysUsageCollector(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	require.NoError(t, RunSysUsageCollector(ctx, db, 1))
	require.NoError(t, RunSysUsageCollector(ctx, db, 1))
	var n int64
	require.NoError(t, db.Table("monitor_data").Where("type = ?", "sys_usage").Count(&n).Error)
	assert.EqualValues(t, 1, n)
}
