//go:build integration

// Migration wizard integration suite: the /api/system/migration/*
// endpoints against a dockerized MariaDB and the legacytest mock panel —
// connect faults, missing grants, inventory, dry-run, single-active-run
// rejection, SSE progress stream, status reattach and the bulk password
// reset (tasks 5.1/5.2).
package api_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	echoMiddleware "github.com/labstack/echo/v5/middleware"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"go-ispconfig/internal/api"
	"go-ispconfig/internal/auth"
	"go-ispconfig/internal/config"
	"go-ispconfig/internal/database"
	"go-ispconfig/internal/legacy/importer"
	"go-ispconfig/internal/legacy/legacytest"
	"go-ispconfig/internal/model"
)

// migrationEnv is the running API server plus an admin session.
type migrationEnv struct {
	srv    *httptest.Server
	db     *gorm.DB
	cookie string
	csrf   string
}

// setupMigrationEnv boots MariaDB, the API server and an admin login.
func setupMigrationEnv(t *testing.T) *migrationEnv {
	t.Helper()
	dsnPrefix, container := database.StartMariaDB(t, "wizard")
	database.MariaDBExec(t, container, "CREATE DATABASE wizard CHARACTER SET utf8mb4")
	db, err := database.Open(dsnPrefix + "/wizard?charset=utf8mb4&parseTime=True&loc=Local")
	require.NoError(t, err)
	needSeed, err := database.Migrate(db)
	require.NoError(t, err)
	require.True(t, needSeed)
	_, err = database.Seed(db, "panel.test", "wizard-admin-pw")
	require.NoError(t, err)

	e := echo.New()
	e.Use(echoMiddleware.Recover())
	deps := &api.Deps{DB: db, Sessions: auth.NewStore(db, 0), Config: &config.Config{}}
	require.NoError(t, api.Register(e, deps))
	srv := httptest.NewServer(e)
	t.Cleanup(srv.Close)

	env := &migrationEnv{srv: srv, db: db}
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/login",
		bytes.NewReader([]byte(`{"username":"admin","password":"wizard-admin-pw"}`)))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err := srv.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var login api.LoginResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&login))
	env.csrf = login.CSRFToken
	for _, ck := range resp.Cookies() {
		if ck.Name == auth.SessionCookieName {
			env.cookie = ck.Value
		}
	}
	require.NotEmpty(t, env.cookie)
	return env
}

// mcall wraps the shared call helper with the admin session.
func (env *migrationEnv) mcall(t *testing.T, method, path string, body any) (int, []byte) {
	t.Helper()
	return call(t, env.srv, method, path, env.cookie, env.csrf, body)
}

func TestMigrationWizard(t *testing.T) {
	env := setupMigrationEnv(t)
	legacy := legacytest.New()
	t.Cleanup(legacy.Close)

	connectBody := map[string]any{
		"url": legacy.URL, "username": legacy.Username, "password": legacy.Password,
	}

	t.Run("endpoints are admin-only", func(t *testing.T) {
		status, _ := call(t, env.srv, http.MethodGet, "/api/system/migration/status", "", "", nil)
		require.Equal(t, http.StatusUnauthorized, status)
	})

	t.Run("connect with wrong password returns the fault", func(t *testing.T) {
		status, data := env.mcall(t, http.MethodPost, "/api/system/migration/connect",
			map[string]any{"url": legacy.URL, "username": legacy.Username, "password": "nope"})
		require.Equal(t, http.StatusBadRequest, status)
		var out api.MigrationError
		require.NoError(t, json.Unmarshal(data, &out))
		require.Equal(t, "remote_fault", out.FaultCode)
		require.NotContains(t, string(data), "nope", "password never echoed")
	})

	t.Run("connect with missing grants names them", func(t *testing.T) {
		var granted []string
		for _, fn := range legacy.Functions {
			if fn != "dns_zone_get" {
				granted = append(granted, fn)
			}
		}
		old := legacy.Functions
		legacy.Functions = granted
		defer func() { legacy.Functions = old }()

		status, data := env.mcall(t, http.MethodPost, "/api/system/migration/connect", connectBody)
		require.Equal(t, http.StatusBadRequest, status)
		var out api.MigrationError
		require.NoError(t, json.Unmarshal(data, &out))
		require.Equal(t, []string{"dns_zone_get"}, out.MissingFunctions)
	})

	t.Run("connect succeeds with panel info", func(t *testing.T) {
		status, data := env.mcall(t, http.MethodPost, "/api/system/migration/connect", connectBody)
		require.Equal(t, http.StatusOK, status)
		var out api.MigrationConnectResponse
		require.NoError(t, json.Unmarshal(data, &out))
		require.Len(t, out.Servers, 1)
		require.False(t, out.MultiServer)
		require.True(t, out.PlainHTTP)
	})

	t.Run("inventory", func(t *testing.T) {
		status, data := env.mcall(t, http.MethodGet, "/api/system/migration/inventory", nil)
		require.Equal(t, http.StatusOK, status)
		var inv importer.Inventory
		require.NoError(t, json.Unmarshal(data, &inv))
		require.Equal(t, 3, inv.Clients)
		require.Equal(t, 1201, inv.WebDomains)
		require.Equal(t, 2, inv.DNSZones)
	})

	t.Run("dry-run returns counts and reset list, writes nothing", func(t *testing.T) {
		status, data := env.mcall(t, http.MethodPost, "/api/system/migration/dry-run", map[string]any{})
		require.Equal(t, http.StatusOK, status)
		var out api.MigrationPlanResponse
		require.NoError(t, json.Unmarshal(data, &out))
		require.Equal(t, 1201, out.Counts["web_domain"].Created)
		require.Empty(t, out.Conflicts)
		require.Equal(t, []string{"reseller1", "client2", "client3"}, out.ResetRequired)

		var n int64
		require.NoError(t, env.db.Model(&model.SysDatalog{}).Count(&n).Error)
		require.Zero(t, n, "dry-run must not write")
	})

	// SSE subscription must be up before the run starts.
	sseReq, err := http.NewRequest(http.MethodGet, env.srv.URL+"/api/system/migration/progress", nil)
	require.NoError(t, err)
	sseReq.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: env.cookie})
	sseResp, err := (&http.Client{}).Do(sseReq)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sseResp.Body.Close() })
	require.Equal(t, http.StatusOK, sseResp.StatusCode)
	require.Contains(t, sseResp.Header.Get("Content-Type"), "text/event-stream")

	t.Run("execute starts one run and rejects a concurrent start", func(t *testing.T) {
		legacy.Delay = 30 * time.Millisecond // keep the run in flight
		defer func() { legacy.Delay = 0 }()

		status, _ := env.mcall(t, http.MethodPost, "/api/system/migration/execute", map[string]any{})
		require.Equal(t, http.StatusAccepted, status)

		status, data := env.mcall(t, http.MethodPost, "/api/system/migration/execute", map[string]any{})
		require.Equal(t, http.StatusConflict, status)
		require.Contains(t, string(data), "already active")

		// Connect and inventory are also locked out while running.
		status, _ = env.mcall(t, http.MethodPost, "/api/system/migration/connect", connectBody)
		require.Equal(t, http.StatusConflict, status)
	})

	t.Run("SSE streams progress and the final status", func(t *testing.T) {
		var sawProgress, sawDone bool
		var last importer.Progress
		scanner := bufio.NewScanner(sseResp.Body)
		scanner.Buffer(make([]byte, 1<<20), 1<<20)
		deadline := time.After(120 * time.Second)
		lines := make(chan string)
		go func() {
			for scanner.Scan() {
				lines <- scanner.Text()
			}
			close(lines)
		}()
		for !sawDone {
			select {
			case <-deadline:
				t.Fatal("SSE stream did not reach a final status in time")
			case line, ok := <-lines:
				require.True(t, ok, "SSE stream closed before the final status")
				payload, found := strings.CutPrefix(line, "data: ")
				if !found {
					continue
				}
				var progress importer.Progress
				if json.Unmarshal([]byte(payload), &progress) == nil && progress.Entity != "" {
					sawProgress = true
					if progress.Entity == "web_domain" {
						last = progress
					}
					continue
				}
				var status api.MigrationStatus
				if json.Unmarshal([]byte(payload), &status) == nil {
					if status.State == "done" || status.State == "failed" {
						require.Equal(t, "done", status.State, "run failed: %s", status.Error)
						require.NotNil(t, status.Report)
						sawDone = true
					}
				}
			}
		}
		require.True(t, sawProgress, "no progress events received")
		require.Equal(t, 1201, last.Done)
		require.Equal(t, 1201, last.Total)
	})

	t.Run("status reattach after the run", func(t *testing.T) {
		status, data := env.mcall(t, http.MethodGet, "/api/system/migration/status", nil)
		require.Equal(t, http.StatusOK, status)
		var out api.MigrationStatus
		require.NoError(t, json.Unmarshal(data, &out))
		require.Equal(t, "done", out.State)
		require.NotNil(t, out.Report)
		require.Equal(t, 1201, out.Progress["web_domain"].Done)
		require.Equal(t, []string{"reseller1", "client2", "client3"}, out.Report.ResetRequired)
		require.NotContains(t, string(data), legacy.Password, "credentials never in responses")

		var n int64
		require.NoError(t, env.db.Model(&model.WebDomain{}).Count(&n).Error)
		require.EqualValues(t, 1201, n, "run applied the import")
	})

	t.Run("bulk reset tokens", func(t *testing.T) {
		status, data := env.mcall(t, http.MethodPost, "/api/system/migration/reset-passwords", nil)
		require.Equal(t, http.StatusOK, status)
		var tokens []importer.ResetToken
		require.NoError(t, json.Unmarshal(data, &tokens))
		require.Len(t, tokens, 3)
		for _, tok := range tokens {
			require.Len(t, tok.Token, 32)
			var u model.SysUser
			require.NoError(t, env.db.Where("username = ?", tok.Username).First(&u).Error)
			require.Equal(t, importer.HashResetToken(tok.Token), u.LostPasswordHash)
		}
	})

	t.Run("multi-server execute requires explicit confirmation", func(t *testing.T) {
		multi := legacytest.New()
		t.Cleanup(multi.Close)
		multi.Servers = append(multi.Servers, legacytest.Rec{"server_id": "2", "server_name": "legacy2"})

		status, data := env.mcall(t, http.MethodPost, "/api/system/migration/connect",
			map[string]any{"url": multi.URL, "username": multi.Username, "password": multi.Password})
		require.Equal(t, http.StatusOK, status)
		var conn api.MigrationConnectResponse
		require.NoError(t, json.Unmarshal(data, &conn))
		require.True(t, conn.MultiServer)

		status, data = env.mcall(t, http.MethodPost, "/api/system/migration/execute", map[string]any{})
		require.Equal(t, http.StatusBadRequest, status)
		require.Contains(t, string(data), "confirm_map_all_to_local_server")
	})
}
