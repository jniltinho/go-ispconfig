//go:build integration

package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/api"
	"go-ispconfig/internal/clients"
	"go-ispconfig/internal/model"
	"go-ispconfig/internal/repository"
)

// TestSitesFTPUserAPI covers /api/sites/ftp-users (tasks 5.1 + 5.3): create
// with parent-derived fields, CRYPT password, password redaction, 422
// validation, cross-client 403, datalog journaling and client limits.
func TestSitesFTPUserAPI(t *testing.T) {
	env := newSitesTestEnv(t, "sitesftp")
	db, srv := env.db, env.srv

	api.RegisterLimitHook(clients.LimitHook(db))
	t.Cleanup(func() {
		api.RegisterLimitHook(func(context.Context, string, *repository.Identity, map[string]any) error {
			return nil
		})
	})

	domainID := env.createDomain(t, env.aCookie, env.aCSRF,
		map[string]any{"server_id": 1, "domain": "clienta-ftp.com", "type": "vhost"})

	var ftpID float64

	t.Run("create derives parent fields and journals datalog", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/sites/ftp-users", env.aCookie, env.aCSRF,
			map[string]any{
				"parent_domain_id": domainID,
				"username":         "alice",
				"password":         "Sup3r-Secret!",
			})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		ftpID = rec["ftp_user_id"].(float64)
		require.Equal(t, "clientaalice", rec["username"], "seeded ftpuser_prefix [CLIENTNAME] prepends the owning group name")
		require.Equal(t, "y", rec["active"], "default applied")
		require.EqualValues(t, -1, rec["quota_size"])
		require.NotContains(t, rec, "password", "password redacted on read")

		var domain model.WebDomain
		require.NoError(t, db.First(&domain, int(domainID)).Error)
		require.EqualValues(t, domain.SysGroupID, rec["sys_groupid"])
		require.EqualValues(t, domain.ServerID, rec["server_id"])
		require.Equal(t, domain.SystemUser, rec["uid"])
		require.Equal(t, domain.SystemGroup, rec["gid"])
		require.Equal(t, domain.DocumentRoot, rec["dir"])
		require.Equal(t, domain.Domain, rec["_parent_domain"])
		require.NotEmpty(t, rec["_server_name"])

		var stored model.FTPUser
		require.NoError(t, db.First(&stored, int(ftpID)).Error)
		require.True(t, strings.HasPrefix(stored.Password, "$6$") || strings.HasPrefix(stored.Password, "$5$") ||
			strings.HasPrefix(stored.Password, "$1$"),
			"password must be CRYPT, got %q", stored.Password)
		require.NotContains(t, stored.Password, "Sup3r-Secret!")

		var dl model.SysDatalog
		require.NoError(t, db.Where("dbtable = ? AND action = 'i'", "ftp_user").
			Order("datalog_id DESC").First(&dl).Error)
		require.Equal(t, fmt.Sprintf("ftp_user_id:%d", int(ftpID)), dl.DBIdx)
		require.EqualValues(t, domain.ServerID, dl.ServerID)
	})

	t.Run("validation is 422 with field keys", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/sites/ftp-users", env.aCookie, env.aCSRF,
			map[string]any{"parent_domain_id": domainID, "username": "bob"})
		require.Equal(t, http.StatusUnprocessableEntity, status)
		require.Contains(t, errKeyOf(t, data).Fields["password"], "password_error_empty")

		status, data = call(t, srv, http.MethodPost, "/api/sites/ftp-users", env.aCookie, env.aCSRF,
			map[string]any{
				"parent_domain_id": domainID,
				"username":         "has space",
				"password":         "Sup3r-Secret!",
			})
		require.Equal(t, http.StatusUnprocessableEntity, status)
		require.Contains(t, errKeyOf(t, data).Fields["username"], "username_error_regex")

		// dir outside docroot
		status, data = call(t, srv, http.MethodPost, "/api/sites/ftp-users", env.aCookie, env.aCSRF,
			map[string]any{
				"parent_domain_id": domainID,
				"username":         "outpath",
				"password":         "Sup3r-Secret!",
				"dir":              "/etc/passwd",
			})
		require.Equal(t, http.StatusUnprocessableEntity, status, "%s", data)
		require.Contains(t, errKeyOf(t, data).Fields["dir"], "directory_error_notinweb")

		// weak password
		status, data = call(t, srv, http.MethodPost, "/api/sites/ftp-users", env.aCookie, env.aCSRF,
			map[string]any{
				"parent_domain_id": domainID,
				"username":         "weakpw",
				"password":         "short",
			})
		require.Equal(t, http.StatusUnprocessableEntity, status)
		require.Contains(t, errKeyOf(t, data).Fields["password"], "weak_password_txt")

		// duplicate username
		status, data = call(t, srv, http.MethodPost, "/api/sites/ftp-users", env.aCookie, env.aCSRF,
			map[string]any{
				"parent_domain_id": domainID,
				"username":         "alice",
				"password":         "Sup3r-Secret!",
			})
		require.Equal(t, http.StatusUnprocessableEntity, status)
		require.Contains(t, errKeyOf(t, data).Fields["username"], "username_error_unique")
	})

	t.Run("cross-client access is denied", func(t *testing.T) {
		status, _ := call(t, srv, http.MethodPost, "/api/sites/ftp-users", env.bCookie, env.bCSRF,
			map[string]any{
				"parent_domain_id": domainID,
				"username":         "steal",
				"password":         "Sup3r-Secret!",
			})
		require.Equal(t, http.StatusForbidden, status, "parent of another client")

		status, _ = call(t, srv, http.MethodGet,
			fmt.Sprintf("/api/sites/ftp-users/%d", int(ftpID)), env.bCookie, "", nil)
		require.Equal(t, http.StatusForbidden, status)

		status, _ = call(t, srv, http.MethodPut,
			fmt.Sprintf("/api/sites/ftp-users/%d", int(ftpID)), env.bCookie, env.bCSRF,
			map[string]any{"quota_size": 100})
		// Cross-client update is denied (403) or hidden (404) depending on
		// whether the riud Get path surfaces not-found vs permission.
		require.True(t, status == http.StatusForbidden || status == http.StatusNotFound,
			"status=%d", status)
	})

	t.Run("update quota and dir under docroot", func(t *testing.T) {
		var domain model.WebDomain
		require.NoError(t, db.First(&domain, int(domainID)).Error)
		sub := domain.DocumentRoot + "/uploads"
		status, data := call(t, srv, http.MethodPut,
			fmt.Sprintf("/api/sites/ftp-users/%d", int(ftpID)), env.aCookie, env.aCSRF,
			map[string]any{"quota_size": 512, "dir": sub})
		require.Equal(t, http.StatusOK, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		require.EqualValues(t, 512, rec["quota_size"])
		require.Equal(t, sub, rec["dir"])

		var dl model.SysDatalog
		require.NoError(t, db.Where("dbtable = ? AND action = 'u'", "ftp_user").
			Order("datalog_id DESC").First(&dl).Error)
		require.Contains(t, dl.Data, `"quota_size":512`)
	})

	t.Run("client limit vetoes create", func(t *testing.T) {
		client := model.Client{Username: "clienta", ContactName: "Client A",
			LimitFTPUser: 1, LimitShellUser: -1}
		require.NoError(t, db.Create(&client).Error)
		require.NoError(t, db.Model(&model.SysUser{}).Where("username = ?", "clienta").
			Update("client_id", client.ClientID).Error)
		require.NoError(t, db.Model(&model.SysGroup{}).Where("name = ?", "clienta").
			Update("client_id", client.ClientID).Error)

		// One FTP user already exists → limit 1 blocks.
		status, data := call(t, srv, http.MethodPost, "/api/sites/ftp-users", env.aCookie, env.aCSRF,
			map[string]any{
				"parent_domain_id": domainID,
				"username":         "overlimit",
				"password":         "Sup3r-Secret!",
			})
		require.Equal(t, http.StatusForbidden, status, "%s", data)
		require.Equal(t, "error.limit_ftp_user", errKeyOf(t, data).Key)

		require.NoError(t, db.Model(&model.Client{}).Where("client_id = ?", client.ClientID).
			Update("limit_ftp_user", -1).Error)
	})

	t.Run("list is permission-scoped and searchable", func(t *testing.T) {
		status, data := call(t, srv, http.MethodGet, "/api/sites/ftp-users", env.aCookie, "", nil)
		require.Equal(t, http.StatusOK, status, "%s", data)
		var res api.ListResponse
		require.NoError(t, json.Unmarshal(data, &res))
		require.GreaterOrEqual(t, res.Total, int64(1))
		require.NotContains(t, res.Items[0], "password")

		status, data = call(t, srv, http.MethodGet, "/api/sites/ftp-users?username=alice", env.aCookie, "", nil)
		require.Equal(t, http.StatusOK, status)
		require.NoError(t, json.Unmarshal(data, &res))
		require.EqualValues(t, 1, res.Total)

		status, data = call(t, srv, http.MethodGet, "/api/sites/ftp-users", env.bCookie, "", nil)
		require.Equal(t, http.StatusOK, status)
		require.NoError(t, json.Unmarshal(data, &res))
		require.Zero(t, res.Total, "client B must not see client A's FTP users")
	})

	t.Run("delete journals and removes the row", func(t *testing.T) {
		status, _ := call(t, srv, http.MethodDelete,
			fmt.Sprintf("/api/sites/ftp-users/%d", int(ftpID)), env.aCookie, env.aCSRF, nil)
		require.Equal(t, http.StatusNoContent, status)

		var dl model.SysDatalog
		require.NoError(t, db.Where("dbtable = ? AND action = 'd'", "ftp_user").
			Order("datalog_id DESC").First(&dl).Error)
		require.Equal(t, fmt.Sprintf("ftp_user_id:%d", int(ftpID)), dl.DBIdx)

		status, _ = call(t, srv, http.MethodGet,
			fmt.Sprintf("/api/sites/ftp-users/%d", int(ftpID)), env.aCookie, "", nil)
		require.Equal(t, http.StatusForbidden, status, "missing row reads as 403 under riud")
	})
}

// TestSitesShellUserAPI covers /api/sites/shell-users (tasks 5.2 + 5.3).
func TestSitesShellUserAPI(t *testing.T) {
	env := newSitesTestEnv(t, "sitesshell")
	db, srv := env.db, env.srv

	api.RegisterLimitHook(clients.LimitHook(db))
	t.Cleanup(func() {
		api.RegisterLimitHook(func(context.Context, string, *repository.Identity, map[string]any) error {
			return nil
		})
	})

	// Raise website + shell limits for clienta (schema defaults block create).
	client := model.Client{Username: "clienta", ContactName: "Client A",
		LimitWebDomain: -1, LimitShellUser: -1, LimitFTPUser: -1, SSHChroot: "no,jailkit"}
	require.NoError(t, db.Create(&client).Error)
	require.NoError(t, db.Model(&model.SysUser{}).Where("username = ?", "clienta").
		Update("client_id", client.ClientID).Error)
	require.NoError(t, db.Model(&model.SysGroup{}).Where("name = ?", "clienta").
		Update("client_id", client.ClientID).Error)

	domainID := env.createDomain(t, env.aCookie, env.aCSRF,
		map[string]any{"server_id": 1, "domain": "clienta-shell.com", "type": "vhost"})

	var shellID float64

	t.Run("create derives parent fields and journals datalog", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/sites/shell-users", env.aCookie, env.aCSRF,
			map[string]any{
				"parent_domain_id": domainID,
				"username":         "alice",
				"password":         "Sup3r-Secret!",
			})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		shellID = rec["shell_user_id"].(float64)
		require.Equal(t, "clientaalice", rec["username"], "shelluser_prefix applies")
		require.Equal(t, "y", rec["active"])
		require.Equal(t, "/bin/bash", rec["shell"])
		require.NotContains(t, rec, "password")

		var domain model.WebDomain
		require.NoError(t, db.First(&domain, int(domainID)).Error)
		require.EqualValues(t, domain.SysGroupID, rec["sys_groupid"])
		require.Equal(t, domain.SystemUser, rec["puser"])
		require.Equal(t, domain.SystemGroup, rec["pgroup"])
		require.Equal(t, domain.DocumentRoot, rec["dir"])

		var stored model.ShellUser
		require.NoError(t, db.First(&stored, int(shellID)).Error)
		require.True(t, strings.HasPrefix(stored.Password, "$6$") || strings.HasPrefix(stored.Password, "$5$") ||
			strings.HasPrefix(stored.Password, "$1$"),
			"password must be CRYPT, got %q", stored.Password)

		var dl model.SysDatalog
		require.NoError(t, db.Where("dbtable = ? AND action = 'i'", "shell_user").
			Order("datalog_id DESC").First(&dl).Error)
		require.Equal(t, fmt.Sprintf("shell_user_id:%d", int(shellID)), dl.DBIdx)
	})

	t.Run("blacklist and length validation", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/sites/shell-users", env.aCookie, env.aCSRF,
			map[string]any{
				"parent_domain_id": domainID,
				"username":         "root",
				"password":         "Sup3r-Secret!",
			})
		require.Equal(t, http.StatusUnprocessableEntity, status, "%s", data)
		require.Contains(t, errKeyOf(t, data).Fields["username"], "username_error_blacklist")

		status, data = call(t, srv, http.MethodPost, "/api/sites/shell-users", env.aCookie, env.aCSRF,
			map[string]any{
				"parent_domain_id": domainID,
				"username":         strings.Repeat("a", 33),
				"password":         "Sup3r-Secret!",
			})
		require.Equal(t, http.StatusUnprocessableEntity, status)
		// Either regex (field validator) or prepare length key.
		fields := errKeyOf(t, data).Fields["username"]
		require.True(t, containsAny(fields, "username_error_regex", "username_error_len"),
			"got %v", fields)
	})

	t.Run("parent domain is immutable on update", func(t *testing.T) {
		bDomainID := env.createDomain(t, env.bCookie, env.bCSRF,
			map[string]any{"server_id": 1, "domain": "clientb-shell.com", "type": "vhost"})
		// Client A cannot reparent to B's site; also immutability key when
		// they try to change to a different owned domain.
		status, data := call(t, srv, http.MethodPut,
			fmt.Sprintf("/api/sites/shell-users/%d", int(shellID)), env.aCookie, env.aCSRF,
			map[string]any{"parent_domain_id": bDomainID})
		// Cross-client parent → 403 from loadOwned, or 422 immutable.
		require.True(t, status == http.StatusForbidden || status == http.StatusUnprocessableEntity,
			"status=%d body=%s", status, data)

		// Same client, different domain still locked.
		aDomain2 := env.createDomain(t, env.aCookie, env.aCSRF,
			map[string]any{"server_id": 1, "domain": "clienta-shell2.com", "type": "vhost"})
		status, data = call(t, srv, http.MethodPut,
			fmt.Sprintf("/api/sites/shell-users/%d", int(shellID)), env.aCookie, env.aCSRF,
			map[string]any{"parent_domain_id": aDomain2})
		require.Equal(t, http.StatusUnprocessableEntity, status, "%s", data)
		require.Contains(t, errKeyOf(t, data).Fields["parent_domain_id"], "parent_domain_id_error_immutable")
	})

	t.Run("jailkit chroot allowed when client ssh_chroot permits", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/sites/shell-users", env.aCookie, env.aCSRF,
			map[string]any{
				"parent_domain_id": domainID,
				"username":         "jailed",
				"password":         "Sup3r-Secret!",
				"chroot":           "jailkit",
			})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		require.Equal(t, "jailkit", rec["chroot"])

		// Forbid jailkit and retry.
		require.NoError(t, db.Model(&model.Client{}).Where("client_id = ?", client.ClientID).
			Update("ssh_chroot", "no").Error)
		status, data = call(t, srv, http.MethodPost, "/api/sites/shell-users", env.aCookie, env.aCSRF,
			map[string]any{
				"parent_domain_id": domainID,
				"username":         "jailed2",
				"password":         "Sup3r-Secret!",
				"chroot":           "jailkit",
			})
		require.Equal(t, http.StatusUnprocessableEntity, status, "%s", data)
		require.Contains(t, errKeyOf(t, data).Fields["chroot"], "chroot_error_notallowed")
		require.NoError(t, db.Model(&model.Client{}).Where("client_id = ?", client.ClientID).
			Update("ssh_chroot", "no,jailkit").Error)
	})

	t.Run("cross-client access is denied", func(t *testing.T) {
		status, _ := call(t, srv, http.MethodGet,
			fmt.Sprintf("/api/sites/shell-users/%d", int(shellID)), env.bCookie, "", nil)
		require.Equal(t, http.StatusForbidden, status)

		status, _ = call(t, srv, http.MethodPut,
			fmt.Sprintf("/api/sites/shell-users/%d", int(shellID)), env.bCookie, env.bCSRF,
			map[string]any{"active": "n"})
		require.Equal(t, http.StatusForbidden, status)
	})

	t.Run("toggle active journals update", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPut,
			fmt.Sprintf("/api/sites/shell-users/%d", int(shellID)), env.aCookie, env.aCSRF,
			map[string]any{"active": "n"})
		require.Equal(t, http.StatusOK, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		require.Equal(t, "n", rec["active"])

		var dl model.SysDatalog
		require.NoError(t, db.Where("dbtable = ? AND action = 'u'", "shell_user").
			Order("datalog_id DESC").First(&dl).Error)
		require.Contains(t, dl.Data, `"active":"n"`)
	})

	t.Run("client limit vetoes create", func(t *testing.T) {
		require.NoError(t, db.Model(&model.Client{}).Where("client_id = ?", client.ClientID).
			Update("limit_shell_user", 0).Error)
		status, data := call(t, srv, http.MethodPost, "/api/sites/shell-users", env.aCookie, env.aCSRF,
			map[string]any{
				"parent_domain_id": domainID,
				"username":         "blocked",
				"password":         "Sup3r-Secret!",
			})
		require.Equal(t, http.StatusForbidden, status, "%s", data)
		require.Equal(t, "error.limit_shell_user", errKeyOf(t, data).Key)
		require.NoError(t, db.Model(&model.Client{}).Where("client_id = ?", client.ClientID).
			Update("limit_shell_user", -1).Error)
	})

	t.Run("delete journals", func(t *testing.T) {
		status, _ := call(t, srv, http.MethodDelete,
			fmt.Sprintf("/api/sites/shell-users/%d", int(shellID)), env.aCookie, env.aCSRF, nil)
		require.Equal(t, http.StatusNoContent, status)

		var dl model.SysDatalog
		require.NoError(t, db.Where("dbtable = ? AND action = 'd'", "shell_user").
			Order("datalog_id DESC").First(&dl).Error)
		require.Equal(t, fmt.Sprintf("shell_user_id:%d", int(shellID)), dl.DBIdx)
	})
}

func containsAny(ss []string, keys ...string) bool {
	set := map[string]struct{}{}
	for _, s := range ss {
		set[s] = struct{}{}
	}
	for _, k := range keys {
		if _, ok := set[k]; ok {
			return true
		}
	}
	return false
}
