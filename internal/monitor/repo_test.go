package monitor

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/auth"
	"go-ispconfig/internal/model"
)

func TestHasMonitorModule(t *testing.T) {
	assert.False(t, HasMonitorModule(nil))
	assert.True(t, HasMonitorModule(&auth.SessionData{Typ: "admin"}))
	assert.True(t, HasMonitorModule(&auth.SessionData{Typ: "user", Modules: "dashboard,monitor,sites"}))
	assert.False(t, HasMonitorModule(&auth.SessionData{Typ: "user", Modules: "dashboard,sites"}))
}

func TestListData_latestOnly(t *testing.T) {
	db := testDB(t)
	now := uint32(time.Now().Unix())
	require.NoError(t, db.Create(&model.MonitorData{ServerID: 1, Type: "cpu_info", Created: now - 10, Data: `{"v":1}`, State: "no_state"}).Error)
	require.NoError(t, db.Create(&model.MonitorData{ServerID: 1, Type: "cpu_info", Created: now, Data: `{"v":2}`, State: "no_state"}).Error)
	require.NoError(t, db.Create(&model.MonitorData{ServerID: 1, Type: "mem_usage", Created: now, Data: `{}`, State: "no_state"}).Error)

	rows, err := ListData(context.Background(), db, DataFilter{
		ServerIDs:  []uint32{1},
		LatestOnly: true,
	})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	for _, r := range rows {
		if r.Type == "cpu_info" {
			assert.EqualValues(t, now, r.Created)
			assert.Contains(t, r.Data, `"v":2`)
		}
	}
}

func TestSysLogClear_setsZero(t *testing.T) {
	db := testDB(t)
	require.NoError(t, db.Exec(`
CREATE TABLE sys_log (
  syslog_id INTEGER PRIMARY KEY AUTOINCREMENT,
  server_id INTEGER NOT NULL DEFAULT 0,
  datalog_id INTEGER NOT NULL DEFAULT 0,
  loglevel INTEGER NOT NULL DEFAULT 0,
  tstamp INTEGER NOT NULL DEFAULT 0,
  message TEXT
)`).Error)
	row := model.SysLog{ServerID: 1, Loglevel: 2, Message: "boom", Tstamp: 100}
	require.NoError(t, db.Create(&row).Error)
	n, err := ClearSysLog(context.Background(), db, row.SyslogID)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)
	var got model.SysLog
	require.NoError(t, db.First(&got, row.SyslogID).Error)
	assert.EqualValues(t, 0, got.Loglevel)
	assert.Equal(t, "boom", got.Message) // not deleted
}

func TestListJobqueue_pending(t *testing.T) {
	db := testDB(t)
	require.NoError(t, db.Exec(`
CREATE TABLE sys_datalog (
  datalog_id INTEGER PRIMARY KEY AUTOINCREMENT,
  server_id INTEGER NOT NULL DEFAULT 0,
  dbtable TEXT,
  dbidx TEXT,
  action TEXT,
  tstamp INTEGER,
  user TEXT,
  data TEXT,
  status TEXT,
  error TEXT,
  session_id TEXT
)`).Error)
	// server.updated = 5 → pending ids > 5
	require.NoError(t, db.Create(&model.SysDatalog{DatalogID: 3, ServerID: 1, DBTable: "web_domain", Action: "i"}).Error)
	require.NoError(t, db.Create(&model.SysDatalog{DatalogID: 6, ServerID: 1, DBTable: "web_domain", Action: "u"}).Error)
	require.NoError(t, db.Create(&model.SysDatalog{DatalogID: 7, ServerID: 0, DBTable: "client", Action: "i"}).Error)

	rows, total, err := ListJobqueue(context.Background(), db, JobqueueFilter{
		Servers: []model.Server{{ServerID: 1, Updated: 5}},
	})
	require.NoError(t, err)
	assert.EqualValues(t, 2, total)
	require.Len(t, rows, 2)
	assert.EqualValues(t, 6, rows[0].DatalogID)
}

func TestListDatalogHistory_andDecode(t *testing.T) {
	db := testDB(t)
	require.NoError(t, db.Exec(`
CREATE TABLE sys_datalog (
  datalog_id INTEGER PRIMARY KEY AUTOINCREMENT,
  server_id INTEGER NOT NULL DEFAULT 0,
  dbtable TEXT,
  dbidx TEXT,
  action TEXT,
  tstamp INTEGER,
  user TEXT,
  data TEXT,
  status TEXT,
  error TEXT,
  session_id TEXT
)`).Error)
	payload := `{"old":{"x":1},"new":{"x":2}}`
	require.NoError(t, db.Create(&model.SysDatalog{
		ServerID: 1, DBTable: "web_domain", Action: "u", Data: payload, User: "admin", Tstamp: 100,
	}).Error)
	rows, total, err := ListDatalogHistory(context.Background(), db, DatalogHistoryFilter{
		Action: "u",
	})
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	diff, res := DecodeDatalogDiff(rows[0].Data)
	require.Empty(t, res.DecodeError)
	assert.EqualValues(t, 1, diff.Old["x"])
	assert.EqualValues(t, 2, diff.New["x"])
}
