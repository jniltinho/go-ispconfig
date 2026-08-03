package monitor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/model"
)

func TestTailFile_lastLines(t *testing.T) {
	dir := t.TempDir()
	// validateLogPath only allows /var/log or /tmp — use /tmp via TempDir which is under /tmp on Linux.
	// t.TempDir is often /tmp/Test... so rewrite by creating under /tmp.
	path := filepath.Join("/tmp", "goisp-mon-log-"+t.Name())
	require.NoError(t, os.WriteFile(path, []byte(strings.Join([]string{
		"line1", "line2", "line3", "line4", "line5",
	}, "\n")+"\n"), 0o644))
	t.Cleanup(func() { _ = os.Remove(path) })

	text, err := TailFile(path, 3)
	require.NoError(t, err)
	assert.Equal(t, "line3\nline4\nline5", text)
	_ = dir
}

func TestTailFile_rejectsTraversal(t *testing.T) {
	text, err := TailFile("/etc/passwd", 10)
	require.NoError(t, err)
	assert.Equal(t, "Logfile path error.", text)
}

func TestTailFile_missing(t *testing.T) {
	text, err := TailFile("/tmp/does-not-exist-goisp-monitor.log", 10)
	require.NoError(t, err)
	assert.Contains(t, text, "Unable to read")
}

func TestCollectSysLogState(t *testing.T) {
	db := testDB(t)
	// Need sys_log table for this test.
	require.NoError(t, db.Exec(`
CREATE TABLE sys_log (
  syslog_id INTEGER PRIMARY KEY AUTOINCREMENT,
  server_id INTEGER NOT NULL DEFAULT 0,
  datalog_id INTEGER NOT NULL DEFAULT 0,
  loglevel INTEGER NOT NULL DEFAULT 0,
  tstamp INTEGER NOT NULL DEFAULT 0,
  message TEXT
)`).Error)
	require.NoError(t, db.Create(&model.SysLog{ServerID: 1, Loglevel: 1, Message: "warn"}).Error)
	require.NoError(t, db.Create(&model.SysLog{ServerID: 1, Loglevel: 2, Message: "err"}).Error)
	require.NoError(t, db.Create(&model.SysLog{ServerID: 1, Loglevel: 0, Message: "cleared"}).Error)

	data, state, err := CollectSysLogState(context.Background(), db, 1)
	require.NoError(t, err)
	assert.Equal(t, "error", state)
	assert.Equal(t, 2, data["open_count"])
}

func TestRunLogCollectors(t *testing.T) {
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
	path := filepath.Join("/tmp", "goisp-mon-isp-"+t.Name()+".log")
	require.NoError(t, os.WriteFile(path, []byte("hello monitor\n"), 0o644))
	t.Cleanup(func() { _ = os.Remove(path) })

	err := RunLogCollectors(context.Background(), db, 1, LogPaths{
		ISPConfig:   path,
		LetsEncrypt: path,
		Messages:    path,
	})
	require.NoError(t, err)
	var n int64
	require.NoError(t, db.Table("monitor_data").Count(&n).Error)
	assert.EqualValues(t, 4, n) // 3 logs + sys_log
}
