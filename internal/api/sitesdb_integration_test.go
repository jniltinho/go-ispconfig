//go:build integration

package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/api"
	"go-ispconfig/internal/clients"
	"go-ispconfig/internal/model"
	"go-ispconfig/internal/repository"
)

// createDatabaseUser creates a database user through the API and returns
// its id.
func createDatabaseUser(t *testing.T, env *sitesTestEnv, cookie, csrf string, body map[string]any) float64 {
	t.Helper()
	status, data := call(t, env.srv, http.MethodPost, "/api/sites/database-users", cookie, csrf, body)
	require.Equal(t, http.StatusCreated, status, "%s", data)
	var rec map[string]any
	require.NoError(t, json.Unmarshal(data, &rec))
	return rec["database_user_id"].(float64)
}

// TestSitesDatabaseAPI covers /api/sites/databases (task 5.1): CRUD with
// parent-group inheritance and datalog journaling, 422 validation,
// immutability guards, cross-client denial and the client limit /
// db_servers vetoes.
func TestSitesDatabaseAPI(t *testing.T) {
	env := newSitesTestEnv(t, "sitesdb")
	db, srv := env.db, env.srv

	// The serve command wires the client limit hook at startup; do the
	// same here (and restore the permissive default afterwards).
	api.RegisterLimitHook(clients.LimitHook(db))
	t.Cleanup(func() {
		api.RegisterLimitHook(func(context.Context, string, *repository.Identity, map[string]any) error {
			return nil
		})
	})

	domainID := env.createDomain(t, env.aCookie, env.aCSRF,
		map[string]any{"server_id": 1, "domain": "clienta-db.com", "type": "vhost"})
	userID := createDatabaseUser(t, env, env.aCookie, env.aCSRF,
		map[string]any{"database_user": "c1app", "database_password": "Sup3r-Secret!"})

	var databaseID float64

	t.Run("create inherits the site group and journals datalog", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/sites/databases", env.aCookie, env.aCSRF,
			map[string]any{"server_id": 1, "parent_domain_id": domainID,
				"database_name": "app_db", "database_user_id": userID})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		databaseID = rec["database_id"].(float64)
		require.Equal(t, "app_db", rec["database_name"], "no prefix configured")
		require.Equal(t, "mysql", rec["type"], "default applied")
		require.Equal(t, "y", rec["active"], "default applied")
		require.Equal(t, "n", rec["remote_access"], "tform default applied")

		var domain model.WebDomain
		require.NoError(t, db.First(&domain, int(domainID)).Error)
		require.EqualValues(t, domain.SysGroupID, rec["sys_groupid"],
			"database owned by the parent site's group")

		var dl model.SysDatalog
		require.NoError(t, db.Where("dbtable = ? AND action = 'i'", "web_database").
			Order("datalog_id DESC").First(&dl).Error)
		require.Equal(t, fmt.Sprintf("database_id:%d", int(databaseID)), dl.DBIdx)
	})

	t.Run("validation is 422 with field keys", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/sites/databases", env.aCookie, env.aCSRF,
			map[string]any{"server_id": 1, "parent_domain_id": domainID, "database_name": "a"})
		require.Equal(t, http.StatusUnprocessableEntity, status)
		require.Contains(t, errKeyOf(t, data).Fields["database_name"], "database_name_error_regex")

		status, data = call(t, srv, http.MethodPost, "/api/sites/databases", env.aCookie, env.aCSRF,
			map[string]any{"server_id": 1, "parent_domain_id": domainID,
				"database_name": "chdb", "database_charset": "latin2"})
		require.Equal(t, http.StatusUnprocessableEntity, status)
		require.Contains(t, errKeyOf(t, data).Fields["database_charset"], "database_charset_error_regex")

		status, data = call(t, srv, http.MethodPost, "/api/sites/databases", env.aCookie, env.aCSRF,
			map[string]any{"server_id": 1, "parent_domain_id": domainID, "database_name": "ipdb",
				"remote_access": "y", "remote_ips": "not valid!"})
		require.Equal(t, http.StatusUnprocessableEntity, status)
		require.Contains(t, errKeyOf(t, data).Fields["remote_ips"], "database_remote_error_ips")

		status, data = call(t, srv, http.MethodPost, "/api/sites/databases", env.aCookie, env.aCSRF,
			map[string]any{"server_id": 1, "parent_domain_id": domainID, "database_name": "mysql"})
		require.Equal(t, http.StatusUnprocessableEntity, status)
		require.Contains(t, errKeyOf(t, data).Fields["database_name"], "database_name_error_blacklist")

		// duplicate name on the same server
		status, data = call(t, srv, http.MethodPost, "/api/sites/databases", env.aCookie, env.aCSRF,
			map[string]any{"server_id": 1, "parent_domain_id": domainID,
				"database_name": "app_db", "database_user_id": userID})
		require.Equal(t, http.StatusUnprocessableEntity, status)
		require.Contains(t, errKeyOf(t, data).Fields["database_name"], "database_name_error_unique")
	})

	t.Run("cross-client access is denied", func(t *testing.T) {
		status, _ := call(t, srv, http.MethodPost, "/api/sites/databases", env.bCookie, env.bCSRF,
			map[string]any{"server_id": 1, "parent_domain_id": domainID, "database_name": "steal_db"})
		require.Equal(t, http.StatusForbidden, status, "parent of another client")

		status, _ = call(t, srv, http.MethodGet,
			fmt.Sprintf("/api/sites/databases/%d", int(databaseID)), env.bCookie, "", nil)
		require.Equal(t, http.StatusForbidden, status)
	})

	t.Run("charset and name are immutable for clients", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPut,
			fmt.Sprintf("/api/sites/databases/%d", int(databaseID)), env.aCookie, env.aCSRF,
			map[string]any{"database_charset": "utf8"})
		require.Equal(t, http.StatusUnprocessableEntity, status)
		require.Contains(t, errKeyOf(t, data).Fields["database_charset"], "database_charset_change_error")

		status, data = call(t, srv, http.MethodPut,
			fmt.Sprintf("/api/sites/databases/%d", int(databaseID)), env.aCookie, env.aCSRF,
			map[string]any{"database_name": "renamed_db"})
		require.Equal(t, http.StatusUnprocessableEntity, status)
		require.Contains(t, errKeyOf(t, data).Fields["database_name"], "database_name_change_error")

		// admin may rename; the journal carries the rename diff
		status, data = call(t, srv, http.MethodPut,
			fmt.Sprintf("/api/sites/databases/%d", int(databaseID)), env.adminCookie, env.adminCSRF,
			map[string]any{"database_name": "renamed_db"})
		require.Equal(t, http.StatusOK, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		require.Equal(t, "renamed_db", rec["database_name"])
	})

	t.Run("deactivating journals an update", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPut,
			fmt.Sprintf("/api/sites/databases/%d", int(databaseID)), env.aCookie, env.aCSRF,
			map[string]any{"active": "n"})
		require.Equal(t, http.StatusOK, status, "%s", data)
		var dl model.SysDatalog
		require.NoError(t, db.Where("dbtable = ? AND action = 'u'", "web_database").
			Order("datalog_id DESC").First(&dl).Error)
		require.Contains(t, dl.Data, `"active":"n"`)
	})

	t.Run("client limits and db_servers veto creates", func(t *testing.T) {
		// Attach a real client row to clienta: server allow-list {1}.
		client := model.Client{Username: "clienta", ContactName: "Client A",
			LimitDatabase: -1, LimitDatabaseUser: -1, LimitDatabaseQuota: -1,
			DBServers: "1"}
		require.NoError(t, db.Create(&client).Error)
		require.NoError(t, db.Model(&model.SysUser{}).Where("username = ?", "clienta").
			Update("client_id", client.ClientID).Error)
		require.NoError(t, db.Model(&model.SysGroup{}).Where("name = ?", "clienta").
			Update("client_id", client.ClientID).Error)

		status, data := call(t, srv, http.MethodPost, "/api/sites/databases", env.aCookie, env.aCSRF,
			map[string]any{"server_id": 7, "parent_domain_id": domainID, "database_name": "srv_db"})
		require.Equal(t, http.StatusForbidden, status, "%s", data)
		require.Equal(t, "error.not_allowed_server_id", errKeyOf(t, data).Key)

		// One mysql database exists already: limit_database = 1 vetoes.
		require.NoError(t, db.Model(&model.Client{}).Where("client_id = ?", client.ClientID).
			Update("limit_database", 1).Error)
		status, data = call(t, srv, http.MethodPost, "/api/sites/databases", env.aCookie, env.aCSRF,
			map[string]any{"server_id": 1, "parent_domain_id": domainID, "database_name": "over_db"})
		require.Equal(t, http.StatusForbidden, status, "%s", data)
		require.Equal(t, "error.limit_database", errKeyOf(t, data).Key)

		// Finite quota rejects an unlimited (-1) database.
		require.NoError(t, db.Model(&model.Client{}).Where("client_id = ?", client.ClientID).
			Updates(map[string]any{"limit_database": -1, "limit_database_quota": 100}).Error)
		status, data = call(t, srv, http.MethodPost, "/api/sites/databases", env.aCookie, env.aCSRF,
			map[string]any{"server_id": 1, "parent_domain_id": domainID,
				"database_name": "quota_db", "database_quota": -1})
		require.Equal(t, http.StatusForbidden, status, "%s", data)
		require.Equal(t, "error.limit_database_quota", errKeyOf(t, data).Key)

		require.NoError(t, db.Model(&model.Client{}).Where("client_id = ?", client.ClientID).
			Update("limit_database_quota", -1).Error)
	})

	t.Run("delete journals a d row", func(t *testing.T) {
		status, _ := call(t, srv, http.MethodDelete,
			fmt.Sprintf("/api/sites/databases/%d", int(databaseID)), env.aCookie, env.aCSRF, nil)
		require.Equal(t, http.StatusNoContent, status)
		var dl model.SysDatalog
		require.NoError(t, db.Where("dbtable = ? AND action = 'd'", "web_database").
			Order("datalog_id DESC").First(&dl).Error)
		require.Equal(t, fmt.Sprintf("database_id:%d", int(databaseID)), dl.DBIdx)
	})
}
