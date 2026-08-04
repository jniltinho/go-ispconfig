//go:build integration

package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"
	echoMiddleware "github.com/labstack/echo/v5/middleware"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/api"
	"go-ispconfig/internal/auth"
	"go-ispconfig/internal/config"
	"go-ispconfig/internal/database"
	"go-ispconfig/internal/model"
)

// call performs one JSON request against the test server, optionally with
// the session cookie and CSRF token.
func call(t *testing.T, srv *httptest.Server, method, path, cookie, csrf string, body any) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequest(method, srv.URL+path, reader)
	require.NoError(t, err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: cookie})
	}
	if csrf != "" {
		req.Header.Set(auth.CSRFHeaderName, csrf)
	}
	resp, err := srv.Client().Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, data
}

// errKeyOf decodes the {error:{key,fields}} body.
func errKeyOf(t *testing.T, data []byte) api.ErrorInfo {
	t.Helper()
	var body api.ErrorResponse
	require.NoError(t, json.Unmarshal(data, &body))
	return body.Error
}

// TestAPISmoke is the REST API core end-to-end suite against a throwaway
// MariaDB: login (lockout, CSRF, cookie), server_ip CRUD through the
// permission-scoped repository with datalog journaling, validation 422s,
// 401/403 paths, form metadata and the swagger UI.
func TestAPISmoke(t *testing.T) {
	dsnPrefix, container := database.StartMariaDB(t, "api")
	database.MariaDBExec(t, container, "CREATE DATABASE ispconfig CHARACTER SET utf8mb4")
	db, err := database.Open(dsnPrefix + "/ispconfig?charset=utf8mb4&parseTime=True&loc=Local")
	require.NoError(t, err)

	needSeed, err := database.Migrate(db)
	require.NoError(t, err)
	require.True(t, needSeed)
	_, err = database.Seed(db, "server1.example.com", "smoke-test-pw")
	require.NoError(t, err)

	e := echo.New()
	e.Use(echoMiddleware.Recover())
	deps := &api.Deps{DB: db, Sessions: auth.NewStore(db, 0), Config: &config.Config{}}
	require.NoError(t, api.Register(e, deps))
	api.RegisterSwagger(e, deps.Sessions, deps.Config.Swagger)
	srv := httptest.NewServer(e)
	defer srv.Close()

	var cookie, csrf string

	t.Run("login with wrong password is 401", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/login", "", "",
			map[string]any{"username": "admin", "password": "wrong"})
		require.Equal(t, http.StatusUnauthorized, status)
		require.Equal(t, "error.unauthorized", errKeyOf(t, data).Key)
	})

	t.Run("login succeeds with cookie and CSRF token", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/login",
			bytes.NewReader([]byte(`{"username":"admin","password":"smoke-test-pw"}`)))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		resp, err := srv.Client().Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		require.Equal(t, http.StatusOK, resp.StatusCode)

		var login api.LoginResponse
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&login))
		require.Equal(t, "admin", login.Username)
		require.NotEmpty(t, login.CSRFToken)
		csrf = login.CSRFToken

		for _, ck := range resp.Cookies() {
			if ck.Name == auth.SessionCookieName {
				cookie = ck.Value
				require.True(t, ck.HttpOnly)
			}
		}
		require.NotEmpty(t, cookie, "session cookie missing")
		require.Equal(t, cookie, login.SessionID, "session_id must match the cookie for bearer clients")
	})

	t.Run("session info reports the admin", func(t *testing.T) {
		status, data := call(t, srv, http.MethodGet, "/api/session", cookie, "", nil)
		require.Equal(t, http.StatusOK, status)
		var info api.SessionInfo
		require.NoError(t, json.Unmarshal(data, &info))
		require.Equal(t, "admin", info.Username)
		require.Equal(t, "admin", info.Typ)
		require.Equal(t, csrf, info.CSRFToken, "session endpoint must return the CSRF token for SPA reloads")
	})

	t.Run("stay_logged_in creates a permanent session", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/login", "", "",
			map[string]any{"username": "admin", "password": "smoke-test-pw", "stay_logged_in": true})
		require.Equal(t, http.StatusOK, status)
		var login api.LoginResponse
		require.NoError(t, json.Unmarshal(data, &login))
		var row model.SysSession
		require.NoError(t, db.Where("session_id = ?", login.SessionID).First(&row).Error)
		require.Equal(t, "y", row.Permanent)
	})

	t.Run("entity routes require a session", func(t *testing.T) {
		status, data := call(t, srv, http.MethodGet, "/api/server_ip", "", "", nil)
		require.Equal(t, http.StatusUnauthorized, status)
		require.Equal(t, "error.unauthorized", errKeyOf(t, data).Key)
	})

	t.Run("mutating cookie request without CSRF token is 403", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/server_ip", cookie, "",
			map[string]any{"server_id": 1, "ip_address": "10.1.2.3"})
		require.Equal(t, http.StatusForbidden, status)
		require.Equal(t, "error.forbidden", errKeyOf(t, data).Key)
	})

	t.Run("invalid ip is 422 with field key", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/server_ip", cookie, csrf,
			map[string]any{"server_id": 1, "ip_address": "999.1.1.1"})
		require.Equal(t, http.StatusUnprocessableEntity, status)
		info := errKeyOf(t, data)
		require.Equal(t, "error.validation_failed", info.Key)
		require.Equal(t, []string{"ip_error_wrong"}, info.Fields["ip_address"])
	})

	var createdID uint32

	t.Run("create server_ip applies defaults and journals datalog", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/server_ip", cookie, csrf,
			map[string]any{"server_id": 1, "ip_address": "10.1.2.3"})
		require.Equal(t, http.StatusCreated, status)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		require.Equal(t, "10.1.2.3", rec["ip_address"])
		require.Equal(t, "IPv4", rec["ip_type"], "default applied")
		require.Equal(t, "80,443", rec["virtualhost_port"], "default applied")
		createdID = uint32(rec["server_ip_id"].(float64))
		require.NotZero(t, createdID)

		var dl model.SysDatalog
		require.NoError(t, db.Where("dbtable = ?", "server_ip").Order("datalog_id DESC").First(&dl).Error)
		require.Equal(t, "i", dl.Action)
		require.Equal(t, fmt.Sprintf("server_ip_id:%d", createdID), dl.DBIdx)
		require.Equal(t, "admin", dl.User)
	})

	t.Run("duplicate ip is 422 UNIQUE", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/server_ip", cookie, csrf,
			map[string]any{"server_id": 1, "ip_address": "10.1.2.3"})
		require.Equal(t, http.StatusUnprocessableEntity, status)
		require.Equal(t, []string{"ip_error_unique"}, errKeyOf(t, data).Fields["ip_address"])
	})

	t.Run("list paginates and filters", func(t *testing.T) {
		status, data := call(t, srv, http.MethodGet, "/api/server_ip?page=1&limit=10&ip_address=10.1", cookie, "", nil)
		require.Equal(t, http.StatusOK, status)
		var list api.ListResponse
		require.NoError(t, json.Unmarshal(data, &list))
		require.EqualValues(t, 1, list.Total)
		require.Len(t, list.Items, 1)
		require.Equal(t, "10.1.2.3", list.Items[0]["ip_address"])

		status, data = call(t, srv, http.MethodGet, "/api/server_ip?ip_address=nope", cookie, "", nil)
		require.Equal(t, http.StatusOK, status)
		require.NoError(t, json.Unmarshal(data, &list))
		require.Zero(t, list.Total)
	})

	t.Run("list sorts by declared field via order param", func(t *testing.T) {
		status, _ := call(t, srv, http.MethodPost, "/api/server_ip", cookie, csrf,
			map[string]any{"server_id": 1, "ip_address": "10.0.0.9"})
		require.Equal(t, http.StatusCreated, status)

		status, data := call(t, srv, http.MethodGet, "/api/server_ip?order=ip_address", cookie, "", nil)
		require.Equal(t, http.StatusOK, status)
		var list api.ListResponse
		require.NoError(t, json.Unmarshal(data, &list))
		require.GreaterOrEqual(t, len(list.Items), 2)
		require.Equal(t, "10.0.0.9", list.Items[0]["ip_address"])

		status, data = call(t, srv, http.MethodGet, "/api/server_ip?order=ip_address.desc", cookie, "", nil)
		require.Equal(t, http.StatusOK, status)
		require.NoError(t, json.Unmarshal(data, &list))
		require.Equal(t, "10.1.2.3", list.Items[0]["ip_address"])

		// Undeclared field names fall back to PK order (no SQL injection).
		status, _ = call(t, srv, http.MethodGet, "/api/server_ip?order=;drop", cookie, "", nil)
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("get and update round trip", func(t *testing.T) {
		path := fmt.Sprintf("/api/server_ip/%d", createdID)
		status, _ := call(t, srv, http.MethodGet, path, cookie, "", nil)
		require.Equal(t, http.StatusOK, status)

		status, data := call(t, srv, http.MethodPut, path, cookie, csrf,
			map[string]any{"virtualhost_port": "8080"})
		require.Equal(t, http.StatusOK, status)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		require.Equal(t, "8080", rec["virtualhost_port"])
		require.Equal(t, "10.1.2.3", rec["ip_address"], "unchanged fields kept")

		var dl model.SysDatalog
		require.NoError(t, db.Where("dbtable = ? AND action = 'u'", "server_ip").
			Order("datalog_id DESC").First(&dl).Error)
		require.Contains(t, dl.Data, "8080")
	})

	t.Run("update with invalid port is 422", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPut, fmt.Sprintf("/api/server_ip/%d", createdID), cookie, csrf,
			map[string]any{"virtualhost_port": "not-a-port"})
		require.Equal(t, http.StatusUnprocessableEntity, status)
		require.Equal(t, []string{"error_port_syntax"}, errKeyOf(t, data).Fields["virtualhost_port"])
	})

	t.Run("form metadata for server_ip", func(t *testing.T) {
		status, data := call(t, srv, http.MethodGet, "/api/meta/forms/server_ip", cookie, "", nil)
		require.Equal(t, http.StatusOK, status)
		var meta api.FormMeta
		require.NoError(t, json.Unmarshal(data, &meta))
		require.Equal(t, "server_ip", meta.Name)
		require.NotEmpty(t, meta.Tabs[0].Fields)
	})

	t.Run("swagger requires an admin session by default", func(t *testing.T) {
		status, _ := call(t, srv, http.MethodGet, "/swagger/", "", "", nil)
		require.Equal(t, http.StatusUnauthorized, status)

		status, data := call(t, srv, http.MethodGet, "/swagger/", cookie, "", nil)
		require.Equal(t, http.StatusOK, status)
		require.Contains(t, string(data), "swagger-ui")
		status, data = call(t, srv, http.MethodGet, "/swagger/doc.json", cookie, "", nil)
		require.Equal(t, http.StatusOK, status)
		require.Contains(t, string(data), `"/server_ip"`)
	})

	t.Run("scheduler status reads the sys_config mirror", func(t *testing.T) {
		require.NoError(t, db.Exec(
			"REPLACE INTO sys_config (`group`, `name`, `value`) VALUES ('scheduler', 'datalog_prune_last_run', '2026-08-01T03:00:00Z'), ('scheduler', 'datalog_prune_status', 'ok')").Error)
		status, data := call(t, srv, http.MethodGet, "/api/system/scheduler", cookie, "", nil)
		require.Equal(t, http.StatusOK, status)
		var jobs []api.SchedulerJob
		require.NoError(t, json.Unmarshal(data, &jobs))
		require.Len(t, jobs, 1)
		require.Equal(t, "datalog_prune", jobs[0].Name)
		require.Equal(t, "2026-08-01T03:00:00Z", jobs[0].LastRun)
		require.Equal(t, "ok", jobs[0].Status)
	})

	t.Run("delete removes the record and journals datalog", func(t *testing.T) {
		status, _ := call(t, srv, http.MethodDelete, fmt.Sprintf("/api/server_ip/%d", createdID), cookie, csrf, nil)
		require.Equal(t, http.StatusNoContent, status)

		status, _ = call(t, srv, http.MethodGet, fmt.Sprintf("/api/server_ip/%d", createdID), cookie, "", nil)
		require.Equal(t, http.StatusForbidden, status, "denied and missing are indistinguishable by design")

		var dl model.SysDatalog
		require.NoError(t, db.Where("dbtable = ? AND action = 'd'", "server_ip").
			Order("datalog_id DESC").First(&dl).Error)
	})

	t.Run("logout invalidates the session", func(t *testing.T) {
		status, _ := call(t, srv, http.MethodPost, "/api/logout", cookie, csrf, nil)
		require.Equal(t, http.StatusNoContent, status)
		status, _ = call(t, srv, http.MethodGet, "/api/session", cookie, "", nil)
		require.Equal(t, http.StatusUnauthorized, status)
	})

	t.Run("repeated failed logins are locked out", func(t *testing.T) {
		var status int
		var data []byte
		for i := 0; i < 8; i++ {
			status, data = call(t, srv, http.MethodPost, "/api/login", "", "",
				map[string]any{"username": "admin", "password": "wrong"})
		}
		require.Equal(t, http.StatusTooManyRequests, status)
		require.Equal(t, "error.too_many_requests", errKeyOf(t, data).Key)
	})
}
