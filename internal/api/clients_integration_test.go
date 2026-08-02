//go:build integration

package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
	echoMiddleware "github.com/labstack/echo/v5/middleware"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"go-ispconfig/internal/api"
	"go-ispconfig/internal/auth"
	"go-ispconfig/internal/clients"
	"go-ispconfig/internal/config"
	"go-ispconfig/internal/database"
	"go-ispconfig/internal/model"
	"go-ispconfig/internal/repository"
)

// newClientsTestEnv boots a migrated MariaDB behind the full API stack
// with the client limit hook wired (as cmd/serve does).
func newClientsTestEnv(t *testing.T) (*gorm.DB, *httptest.Server, string, string) {
	db, srv, cookie, csrf, _ := newClientsTestEnvDeps(t)
	return db, srv, cookie, csrf
}

// newClientsTestEnvDeps additionally exposes Deps so tests can inject a
// fake Mailer after registration.
func newClientsTestEnvDeps(t *testing.T) (*gorm.DB, *httptest.Server, string, string, *api.Deps) {
	t.Helper()
	dsnPrefix, container := database.StartMariaDB(t, "clientsapi")
	database.MariaDBExec(t, container, "CREATE DATABASE ispconfig CHARACTER SET utf8mb4")
	db, err := database.Open(dsnPrefix + "/ispconfig?charset=utf8mb4&parseTime=True&loc=Local")
	require.NoError(t, err)
	needSeed, err := database.Migrate(db)
	require.NoError(t, err)
	require.True(t, needSeed)
	_, err = database.Seed(db, "server1.example.com", "smoke-test-pw")
	require.NoError(t, err)

	api.RegisterLimitHook(clients.LimitHook(db))
	t.Cleanup(func() {
		api.RegisterLimitHook(func(context.Context, string, *repository.Identity, map[string]any) error { return nil })
	})

	e := echo.New()
	e.Use(echoMiddleware.Recover())
	deps := &api.Deps{DB: db, Sessions: auth.NewStore(db, 0), Config: &config.Config{}}
	require.NoError(t, api.Register(e, deps))
	srv := httptest.NewServer(e)
	t.Cleanup(srv.Close)

	adminCookie, adminCSRF := login(t, srv, "admin", "smoke-test-pw")
	return db, srv, adminCookie, adminCSRF, deps
}

func TestClientsAPI(t *testing.T) {
	db, srv, adminCookie, adminCSRF := newClientsTestEnv(t)

	var resellerID, childID float64
	var resellerGroup uint32

	t.Run("admin creates a reseller with provisioned identity", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/resellers", adminCookie, adminCSRF,
			map[string]any{
				"contact_name": "Res Ellen", "username": "resellen",
				"password": "res-pw-longenough", "email": "res@example.com",
			})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		resellerID = rec["client_id"].(float64)
		require.NotContains(t, rec, "password", "credential fields must be redacted")
		require.NotContains(t, rec, "id_rsa")
		require.EqualValues(t, 100, rec["limit_client"], "reseller.tform default")

		var grp model.SysGroup
		require.NoError(t, db.Where("client_id = ?", resellerID).Take(&grp).Error)
		resellerGroup = grp.GroupID
		var u model.SysUser
		require.NoError(t, db.Where("username = ?", "resellen").Take(&u).Error)
		require.EqualValues(t, resellerID, u.ClientID)
		require.Contains(t, u.Modules, "client", "resellers carry the client module")
		require.True(t, strings.HasPrefix(u.Passwort, "$2"), "bcrypt hash stored")

		var dl model.SysDatalog
		require.NoError(t, db.Where("dbtable = 'client' AND action = 'i'").
			Order("datalog_id DESC").First(&dl).Error)
		require.Equal(t, fmt.Sprintf("client_id:%d", int(resellerID)), dl.DBIdx)
		require.NotContains(t, dl.Data, "res-pw-longenough", "plaintext never journaled")
	})

	t.Run("create validation: empty username and duplicate username are 422", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/clients", adminCookie, adminCSRF,
			map[string]any{"contact_name": "X", "password": "x-pw-longenough"})
		require.Equal(t, http.StatusUnprocessableEntity, status, "%s", data)
		require.Contains(t, string(data), "username_error_empty")

		status, data = call(t, srv, http.MethodPost, "/api/clients", adminCookie, adminCSRF,
			map[string]any{"contact_name": "X", "username": "resellen", "password": "x-pw-longenough"})
		require.Equal(t, http.StatusUnprocessableEntity, status, "%s", data)
		require.Contains(t, string(data), "username_error_unique")
	})

	t.Run("nested reseller is rejected", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/resellers", adminCookie, adminCSRF,
			map[string]any{
				"contact_name": "Nested", "username": "nested",
				"password": "nested-pw-long", "parent_client_id": resellerID,
			})
		require.Equal(t, http.StatusUnprocessableEntity, status, "%s", data)
		require.Contains(t, string(data), "error.reseller_cannot_have_parent")
	})

	t.Run("admin creates a client under the reseller (re-owned to its group)", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/clients", adminCookie, adminCSRF,
			map[string]any{
				"contact_name": "Kid One", "username": "kidone",
				"password": "kid-pw-longenough", "parent_client_id": resellerID,
			})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		var row model.Client
		require.NoError(t, db.Take(&row, rec["client_id"]).Error)
		require.Equal(t, resellerGroup, row.SysGroupID, "D3.5 parent re-own")
	})

	resCookie, resCSRF := login(t, srv, "resellen", "res-pw-longenough")

	t.Run("reseller creates a client; parent is forced to the reseller", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/clients", resCookie, resCSRF,
			map[string]any{
				"contact_name": "Kid Two", "username": "kidtwo",
				"password": "kid2-pw-longenough",
			})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		childID = rec["client_id"].(float64)
		require.EqualValues(t, resellerID, rec["parent_client_id"])
	})

	t.Run("reseller cannot reach the reseller surface", func(t *testing.T) {
		status, _ := call(t, srv, http.MethodGet, "/api/resellers", resCookie, "", nil)
		require.Equal(t, http.StatusForbidden, status)
	})

	t.Run("cross-tenant access is denied", func(t *testing.T) {
		// kidone is admin-owned... create an admin-owned top-level client
		// and verify the reseller cannot read or update it.
		status, data := call(t, srv, http.MethodPost, "/api/clients", adminCookie, adminCSRF,
			map[string]any{
				"contact_name": "Loner", "username": "loner", "password": "loner-pw-long",
			})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		lonerID := int(rec["client_id"].(float64))

		status, _ = call(t, srv, http.MethodGet, fmt.Sprintf("/api/clients/%d", lonerID), resCookie, "", nil)
		require.Equal(t, http.StatusForbidden, status)
		status, _ = call(t, srv, http.MethodPut, fmt.Sprintf("/api/clients/%d", lonerID), resCookie, resCSRF,
			map[string]any{"contact_name": "Hacked"})
		require.Equal(t, http.StatusForbidden, status)
		status, _ = call(t, srv, http.MethodGet, "/api/clients/by-username/loner", resCookie, "", nil)
		require.Equal(t, http.StatusNotFound, status, "inaccessible lookup is 404")
	})

	t.Run("by-id routes never cross the clients/resellers surfaces", func(t *testing.T) {
		status, _ := call(t, srv, http.MethodGet, fmt.Sprintf("/api/clients/%d", int(resellerID)), adminCookie, "", nil)
		require.Equal(t, http.StatusNotFound, status, "reseller hidden on /clients")
		status, _ = call(t, srv, http.MethodGet, fmt.Sprintf("/api/resellers/%d", int(childID)), adminCookie, "", nil)
		require.Equal(t, http.StatusNotFound, status, "client hidden on /resellers")
		status, _ = call(t, srv, http.MethodPut, fmt.Sprintf("/api/clients/%d", int(resellerID)), adminCookie, adminCSRF,
			map[string]any{"contact_name": "X"})
		require.Equal(t, http.StatusNotFound, status)
		status, _ = call(t, srv, http.MethodDelete, fmt.Sprintf("/api/clients/%d", int(resellerID)), adminCookie, adminCSRF, nil)
		require.Equal(t, http.StatusNotFound, status)
	})

	t.Run("child limits are capped to the parent on create", func(t *testing.T) {
		// resellen has default limits (limit_web_domain -1 template-less),
		// so cap against a tighter parent: set it first.
		require.NoError(t, db.Model(&model.Client{}).Where("client_id = ?", resellerID).
			Update("limit_web_domain", 5).Error)
		status, data := call(t, srv, http.MethodPost, "/api/clients", resCookie, resCSRF,
			map[string]any{
				"contact_name": "Greedy", "username": "greedy",
				"password": "greedy-pw-long1", "limit_web_domain": -1,
			})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		require.EqualValues(t, 5, rec["limit_web_domain"], "child -1 clamped to parent's 5")
	})

	t.Run("role-scoped lists", func(t *testing.T) {
		status, data := call(t, srv, http.MethodGet, "/api/clients?limit=100", adminCookie, "", nil)
		require.Equal(t, http.StatusOK, status)
		require.NotContains(t, string(data), `"resellen"`, "resellers excluded from /clients")
		require.Contains(t, string(data), `"kidone"`)
		require.NotContains(t, string(data), `"password"`, "list is redacted")

		status, data = call(t, srv, http.MethodGet, "/api/resellers", adminCookie, "", nil)
		require.Equal(t, http.StatusOK, status)
		require.Contains(t, string(data), `"resellen"`)
		require.NotContains(t, string(data), `"kidone"`)
	})

	t.Run("lookup helpers", func(t *testing.T) {
		status, data := call(t, srv, http.MethodGet, "/api/clients/by-username/kidtwo", adminCookie, "", nil)
		require.Equal(t, http.StatusOK, status)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		require.EqualValues(t, childID, rec["client_id"])
		require.NotContains(t, rec, "password")

		status, _ = call(t, srv, http.MethodGet, "/api/clients/by-username/ghost", adminCookie, "", nil)
		require.Equal(t, http.StatusNotFound, status)

		status, data = call(t, srv, http.MethodGet,
			fmt.Sprintf("/api/clients/by-groupid/%d", resellerGroup), adminCookie, "", nil)
		require.Equal(t, http.StatusOK, status)
		require.NoError(t, json.Unmarshal(data, &rec))
		require.EqualValues(t, resellerID, rec["client_id"])

		var u model.SysUser
		require.NoError(t, db.Where("username = ?", "kidtwo").Take(&u).Error)
		status, data = call(t, srv, http.MethodGet,
			fmt.Sprintf("/api/clients/id-by-sysuser/%d", u.UserID), adminCookie, "", nil)
		require.Equal(t, http.StatusOK, status)
		require.NoError(t, json.Unmarshal(data, &rec))
		require.EqualValues(t, childID, rec["client_id"])
	})

	t.Run("update syncs the login identity", func(t *testing.T) {
		path := fmt.Sprintf("/api/clients/%d", int(childID))
		status, data := call(t, srv, http.MethodPut, path, adminCookie, adminCSRF,
			map[string]any{"username": "kidtwo2", "locked": "y"})
		require.Equal(t, http.StatusOK, status, "%s", data)
		var u model.SysUser
		require.NoError(t, db.Where("client_id = ?", childID).Take(&u).Error)
		require.Equal(t, "kidtwo2", u.Username, "sys_user renamed")
		require.EqualValues(t, 0, u.Active, "locked disables the login")
	})

	t.Run("change-password", func(t *testing.T) {
		path := fmt.Sprintf("/api/clients/%d/change-password", int(childID))
		status, data := call(t, srv, http.MethodPost, path, adminCookie, adminCSRF,
			map[string]any{"password": "short"})
		require.Equal(t, http.StatusUnprocessableEntity, status, "%s", data)
		require.Contains(t, string(data), "password_error_length")

		status, data = call(t, srv, http.MethodPost, path, adminCookie, adminCSRF,
			map[string]any{"password": "brand-new-pw-1"})
		require.Equal(t, http.StatusNoContent, status, "%s", data)

		// Unlock first (locked in the previous subtest), then log in.
		status, _ = call(t, srv, http.MethodPut, fmt.Sprintf("/api/clients/%d", int(childID)),
			adminCookie, adminCSRF, map[string]any{"locked": "n"})
		require.Equal(t, http.StatusOK, status)
		login(t, srv, "kidtwo2", "brand-new-pw-1")

		var row model.Client
		require.NoError(t, db.Take(&row, int(childID)).Error)
		require.True(t, strings.HasPrefix(row.Password, "$2"))
	})

	t.Run("reseller with children cannot be deleted", func(t *testing.T) {
		status, data := call(t, srv, http.MethodDelete,
			fmt.Sprintf("/api/resellers/%d", int(resellerID)), adminCookie, adminCSRF, nil)
		require.Equal(t, http.StatusUnprocessableEntity, status, "%s", data)
		require.Contains(t, string(data), "error.client_has_children")
	})

	t.Run("delete removes the identity and journals", func(t *testing.T) {
		status, _ := call(t, srv, http.MethodDelete,
			fmt.Sprintf("/api/clients/%d", int(childID)), adminCookie, adminCSRF, nil)
		require.Equal(t, http.StatusNoContent, status)
		var n int64
		require.NoError(t, db.Model(&model.SysUser{}).Where("client_id = ?", childID).Count(&n).Error)
		require.Zero(t, n, "sys_user deprovisioned")
		require.NoError(t, db.Model(&model.SysGroup{}).Where("client_id = ?", childID).Count(&n).Error)
		require.Zero(t, n, "sys_group deprovisioned")
		var dl model.SysDatalog
		require.NoError(t, db.Where("dbtable = 'client' AND action = 'd'").
			Order("datalog_id DESC").First(&dl).Error)
		require.Equal(t, fmt.Sprintf("client_id:%d", int(childID)), dl.DBIdx)
	})

	t.Run("delete-everything cascades child clients", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/resellers", adminCookie, adminCSRF,
			map[string]any{"contact_name": "Cascade", "username": "cascade", "password": "cascade-pw-long"})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		cascadeID := int(rec["client_id"].(float64))
		cCookie, cCSRF := login(t, srv, "cascade", "cascade-pw-long")
		status, data = call(t, srv, http.MethodPost, "/api/clients", cCookie, cCSRF,
			map[string]any{"contact_name": "Cascade Kid", "username": "cascadekid", "password": "ckid-pw-long1"})
		require.Equal(t, http.StatusCreated, status, "%s", data)

		status, data = call(t, srv, http.MethodDelete,
			fmt.Sprintf("/api/clients/%d/everything", cascadeID), adminCookie, adminCSRF, nil)
		require.Equal(t, http.StatusNoContent, status, "%s", data)
		var n int64
		require.NoError(t, db.Model(&model.Client{}).
			Where("username IN ?", []string{"cascade", "cascadekid"}).Count(&n).Error)
		require.Zero(t, n, "reseller and child both gone")
		require.NoError(t, db.Model(&model.SysUser{}).
			Where("username IN ?", []string{"cascade", "cascadekid"}).Count(&n).Error)
		require.Zero(t, n)
	})

	t.Run("weak password on create is rejected", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/clients", adminCookie, adminCSRF,
			map[string]any{"contact_name": "Weak", "username": "weakpw", "password": "short"})
		require.Equal(t, http.StatusUnprocessableEntity, status, "%s", data)
		require.Contains(t, string(data), "password_error_length")
	})

	t.Run("resource-counts for the delete confirmation", func(t *testing.T) {
		status, data := call(t, srv, http.MethodGet,
			fmt.Sprintf("/api/clients/%d/resource-counts", int(resellerID)), adminCookie, "", nil)
		require.Equal(t, http.StatusOK, status, "%s", data)
		var counts map[string]int64
		require.NoError(t, json.Unmarshal(data, &counts))
		require.Positive(t, counts["child_clients"], "resellen has children")
		require.Contains(t, counts, "web_domains")
		require.Contains(t, counts, "dns_zones")
	})

	t.Run("delete-everything purges owned resources", func(t *testing.T) {
		// New client owning a DNS zone.
		status, data := call(t, srv, http.MethodPost, "/api/clients", adminCookie, adminCSRF,
			map[string]any{
				"contact_name": "Doomed", "username": "doomed", "password": "doom-pw-long",
			})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		doomedID := int(rec["client_id"].(float64))
		dCookie, dCSRF := login(t, srv, "doomed", "doom-pw-long")
		status, data = call(t, srv, http.MethodPost, "/api/dns/zones", dCookie, dCSRF,
			map[string]any{
				"server_id": 1, "origin": "doomed.example.com.", "ns": "ns1.example.com.",
				"mbox": "hostmaster.example.com.", "active": "Y",
			})
		require.Equal(t, http.StatusCreated, status, "%s", data)

		// Plain delete refuses nothing here, but delete-everything is admin only.
		status, _ = call(t, srv, http.MethodDelete,
			fmt.Sprintf("/api/clients/%d/everything", doomedID), dCookie, dCSRF, nil)
		require.Equal(t, http.StatusForbidden, status)

		status, data = call(t, srv, http.MethodDelete,
			fmt.Sprintf("/api/clients/%d/everything", doomedID), adminCookie, adminCSRF, nil)
		require.Equal(t, http.StatusNoContent, status, "%s", data)

		var n int64
		require.NoError(t, db.Model(&model.DNSSoa{}).Where("origin = ?", "doomed.example.com.").Count(&n).Error)
		require.Zero(t, n, "owned zone purged")
		require.NoError(t, db.Model(&model.Client{}).Where("client_id = ?", doomedID).Count(&n).Error)
		require.Zero(t, n)
		var dl model.SysDatalog
		require.NoError(t, db.Where("dbtable = 'dns_soa' AND action = 'd'").
			Order("datalog_id DESC").First(&dl).Error)
		require.Contains(t, dl.Data, "doomed.example.com.", "purge journaled for the daemon")
	})
}
