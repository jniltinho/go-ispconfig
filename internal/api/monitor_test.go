package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"go-ispconfig/internal/auth"
	"go-ispconfig/internal/model"
)

// monitorTestDB builds an in-memory sqlite DB with the tables the monitor
// endpoints read (approximated schema; MariaDB parity runs in integration).
func monitorTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	for _, stmt := range []string{
		`CREATE TABLE monitor_data (
  server_id INTEGER NOT NULL DEFAULT 0,
  type TEXT NOT NULL DEFAULT '',
  created INTEGER NOT NULL DEFAULT 0,
  data TEXT,
  state TEXT NOT NULL DEFAULT 'unknown',
  PRIMARY KEY (server_id, type, created))`,
		`CREATE TABLE server (
  server_id INTEGER PRIMARY KEY AUTOINCREMENT,
  sys_userid INTEGER NOT NULL DEFAULT 1,
  sys_groupid INTEGER NOT NULL DEFAULT 1,
  sys_perm_user TEXT NOT NULL DEFAULT 'riud',
  sys_perm_group TEXT NOT NULL DEFAULT 'riud',
  sys_perm_other TEXT NOT NULL DEFAULT '',
  server_name TEXT NOT NULL DEFAULT '',
  mail_server INTEGER NOT NULL DEFAULT 0,
  web_server INTEGER NOT NULL DEFAULT 0,
  dns_server INTEGER NOT NULL DEFAULT 0,
  file_server INTEGER NOT NULL DEFAULT 0,
  db_server INTEGER NOT NULL DEFAULT 0,
  vserver_server INTEGER NOT NULL DEFAULT 0,
  proxy_server INTEGER NOT NULL DEFAULT 0,
  firewall_server INTEGER NOT NULL DEFAULT 0,
  updated INTEGER NOT NULL DEFAULT 0,
  active INTEGER NOT NULL DEFAULT 1)`,
		`CREATE TABLE sys_log (
  syslog_id INTEGER PRIMARY KEY AUTOINCREMENT,
  server_id INTEGER NOT NULL DEFAULT 0,
  datalog_id INTEGER NOT NULL DEFAULT 0,
  loglevel INTEGER NOT NULL DEFAULT 0,
  tstamp INTEGER NOT NULL DEFAULT 0,
  message TEXT)`,
		`CREATE TABLE sys_datalog (
  datalog_id INTEGER PRIMARY KEY AUTOINCREMENT,
  server_id INTEGER NOT NULL DEFAULT 0,
  dbtable TEXT NOT NULL DEFAULT '',
  dbidx TEXT NOT NULL DEFAULT '',
  action TEXT NOT NULL DEFAULT '',
  tstamp INTEGER NOT NULL DEFAULT 0,
  user TEXT NOT NULL DEFAULT '',
  data TEXT,
  status TEXT NOT NULL DEFAULT 'pending',
  error TEXT,
  session_id TEXT NOT NULL DEFAULT '')`,
	} {
		require.NoError(t, db.Exec(stmt).Error)
	}
	require.NoError(t, db.Exec(
		`INSERT INTO server (server_id, server_name) VALUES (1, 'server1.example.com')`).Error)
	return db
}

// monitorTestServer wires the monitor routes with stub sessions:
// "adm" (admin), "mon" (user with monitor module), "usr" (no monitor).
func monitorTestServer(t *testing.T, db *gorm.DB) *echo.Echo {
	t.Helper()
	e := echo.New()
	e.HTTPErrorHandler = ErrorHandler()
	sessions := stubSessions{
		"adm": {UserID: 1, Username: "admin", Typ: "admin"},
		"mon": {UserID: 2, Username: "watcher", Typ: "user", Groups: []uint32{2}, Modules: "dashboard,monitor"},
		"usr": {UserID: 3, Username: "client1", Typ: "user", Groups: []uint32{3}, Modules: "dashboard,sites"},
	}
	g := e.Group("/api", auth.Middleware(sessions))
	protected := g.Group("", auth.RequireAuth())
	registerMonitorRoutes(protected, &Deps{DB: db})
	return e
}

// monitorGet performs a GET with the given bearer token.
func monitorGet(e *echo.Echo, bearer, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestMonitorModuleGate(t *testing.T) {
	e := monitorTestServer(t, monitorTestDB(t))
	for _, path := range []string{"/api/monitor/state", "/api/monitor/data", "/api/monitor/data/cpu_info"} {
		assert.Equal(t, http.StatusUnauthorized, monitorGet(e, "", path).Code, path)
		assert.Equal(t, http.StatusForbidden, monitorGet(e, "usr", path).Code,
			"%s must 403 without the monitor module", path)
	}
}

func TestMonitorStateAndData(t *testing.T) {
	db := monitorTestDB(t)
	now := uint32(time.Now().Unix())
	require.NoError(t, db.Create(&model.MonitorData{
		ServerID: 1, Type: "disk_usage", Created: now,
		Data: `[{"fs":"/dev/sda1","mounted":"/","percent":42}]`, State: "ok",
	}).Error)
	require.NoError(t, db.Create(&model.MonitorData{
		ServerID: 1, Type: "services", Created: now, Data: `{"webserver":0}`, State: "error",
	}).Error)
	e := monitorTestServer(t, db)

	rec := monitorGet(e, "adm", "/api/monitor/state")
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, `"server_name":"server1.example.com"`)
	assert.Contains(t, body, `"state":"error"`, "services error must fold into the server state")

	rec = monitorGet(e, "adm", "/api/monitor/data?type=disk_usage")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"percent":42`, "payload must be decoded JSON")

	rec = monitorGet(e, "adm", "/api/monitor/data/services")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"webserver":0`)

	rec = monitorGet(e, "adm", "/api/monitor/data/mailq")
	assert.Equal(t, http.StatusNotFound, rec.Code, "unknown type has no sample")

	// Non-admin monitor user reads nothing: no readable server rows.
	rec = monitorGet(e, "mon", "/api/monitor/data")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "[]\n", rec.Body.String())
}

// monitorPost performs a JSON POST with the given bearer token.
func monitorPost(e *echo.Echo, bearer, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestMonitorSysLogListAndClear(t *testing.T) {
	db := monitorTestDB(t)
	now := uint32(time.Now().Unix())
	require.NoError(t, db.Create(&model.SysLog{ServerID: 1, Loglevel: 1, Tstamp: now, Message: "warn one"}).Error)
	require.NoError(t, db.Create(&model.SysLog{ServerID: 1, Loglevel: 1, Tstamp: now, Message: "warn two"}).Error)
	require.NoError(t, db.Create(&model.SysLog{ServerID: 1, Loglevel: 2, Tstamp: now, Message: "an error"}).Error)
	e := monitorTestServer(t, db)

	rec := monitorGet(e, "adm", "/api/monitor/sys-log?loglevel=1")
	require.Equal(t, http.StatusOK, rec.Code)
	var list SysLogList
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	assert.EqualValues(t, 2, list.Total)

	// Clear is admin-only.
	assert.Equal(t, http.StatusForbidden,
		monitorPost(e, "mon", "/api/monitor/sys-log/clear", `{"loglevel":1}`).Code)

	// Batch clear by level: rows stay, loglevel drops to 0 (no DELETE).
	rec = monitorPost(e, "adm", "/api/monitor/sys-log/clear", `{"loglevel":1}`)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"cleared":2`)
	var rows []model.SysLog
	require.NoError(t, db.Order("syslog_id").Find(&rows).Error)
	require.Len(t, rows, 3, "clear must never delete rows")
	assert.EqualValues(t, 0, rows[0].Loglevel)
	assert.EqualValues(t, 0, rows[1].Loglevel)
	assert.EqualValues(t, 2, rows[2].Loglevel)

	// Single-id clear.
	rec = monitorPost(e, "adm", "/api/monitor/sys-log/clear",
		fmt.Sprintf(`{"syslog_id":%d}`, rows[2].SyslogID))
	require.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, db.Order("syslog_id").Find(&rows).Error)
	assert.EqualValues(t, 0, rows[2].Loglevel)

	// Missing selector is a 400.
	assert.Equal(t, http.StatusBadRequest,
		monitorPost(e, "adm", "/api/monitor/sys-log/clear", `{}`).Code)
}

func TestMonitorJobqueueAndDatalog(t *testing.T) {
	db := monitorTestDB(t)
	// server.updated = 2: datalog rows 1..2 are processed, 3+ are pending.
	require.NoError(t, db.Exec(`UPDATE server SET updated = 2 WHERE server_id = 1`).Error)
	rows := []model.SysDatalog{
		{ServerID: 1, DBTable: "web_domain", DBIdx: "domain_id:1", Action: "i", Tstamp: 100, User: "admin",
			Data: `{"old":null,"new":{"domain":"a.example.com"}}`, Status: "ok"},
		{ServerID: 1, DBTable: "web_domain", DBIdx: "domain_id:1", Action: "u", Tstamp: 200, User: "admin",
			Data:   `a:2:{s:3:"old";a:1:{s:6:"domain";s:13:"a.example.com";}s:3:"new";a:1:{s:6:"domain";s:13:"b.example.com";}}`,
			Status: "ok"},
		{ServerID: 1, DBTable: "mail_domain", DBIdx: "domain_id:2", Action: "i", Tstamp: 300, User: "admin",
			Data: `{"old":null,"new":{"domain":"mail.example.com"}}`, Status: "pending"},
		{ServerID: 0, DBTable: "sys_config", DBIdx: "name:x", Action: "u", Tstamp: 400, User: "admin",
			Data: `{"old":1,"new":2}`, Status: "pending"},
	}
	for i := range rows {
		require.NoError(t, db.Create(&rows[i]).Error)
	}
	e := monitorTestServer(t, db)

	// Module gate applies to the new routes too.
	for _, path := range []string{
		"/api/monitor/jobqueue", "/api/monitor/jobqueue/count",
		"/api/monitor/datalog", "/api/monitor/datalog/1",
	} {
		assert.Equal(t, http.StatusForbidden, monitorGet(e, "usr", path).Code, path)
	}

	// Jobqueue: only rows with datalog_id > server.updated (3 and 4, incl. server_id=0).
	rec := monitorGet(e, "adm", "/api/monitor/jobqueue")
	require.Equal(t, http.StatusOK, rec.Code)
	var list DatalogList
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	assert.EqualValues(t, 2, list.Total)
	require.Len(t, list.Items, 2)
	assert.EqualValues(t, 3, list.Items[0].DatalogID, "pending rows come oldest first")
	assert.Empty(t, list.Items[0].Data, "list omits the payload")

	// Jobqueue dbtable filter.
	rec = monitorGet(e, "adm", "/api/monitor/jobqueue?dbtable=mail_domain")
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	assert.EqualValues(t, 1, list.Total)

	// Count matches the unfiltered pending list.
	rec = monitorGet(e, "adm", "/api/monitor/jobqueue/count")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"count":2}`, rec.Body.String())

	// Datalog history: all rows, newest first.
	rec = monitorGet(e, "adm", "/api/monitor/datalog")
	require.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	assert.EqualValues(t, 4, list.Total)
	assert.EqualValues(t, 4, list.Items[0].DatalogID)

	// History filters.
	rec = monitorGet(e, "adm", "/api/monitor/datalog?action=u")
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	assert.EqualValues(t, 2, list.Total)

	// Detail decodes JSON payloads.
	rec = monitorGet(e, "adm", "/api/monitor/datalog/1")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"new":{"domain":"a.example.com"}`)

	// Detail decodes legacy PHP-serialize payloads.
	rec = monitorGet(e, "adm", "/api/monitor/datalog/2")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"b.example.com"`, "PHP serialize payload must decode")
	assert.NotContains(t, rec.Body.String(), "decode_error")

	// Unknown id → 404; bad id → 400.
	assert.Equal(t, http.StatusNotFound, monitorGet(e, "adm", "/api/monitor/datalog/99").Code)
	assert.Equal(t, http.StatusBadRequest, monitorGet(e, "adm", "/api/monitor/datalog/abc").Code)

	// Monitor user without readable servers sees nothing.
	rec = monitorGet(e, "mon", "/api/monitor/datalog")
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	assert.EqualValues(t, 0, list.Total)
	assert.Equal(t, http.StatusNotFound, monitorGet(e, "mon", "/api/monitor/datalog/1").Code,
		"detail must not leak rows of unreadable servers")
	rec = monitorGet(e, "mon", "/api/monitor/jobqueue/count")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"count":0}`, rec.Body.String())
}

func TestMonitorServersBadServerID(t *testing.T) {
	e := monitorTestServer(t, monitorTestDB(t))
	assert.Equal(t, http.StatusBadRequest, monitorGet(e, "adm", "/api/monitor/state?server_id=abc").Code)
	// A server outside the readable set yields an empty list, not an error.
	rec := monitorGet(e, "adm", "/api/monitor/state?server_id=99")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "[]\n", rec.Body.String())
}
