package monitor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm/logger"

	"go-ispconfig/internal/engine"
)

func TestRegisterJobs_persistsSpecsAndRunsCPU(t *testing.T) {
	db := testDB(t)
	require.NoError(t, db.Exec(`
CREATE TABLE sys_config (
  "group" TEXT NOT NULL,
  name TEXT NOT NULL,
  value TEXT,
  PRIMARY KEY ("group", name)
)`).Error)
	require.NoError(t, db.Exec(`
CREATE TABLE server (
  server_id INTEGER PRIMARY KEY,
  web_server INTEGER DEFAULT 0,
  file_server INTEGER DEFAULT 0,
  mail_server INTEGER DEFAULT 0,
  dns_server INTEGER DEFAULT 0,
  db_server INTEGER DEFAULT 0,
  active INTEGER DEFAULT 1,
  mirror_server_id INTEGER DEFAULT 0,
  server_name TEXT DEFAULT '',
  updated INTEGER DEFAULT 0
)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO server (server_id, server_name, active) VALUES (1, 'local', 1)`).Error)

	// Need sys_log for monitor_sys_log job if we ever run it; create empty.
	require.NoError(t, db.Exec(`
CREATE TABLE sys_log (
  syslog_id INTEGER PRIMARY KEY AUTOINCREMENT,
  server_id INTEGER NOT NULL DEFAULT 0,
  datalog_id INTEGER NOT NULL DEFAULT 0,
  loglevel INTEGER NOT NULL DEFAULT 0,
  tstamp INTEGER NOT NULL DEFAULT 0,
  message TEXT
)`).Error)

	sched := engine.NewScheduler(db, nil)
	// Silence gorm noise from setConfig when keys missing.
	_ = logger.Default

	err := RegisterJobs(sched, db, RegisterOptions{
		ServerID:           1,
		Version:            "test-1",
		EnableSystemUpdate: true,
		LogPaths: LogPaths{
			ISPConfig:   "/tmp/goisp-reg-isp.log",
			LetsEncrypt: "/tmp/goisp-reg-le.log",
			Messages:    "/tmp/goisp-reg-msg.log",
		},
		Prober: fakeProber{},
	})
	require.NoError(t, err)

	jobs := sched.Jobs(context.Background())
	assert.GreaterOrEqual(t, len(jobs), 14)

	var specs int64
	require.NoError(t, db.Table("sys_config").
		Where("`group` = ? AND name LIKE ?", "scheduler", "%_spec").
		Count(&specs).Error)
	assert.GreaterOrEqual(t, specs, int64(14))

	require.NoError(t, sched.RunJob(context.Background(), "monitor_cpu_info"))
	var n int64
	require.NoError(t, db.Table("monitor_data").Where("type = ?", "cpu_info").Count(&n).Error)
	assert.EqualValues(t, 1, n)
}
