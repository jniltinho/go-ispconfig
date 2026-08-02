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
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"go-ispconfig/internal/api"
	"go-ispconfig/internal/auth"
	"go-ispconfig/internal/config"
	"go-ispconfig/internal/database"
	"go-ispconfig/internal/model"
)

// sitesTestEnv is the shared fixture of the sites API suites: a migrated
// throwaway MariaDB behind the full API stack, with logged-in sessions for
// the admin and two client-level users in separate groups.
type sitesTestEnv struct {
	db  *gorm.DB
	srv *httptest.Server
	// adminCookie/adminCSRF, aCookie/aCSRF (clienta), bCookie/bCSRF (clientb)
	adminCookie, adminCSRF string
	aCookie, aCSRF         string
	bCookie, bCSRF         string
}

// login authenticates a user and returns the session cookie and CSRF token.
func login(t *testing.T, srv *httptest.Server, username, password string) (cookie, csrf string) {
	t.Helper()
	status, data := call(t, srv, http.MethodPost, "/api/login", "", "",
		map[string]any{"username": username, "password": password})
	require.Equal(t, http.StatusOK, status, "login %s: %s", username, data)
	var resp api.LoginResponse
	require.NoError(t, json.Unmarshal(data, &resp))
	return resp.SessionID, resp.CSRFToken
}

// newSitesTestEnv boots the fixture. name isolates the MariaDB container.
func newSitesTestEnv(t *testing.T, name string) *sitesTestEnv {
	t.Helper()
	dsnPrefix, container := database.StartMariaDB(t, name)
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
	srv := httptest.NewServer(e)
	t.Cleanup(srv.Close)

	// Two client-level users in separate groups for cross-client checks.
	hash, err := auth.HashPassword("client-pw")
	require.NoError(t, err)
	for i, username := range []string{"clienta", "clientb"} {
		group := model.SysGroup{Name: username, ClientID: uint32(i + 1)}
		require.NoError(t, db.Create(&group).Error)
		user := model.SysUser{
			Username: username, Passwort: hash, Typ: "user", Active: 1,
			Language: "en", Groups: fmt.Sprint(group.GroupID), DefaultGroup: group.GroupID,
		}
		require.NoError(t, db.Create(&user).Error)
	}

	env := &sitesTestEnv{db: db, srv: srv}
	env.adminCookie, env.adminCSRF = login(t, srv, "admin", "smoke-test-pw")
	env.aCookie, env.aCSRF = login(t, srv, "clienta", "client-pw")
	env.bCookie, env.bCSRF = login(t, srv, "clientb", "client-pw")
	return env
}

// createDomain creates a web domain through the API and returns its id.
func (env *sitesTestEnv) createDomain(t *testing.T, cookie, csrf string, body map[string]any) float64 {
	t.Helper()
	status, data := call(t, env.srv, http.MethodPost, "/api/sites/web-domains", cookie, csrf, body)
	require.Equal(t, http.StatusCreated, status, "%s", data)
	var rec map[string]any
	require.NoError(t, json.Unmarshal(data, &rec))
	return rec["domain_id"].(float64)
}

// TestSitesWebDomainAPI covers /api/sites/web-domains (task 6.2): CRUD with
// derived defaults and transactional datalog, 422 validation including the
// nginx_directives blacklist, cross-client denial, and the datalog
// pending/error state + access-filtered metadata surfaces (task 6.4).
func TestSitesWebDomainAPI(t *testing.T) {
	env := newSitesTestEnv(t, "sites")
	db, srv := env.db, env.srv

	var adminDomainID, clientDomainID float64

	t.Run("admin create derives document_root and journals datalog", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/sites/web-domains", env.adminCookie, env.adminCSRF,
			map[string]any{"server_id": 1, "domain": "Example.COM", "type": "vhost"})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		adminDomainID = rec["domain_id"].(float64)

		require.Equal(t, "example.com", rec["domain"], "TOLOWER filter applied")
		id := int(adminDomainID)
		require.Equal(t, fmt.Sprintf("/var/www/clients/client0/web%d", id), rec["document_root"])
		require.Equal(t, fmt.Sprintf("web%d", id), rec["system_user"])
		require.Equal(t, "client0", rec["system_group"])
		require.Equal(t, "php-fpm", rec["php"], "default applied")
		require.Equal(t, "y", rec["active"], "default applied")

		var dl model.SysDatalog
		require.NoError(t, db.Where("dbtable = ? AND action = 'i'", "web_domain").
			Order("datalog_id DESC").First(&dl).Error)
		require.Equal(t, fmt.Sprintf("domain_id:%d", id), dl.DBIdx)
		require.Contains(t, dl.Data, `"old":{}`)
		require.Contains(t, dl.Data, rec["document_root"],
			"derived fields must be part of the journaled insert")
	})

	t.Run("pending state until the daemon processed the row", func(t *testing.T) {
		status, data := call(t, srv, http.MethodGet,
			fmt.Sprintf("/api/sites/web-domains/%d", int(adminDomainID)), env.adminCookie, "", nil)
		require.Equal(t, http.StatusOK, status)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		require.Equal(t, "pending", rec["_datalog_state"])
	})

	t.Run("validation failures are 422 without record or datalog row", func(t *testing.T) {
		var before int64
		require.NoError(t, db.Model(&model.SysDatalog{}).Count(&before).Error)

		status, data := call(t, srv, http.MethodPost, "/api/sites/web-domains", env.adminCookie, env.adminCSRF,
			map[string]any{"server_id": 1, "type": "vhost"})
		require.Equal(t, http.StatusUnprocessableEntity, status)
		require.Contains(t, errKeyOf(t, data).Fields["domain"], "domain_error_empty")

		status, data = call(t, srv, http.MethodPost, "/api/sites/web-domains", env.adminCookie, env.adminCSRF,
			map[string]any{"server_id": 1, "domain": "not a domain", "type": "vhost"})
		require.Equal(t, http.StatusUnprocessableEntity, status)
		require.Contains(t, errKeyOf(t, data).Fields["domain"], "domain_error_regex")

		status, data = call(t, srv, http.MethodPost, "/api/sites/web-domains", env.adminCookie, env.adminCSRF,
			map[string]any{"server_id": 1, "domain": "evil.com", "type": "vhost",
				"nginx_directives": "load_module /tmp/evil.so;"})
		require.Equal(t, http.StatusUnprocessableEntity, status)
		fields := errKeyOf(t, data).Fields["nginx_directives"]
		require.Len(t, fields, 1)
		require.Contains(t, fields[0], "load_module", "error must name the blocked directive")

		var after int64
		require.NoError(t, db.Model(&model.SysDatalog{}).Count(&after).Error)
		require.Equal(t, before, after, "no datalog row for rejected writes")
		var n int64
		require.NoError(t, db.Model(&model.WebDomain{}).Where("domain = ?", "evil.com").Count(&n).Error)
		require.Zero(t, n)
	})

	t.Run("client create is stamped and derives client system_group", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/sites/web-domains", env.aCookie, env.aCSRF,
			map[string]any{"server_id": 1, "domain": "clienta.com", "type": "vhost",
				"document_root": "/etc/hacked"})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		clientDomainID = rec["domain_id"].(float64)

		require.Equal(t, "client1", rec["system_group"], "client_id resolved from sys_group")
		require.Equal(t,
			fmt.Sprintf("/var/www/clients/client1/web%d", int(clientDomainID)), rec["document_root"],
			"admin-only document_root from the body must be ignored for clients")
	})

	t.Run("cross-client access is denied", func(t *testing.T) {
		path := fmt.Sprintf("/api/sites/web-domains/%d", int(clientDomainID))
		status, _ := call(t, srv, http.MethodGet, path, env.bCookie, "", nil)
		require.Equal(t, http.StatusForbidden, status)

		status, _ = call(t, srv, http.MethodPut, path, env.bCookie, env.bCSRF, map[string]any{"active": "n"})
		require.Equal(t, http.StatusForbidden, status)

		status, _ = call(t, srv, http.MethodDelete, path, env.bCookie, env.bCSRF, nil)
		require.Equal(t, http.StatusForbidden, status)

		// clientb cannot attach a subdomain to clienta's site either.
		status, _ = call(t, srv, http.MethodPost, "/api/sites/web-domains", env.bCookie, env.bCSRF,
			map[string]any{"domain": "sub.clienta.com", "type": "vhostsubdomain",
				"parent_domain_id": clientDomainID, "web_folder": "sub"})
		require.Equal(t, http.StatusForbidden, status)

		// The list only shows clientb's own records.
		status, data := call(t, srv, http.MethodGet, "/api/sites/web-domains", env.bCookie, "", nil)
		require.Equal(t, http.StatusOK, status)
		var list api.ListResponse
		require.NoError(t, json.Unmarshal(data, &list))
		require.Zero(t, list.Total, "no cross-client data leaks into the list")
	})

	t.Run("vhostsubdomain inherits from its parent", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/sites/web-domains", env.aCookie, env.aCSRF,
			map[string]any{"domain": "sub.clienta.com", "type": "vhostsubdomain",
				"parent_domain_id": clientDomainID, "web_folder": "sub"})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		require.EqualValues(t, 1, rec["server_id"], "server inherited from parent")
		require.Equal(t,
			fmt.Sprintf("/var/www/clients/client1/web%d", int(clientDomainID)), rec["document_root"])
		require.Equal(t, fmt.Sprintf("web%d", int(clientDomainID)), rec["system_user"])
	})

	t.Run("update journals a diff", func(t *testing.T) {
		path := fmt.Sprintf("/api/sites/web-domains/%d", int(clientDomainID))
		status, data := call(t, srv, http.MethodPut, path, env.aCookie, env.aCSRF,
			map[string]any{"ssl": "y", "ssl_letsencrypt": "y"})
		require.Equal(t, http.StatusOK, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		require.Equal(t, "y", rec["ssl"])

		var dl model.SysDatalog
		require.NoError(t, db.Where("dbtable = ? AND action = 'u'", "web_domain").
			Order("datalog_id DESC").First(&dl).Error)
		require.Contains(t, dl.Data, "ssl_letsencrypt")
	})

	t.Run("datalog error state surfaces on reads", func(t *testing.T) {
		// Simulate the daemon: process everything, quarantining the last row.
		var last model.SysDatalog
		require.NoError(t, db.Order("datalog_id DESC").First(&last).Error)
		require.NoError(t, db.Model(&model.Server{}).Where("server_id = 1").
			Update("updated", last.DatalogID).Error)
		require.NoError(t, db.Model(&model.SysDatalog{}).
			Where("datalog_id = ?", last.DatalogID).
			Updates(map[string]any{"status": "error", "error": "nginx: -t failed"}).Error)

		status, data := call(t, srv, http.MethodGet,
			fmt.Sprintf("/api/sites/web-domains/%d", int(clientDomainID)), env.aCookie, "", nil)
		require.Equal(t, http.StatusOK, status)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		require.Equal(t, "error", rec["_datalog_state"])
		require.Equal(t, "nginx: -t failed", rec["_datalog_error"])

		// The list carries the same indicator.
		status, data = call(t, srv, http.MethodGet, "/api/sites/web-domains?domain=clienta", env.aCookie, "", nil)
		require.Equal(t, http.StatusOK, status)
		var list api.ListResponse
		require.NoError(t, json.Unmarshal(data, &list))
		require.NotEmpty(t, list.Items)
		require.Equal(t, "error", list.Items[0]["_datalog_state"])
	})

	t.Run("form metadata hides admin-only tab from clients", func(t *testing.T) {
		tabNames := func(cookie string) []string {
			status, data := call(t, srv, http.MethodGet, "/api/meta/forms/web-domains", cookie, "", nil)
			require.Equal(t, http.StatusOK, status)
			var meta api.FormMeta
			require.NoError(t, json.Unmarshal(data, &meta))
			names := make([]string, len(meta.Tabs))
			for i, tab := range meta.Tabs {
				names[i] = tab.Name
			}
			return names
		}
		require.Contains(t, tabNames(env.adminCookie), "advanced")
		require.NotContains(t, tabNames(env.aCookie), "advanced",
			"Options tab must be hidden from client-level users")
	})

	t.Run("delete journals datalog", func(t *testing.T) {
		status, _ := call(t, srv, http.MethodDelete,
			fmt.Sprintf("/api/sites/web-domains/%d", int(adminDomainID)), env.adminCookie, env.adminCSRF, nil)
		require.Equal(t, http.StatusNoContent, status)

		var dl model.SysDatalog
		require.NoError(t, db.Where("dbtable = ? AND action = 'd'", "web_domain").
			Order("datalog_id DESC").First(&dl).Error)
		require.Equal(t, fmt.Sprintf("domain_id:%d", int(adminDomainID)), dl.DBIdx)
	})
}
