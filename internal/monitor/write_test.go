package monitor

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"go-ispconfig/internal/model"
)

func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	// Unique DSN per test — :memory: with shared cache collides across tests.
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	// sqlite has no mediumtext/enum; approximate schema for unit tests.
	require.NoError(t, db.Exec(`
CREATE TABLE monitor_data (
  server_id INTEGER NOT NULL DEFAULT 0,
  type TEXT NOT NULL DEFAULT '',
  created INTEGER NOT NULL DEFAULT 0,
  data TEXT,
  state TEXT NOT NULL DEFAULT 'unknown',
  PRIMARY KEY (server_id, type, created)
)`).Error)
	return db
}

func TestStore_JSONAndPrune(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	now := uint32(time.Now().Unix())

	// Old row outside prune window.
	old := model.MonitorData{
		ServerID: 1, Type: "cpu_info", Created: now - 400,
		Data: `{"old":true}`, State: "no_state",
	}
	require.NoError(t, db.Create(&old).Error)
	// Other server must not be pruned.
	other := model.MonitorData{
		ServerID: 2, Type: "cpu_info", Created: now - 400,
		Data: `{"other":true}`, State: "no_state",
	}
	require.NoError(t, db.Create(&other).Error)

	require.NoError(t, Store(ctx, db, 1, "cpu_info", map[string]any{"cores": 4}, "no_state", now))

	var rows []model.MonitorData
	require.NoError(t, db.Where("type = ?", "cpu_info").Order("server_id, created").Find(&rows).Error)
	require.Len(t, rows, 2)
	assert.EqualValues(t, 1, rows[0].ServerID)
	assert.EqualValues(t, now, rows[0].Created)
	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(rows[0].Data), &payload))
	assert.EqualValues(t, 4, payload["cores"])
	assert.EqualValues(t, 2, rows[1].ServerID)
	assert.Contains(t, rows[1].Data, "other")
}

func TestStore_rejectsEmptyType(t *testing.T) {
	db := testDB(t)
	err := Store(context.Background(), db, 1, "", map[string]any{}, "ok", 0)
	require.Error(t, err)
}

func TestUpsertType_updatesInPlace(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	require.NoError(t, UpsertType(ctx, db, 1, "sys_usage", map[string]any{"load": []any{1.0}}, "ok"))
	require.NoError(t, UpsertType(ctx, db, 1, "sys_usage", map[string]any{"load": []any{1.0, 2.0}}, "ok"))

	var n int64
	require.NoError(t, db.Model(&model.MonitorData{}).Where("type = ?", "sys_usage").Count(&n).Error)
	assert.EqualValues(t, 1, n)

	var row model.MonitorData
	require.NoError(t, db.Where("type = ?", "sys_usage").First(&row).Error)
	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(row.Data), &payload))
	load, ok := payload["load"].([]any)
	require.True(t, ok)
	assert.Len(t, load, 2)
}
