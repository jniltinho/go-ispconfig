//go:build integration

package database

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/model"
)

// TestMonitorDataModelRoundTrip inserts a realistic monitor_data row into a
// migrated MariaDB schema and reads it back (add-monitor-module task 1.1).
// Composite primary key (server_id, type, created) and state enum default
// must survive the round trip.
func TestMonitorDataModelRoundTrip(t *testing.T) {
	dsnPrefix, container := StartMariaDB(t, "monrt")
	MariaDBExec(t, container, "CREATE DATABASE monrt CHARACTER SET utf8mb4")
	db, err := Open(dsnPrefix + "/monrt?charset=utf8mb4&parseTime=True&loc=Local")
	require.NoError(t, err)
	needSeed, err := Migrate(db)
	require.NoError(t, err)
	require.True(t, needSeed)

	row := model.MonitorData{
		ServerID: 1,
		Type:     "cpu_info",
		Created:  1700000000,
		Data:     `{"model":"Intel","cores":4}`,
		State:    model.MonitorStateNoState,
	}
	require.NoError(t, db.Create(&row).Error)

	var got model.MonitorData
	require.NoError(t, db.Where(
		"server_id = ? AND type = ? AND created = ?",
		row.ServerID, row.Type, row.Created,
	).Take(&got).Error)

	assert.EqualValues(t, 1, got.ServerID)
	assert.Equal(t, "cpu_info", got.Type)
	assert.EqualValues(t, 1700000000, got.Created)
	assert.Equal(t, `{"model":"Intel","cores":4}`, got.Data)
	assert.Equal(t, model.MonitorStateNoState, got.State)

	// Default state when omitted.
	row2 := model.MonitorData{
		ServerID: 1,
		Type:     "mem_usage",
		Created:  1700000001,
		Data:     `{}`,
	}
	require.NoError(t, db.Create(&row2).Error)
	var got2 model.MonitorData
	require.NoError(t, db.Where(
		"server_id = ? AND type = ? AND created = ?",
		row2.ServerID, row2.Type, row2.Created,
	).Take(&got2).Error)
	assert.Equal(t, model.MonitorStateUnknown, got2.State)
}
