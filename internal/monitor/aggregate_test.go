package monitor

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/model"
)

func TestAggregateServerState_foldsSeverityAndMessages(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	now := uint32(time.Now().Unix())

	fixtures := []model.MonitorData{
		{ServerID: 1, Type: "disk_usage", Created: now, Data: `{}`, State: "warning"},
		{ServerID: 1, Type: "server_load", Created: now, Data: `{"load_1":1}`, State: "ok"},
		{ServerID: 1, Type: "services", Created: now, Data: `{"webserver":0}`, State: "error"},
		{ServerID: 1, Type: "cpu_info", Created: now, Data: `{"cores":4}`, State: "no_state"},
		{ServerID: 1, Type: "os_info", Created: now, Data: `{"name":"Ubuntu","version":"24.04"}`, State: "no_state"},
		{ServerID: 1, Type: "ispc_info", Created: now, Data: `{"name":"go-ispconfig","version":"0.1"}`, State: "no_state"},
		// Older disk row must not win.
		{ServerID: 1, Type: "disk_usage", Created: now - 100, Data: `{}`, State: "ok"},
	}
	for _, r := range fixtures {
		require.NoError(t, db.Create(&r).Error)
	}

	st, err := AggregateServerState(ctx, db, 1, "srv1")
	require.NoError(t, err)
	assert.Equal(t, "error", st.State)
	assert.Equal(t, "Ubuntu", st.OSName)
	assert.Equal(t, "24.04", st.OSVersion)
	assert.Equal(t, "go-ispconfig", st.ISPCName)
	assert.NotEmpty(t, st.Messages)
	// services error + disk warning contribute messages; load ok too.
	assert.GreaterOrEqual(t, st.Counts["error"], 1)
	assert.GreaterOrEqual(t, st.Counts["warning"], 1)
}

func TestAggregateServerState_emptyUnknown(t *testing.T) {
	db := testDB(t)
	st, err := AggregateServerState(context.Background(), db, 9, "empty")
	require.NoError(t, err)
	assert.Equal(t, "unknown", st.State)
}

func TestAggregateSystemState(t *testing.T) {
	db := testDB(t)
	now := uint32(time.Now().Unix())
	require.NoError(t, db.Create(&model.MonitorData{
		ServerID: 1, Type: "server_load", Created: now, Data: `{}`, State: "ok",
	}).Error)
	out, err := AggregateSystemState(context.Background(), db, []model.Server{
		{ServerID: 1, ServerName: "a"},
		{ServerID: 2, ServerName: "b"},
	})
	require.NoError(t, err)
	require.Len(t, out, 2)
	assert.Equal(t, "ok", out[0].State)
	assert.Equal(t, "unknown", out[1].State)
}
