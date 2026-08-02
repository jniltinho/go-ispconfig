//go:build integration

package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/api"
	"go-ispconfig/internal/model"
)

// TestSitesFolderAPI covers /api/sites/web-folders and
// /api/sites/web-folder-users (task 6.3): CRUD with server_id derived from
// the parent record, crypted folder-user passwords, 422 validation and
// cross-client denial.
func TestSitesFolderAPI(t *testing.T) {
	env := newSitesTestEnv(t, "sitesfld")
	db, srv := env.db, env.srv

	domainID := env.createDomain(t, env.aCookie, env.aCSRF,
		map[string]any{"server_id": 1, "domain": "clienta.com", "type": "vhost"})

	var folderID, folderUserID float64

	t.Run("folder create derives server_id from the parent domain", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/sites/web-folders", env.aCookie, env.aCSRF,
			map[string]any{"parent_domain_id": domainID, "path": "protected"})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		folderID = rec["web_folder_id"].(float64)
		require.EqualValues(t, 1, rec["server_id"])
		require.Equal(t, "y", rec["active"], "default applied")
		require.Equal(t, "protected", rec["path"])

		var dl model.SysDatalog
		require.NoError(t, db.Where("dbtable = ? AND action = 'i'", "web_folder").
			Order("datalog_id DESC").First(&dl).Error)
		require.Equal(t, fmt.Sprintf("web_folder_id:%d", int(folderID)), dl.DBIdx)
	})

	t.Run("folder validation is 422", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/sites/web-folders", env.aCookie, env.aCSRF,
			map[string]any{"parent_domain_id": domainID, "path": "bad path!"})
		require.Equal(t, http.StatusUnprocessableEntity, status)
		require.Contains(t, errKeyOf(t, data).Fields["path"], "path_error_regex")
	})

	t.Run("cross-client folder create is denied", func(t *testing.T) {
		status, _ := call(t, srv, http.MethodPost, "/api/sites/web-folders", env.bCookie, env.bCSRF,
			map[string]any{"parent_domain_id": domainID, "path": "x"})
		require.Equal(t, http.StatusForbidden, status)

		status, _ = call(t, srv, http.MethodGet,
			fmt.Sprintf("/api/sites/web-folders/%d", int(folderID)), env.bCookie, "", nil)
		require.Equal(t, http.StatusForbidden, status)
	})

	t.Run("folder user password is stored crypted", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/sites/web-folder-users", env.aCookie, env.aCSRF,
			map[string]any{"web_folder_id": folderID, "username": "user1", "password": "secret123"})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		folderUserID = rec["web_folder_user_id"].(float64)
		require.EqualValues(t, 1, rec["server_id"], "server derived from the folder")

		var stored model.WebFolderUser
		require.NoError(t, db.First(&stored, int(folderUserID)).Error)
		require.True(t, strings.HasPrefix(stored.Password, "$6$"),
			"password must be SHA-512 crypt, got %q", stored.Password)
		require.NotContains(t, stored.Password, "secret123")

		var dl model.SysDatalog
		require.NoError(t, db.Where("dbtable = ?", "web_folder_user").
			Order("datalog_id DESC").First(&dl).Error)
		require.Equal(t, "i", dl.Action, "datalog row triggers the auth-file rebuild")
	})

	t.Run("folder user update keeps the stored hash without a new password", func(t *testing.T) {
		path := fmt.Sprintf("/api/sites/web-folder-users/%d", int(folderUserID))
		var before model.WebFolderUser
		require.NoError(t, db.First(&before, int(folderUserID)).Error)

		status, _ := call(t, srv, http.MethodPut, path, env.aCookie, env.aCSRF,
			map[string]any{"active": "n"})
		require.Equal(t, http.StatusOK, status)

		var after model.WebFolderUser
		require.NoError(t, db.First(&after, int(folderUserID)).Error)
		require.Equal(t, before.Password, after.Password)
		require.Equal(t, "n", after.Active)
	})

	t.Run("folder user validation is 422", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/sites/web-folder-users", env.aCookie, env.aCSRF,
			map[string]any{"web_folder_id": folderID, "username": "bad user!", "password": ""})
		require.Equal(t, http.StatusUnprocessableEntity, status)
		info := errKeyOf(t, data)
		require.Contains(t, info.Fields["username"], "username_error_regex")
		require.Contains(t, info.Fields["password"], "password_error_empty")
	})

	t.Run("folder user list filters by web_folder_id", func(t *testing.T) {
		status, data := call(t, srv, http.MethodGet,
			fmt.Sprintf("/api/sites/web-folder-users?web_folder_id=%d", int(folderID)), env.aCookie, "", nil)
		require.Equal(t, http.StatusOK, status)
		var list api.ListResponse
		require.NoError(t, json.Unmarshal(data, &list))
		require.EqualValues(t, 1, list.Total)
	})

	t.Run("folder user delete journals datalog", func(t *testing.T) {
		status, _ := call(t, srv, http.MethodDelete,
			fmt.Sprintf("/api/sites/web-folder-users/%d", int(folderUserID)), env.aCookie, env.aCSRF, nil)
		require.Equal(t, http.StatusNoContent, status)

		var dl model.SysDatalog
		require.NoError(t, db.Where("dbtable = ? AND action = 'd'", "web_folder_user").
			Order("datalog_id DESC").First(&dl).Error)
	})
}
