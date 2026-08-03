package api

import (
	"net/http"
	"net/http/httptest"
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

func TestMonitorServersBadServerID(t *testing.T) {
	e := monitorTestServer(t, monitorTestDB(t))
	assert.Equal(t, http.StatusBadRequest, monitorGet(e, "adm", "/api/monitor/state?server_id=abc").Code)
	// A server outside the readable set yields an empty list, not an error.
	rec := monitorGet(e, "adm", "/api/monitor/state?server_id=99")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "[]\n", rec.Body.String())
}
