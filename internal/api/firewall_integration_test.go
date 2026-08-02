//go:build integration

package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"
	echoMiddleware "github.com/labstack/echo/v5/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"go-ispconfig/internal/api"
	"go-ispconfig/internal/auth"
	"go-ispconfig/internal/config"
	"go-ispconfig/internal/database"
	"go-ispconfig/internal/model"
)

// newFirewallTestEnv boots MariaDB + the API with three sessions: the
// superadmin (id 1, passes admin_allow_firewall_config), a second admin
// (id != 1, blocked by the superadmin policy) and a client user (blocked
// by AdminOnly). A second server row lets the immutable/unique checks
// target a different server_id.
func newFirewallTestEnv(t *testing.T) (*gorm.DB, *httptest.Server, string, string, string, string, string, string) {
	t.Helper()
	dsnPrefix, container := database.StartMariaDB(t, "firewallapi")
	database.MariaDBExec(t, container, "CREATE DATABASE ispconfig CHARACTER SET utf8mb4")
	db, err := database.Open(dsnPrefix + "/ispconfig?charset=utf8mb4&parseTime=True&loc=Local")
	require.NoError(t, err)
	needSeed, err := database.Migrate(db)
	require.NoError(t, err)
	require.True(t, needSeed)
	_, err = database.Seed(db, "server1.example.com", "smoke-test-pw")
	require.NoError(t, err)
	// A second server so server_id=2 is a valid, distinct target.
	require.NoError(t, db.Exec(
		"INSERT INTO server (server_id, server_name, config, active) VALUES (2, 'server2.example.com', '', 1)").Error)

	hash, err := auth.HashPassword("pw2026")
	require.NoError(t, err)
	// Second admin (id != 1): admin type but not the superadmin.
	admin2 := model.SysUser{
		Username: "admin2", Passwort: hash, Typ: "admin", Active: 1,
		Language: "en", Groups: "1", DefaultGroup: 1,
	}
	require.NoError(t, db.Create(&admin2).Error)
	require.NotEqual(t, uint32(auth.SuperadminUserID), admin2.UserID, "admin2 must not be id 1")
	// Client user in its own group.
	grp := model.SysGroup{Name: "clientx", ClientID: 1}
	require.NoError(t, db.Create(&grp).Error)
	client := model.SysUser{
		Username: "clientx", Passwort: hash, Typ: "user", Active: 1,
		Language: "en", Groups: fmt.Sprint(grp.GroupID), DefaultGroup: grp.GroupID,
	}
	require.NoError(t, db.Create(&client).Error)

	e := echo.New()
	e.Use(echoMiddleware.Recover())
	deps := &api.Deps{DB: db, Sessions: auth.NewStore(db, 0), Config: &config.Config{}}
	require.NoError(t, api.Register(e, deps))
	srv := httptest.NewServer(e)
	t.Cleanup(srv.Close)

	adminCk, adminCsrf := login(t, srv, "admin", "smoke-test-pw")
	admin2Ck, admin2Csrf := login(t, srv, "admin2", "pw2026")
	clientCk, clientCsrf := login(t, srv, "clientx", "pw2026")
	return db, srv, adminCk, adminCsrf, admin2Ck, admin2Csrf, clientCk, clientCsrf
}

func TestFirewallAPI(t *testing.T) {
	db, srv, adminCk, adminCsrf, admin2Ck, admin2Csrf, clientCk, clientCsrf := newFirewallTestEnv(t)

	var fwID float64
	t.Run("superadmin creates, defaults journal", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/firewall", adminCk, adminCsrf,
			map[string]any{"server_id": 1, "tcp_port": "22,80,443", "udp_port": "53", "active": "y"})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		fwID = rec["firewall_id"].(float64)
		assert.Equal(t, "22,80,443", rec["tcp_port"])

		var dl model.SysDatalog
		require.NoError(t, db.Where("dbtable = 'firewall' AND action = 'i'").
			Order("datalog_id DESC").First(&dl).Error)
		assert.Equal(t, fmt.Sprintf("firewall_id:%d", int(fwID)), dl.DBIdx)
	})

	t.Run("list and get", func(t *testing.T) {
		status, data := call(t, srv, http.MethodGet, "/api/firewall", adminCk, adminCsrf, nil)
		require.Equal(t, http.StatusOK, status, "%s", data)
		status, data = call(t, srv, http.MethodGet, fmt.Sprintf("/api/firewall/%d", int(fwID)), adminCk, adminCsrf, nil)
		require.Equal(t, http.StatusOK, status, "%s", data)
	})

	t.Run("duplicate server_id rejected (UNIQUE)", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/firewall", adminCk, adminCsrf,
			map[string]any{"server_id": 1, "tcp_port": "22", "udp_port": "", "active": "y"})
		require.Equal(t, http.StatusUnprocessableEntity, status, "%s", data)
		assert.Contains(t, string(data), "firewall_error_unique")
	})

	t.Run("full-object update re-sending same server_id passes", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPut, fmt.Sprintf("/api/firewall/%d", int(fwID)), adminCk, adminCsrf,
			map[string]any{"server_id": 1, "tcp_port": "22,80,443,8080", "udp_port": "53", "active": "y"})
		require.Equal(t, http.StatusOK, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		assert.Equal(t, "22,80,443,8080", rec["tcp_port"])
	})

	t.Run("update changing server_id rejected (immutable)", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPut, fmt.Sprintf("/api/firewall/%d", int(fwID)), adminCk, adminCsrf,
			map[string]any{"server_id": 2, "tcp_port": "22", "udp_port": "53", "active": "y"})
		require.Equal(t, http.StatusUnprocessableEntity, status, "%s", data)
		assert.Contains(t, string(data), "firewall_error_server_immutable")
	})

	t.Run("non-superadmin admin is blocked by policy", func(t *testing.T) {
		status, _ := call(t, srv, http.MethodGet, "/api/firewall", admin2Ck, admin2Csrf, nil)
		require.Equal(t, http.StatusForbidden, status)
		status, _ = call(t, srv, http.MethodPost, "/api/firewall", admin2Ck, admin2Csrf,
			map[string]any{"server_id": 2, "tcp_port": "22", "active": "y"})
		require.Equal(t, http.StatusForbidden, status)
	})

	t.Run("client user is blocked", func(t *testing.T) {
		status, _ := call(t, srv, http.MethodGet, "/api/firewall", clientCk, clientCsrf, nil)
		require.Equal(t, http.StatusForbidden, status)
	})

	t.Run("superadmin deletes", func(t *testing.T) {
		status, data := call(t, srv, http.MethodDelete, fmt.Sprintf("/api/firewall/%d", int(fwID)), adminCk, adminCsrf, nil)
		require.Equal(t, http.StatusNoContent, status, "%s", data)
		var dl model.SysDatalog
		require.NoError(t, db.Where("dbtable = 'firewall' AND action = 'd'").
			Order("datalog_id DESC").First(&dl).Error)
		assert.Equal(t, fmt.Sprintf("firewall_id:%d", int(fwID)), dl.DBIdx)
	})
}
