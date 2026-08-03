//go:build integration

package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/model"
)

// TestSitesCronAPI covers /api/sites/crons (task 4.1): CRUD, parent
// inheritance of server_id/sys_groupid, parent immutability, datalog, and
// client isolation.
func TestSitesCronAPI(t *testing.T) {
	env := newSitesTestEnv(t, "sitescron")
	db, srv := env.db, env.srv

	// Admin vhost via API.
	adminDomainID := env.createDomain(t, env.adminCookie, env.adminCSRF, map[string]any{
		"server_id": 1, "domain": "admin-cron.example.com", "type": "vhost",
		"ip_address": "*", "active": "y", "hd_quota": -1, "traffic_quota": -1,
	})
	// Client A vhost — seed via DB with correct group ownership if API limits bite.
	clientDomain := seedClientVhost(t, env, "clienta-cron.example.com", "clienta")

	var cronID float64
	t.Run("admin create inherits parent server_id and journals", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/sites/crons", env.adminCookie, env.adminCSRF,
			map[string]any{
				"parent_domain_id": adminDomainID,
				"command":          "https://admin-cron.example.com/job",
				"run_min":          "*/5", "run_hour": "*", "run_mday": "*", "run_month": "*", "run_wday": "*",
				"type": "url", "log": "n", "active": "y",
				"server_id": 99, // must be overwritten
			})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		cronID = rec["id"].(float64)
		assert.EqualValues(t, 1, rec["server_id"], "server_id forced from parent")
		assert.EqualValues(t, adminDomainID, rec["parent_domain_id"])

		var dl model.SysDatalog
		require.NoError(t, db.Where("dbtable = 'cron' AND action = 'i'").
			Order("datalog_id DESC").First(&dl).Error)
		assert.Equal(t, fmt.Sprintf("id:%d", int(cronID)), dl.DBIdx)
		assert.EqualValues(t, 1, dl.ServerID)
	})

	t.Run("list and get", func(t *testing.T) {
		status, data := call(t, srv, http.MethodGet, "/api/sites/crons", env.adminCookie, "", nil)
		require.Equal(t, http.StatusOK, status, "%s", data)
		status, data = call(t, srv, http.MethodGet, fmt.Sprintf("/api/sites/crons/%d", int(cronID)), env.adminCookie, "", nil)
		require.Equal(t, http.StatusOK, status, "%s", data)
	})

	t.Run("parent_domain_id immutable on update", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPut, fmt.Sprintf("/api/sites/crons/%d", int(cronID)),
			env.adminCookie, env.adminCSRF,
			map[string]any{
				"parent_domain_id": clientDomain.DomainID,
				"command":          "https://admin-cron.example.com/job2",
				"run_min":          "0", "run_hour": "*", "run_mday": "*", "run_month": "*", "run_wday": "*",
				"type": "url", "log": "y", "active": "y",
			})
		if status == http.StatusOK {
			var rec map[string]any
			require.NoError(t, json.Unmarshal(data, &rec))
			assert.EqualValues(t, adminDomainID, rec["parent_domain_id"])
		} else {
			require.Equal(t, http.StatusUnprocessableEntity, status, "%s", data)
		}
		var stored model.Cron
		require.NoError(t, db.Take(&stored, int(cronID)).Error)
		assert.EqualValues(t, adminDomainID, stored.ParentDomainID)
	})

	t.Run("client create on own site inherits sys_groupid", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/sites/crons", env.aCookie, env.aCSRF,
			map[string]any{
				"parent_domain_id": clientDomain.DomainID,
				"command":          "https://clienta-cron.example.com/job",
				"run_min":          "15", "run_hour": "*", "run_mday": "*", "run_month": "*", "run_wday": "*",
				"active": "y", "log": "n",
			})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		assert.EqualValues(t, clientDomain.SysGroupID, rec["sys_groupid"])
	})

	t.Run("client B cannot read client A cron", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/sites/crons", env.aCookie, env.aCSRF,
			map[string]any{
				"parent_domain_id": clientDomain.DomainID,
				"command":          "https://clienta-cron.example.com/secret",
				"run_min":          "30", "run_hour": "*", "run_mday": "*", "run_month": "*", "run_wday": "*",
				"active": "y",
			})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		aID := int(rec["id"].(float64))

		status, _ = call(t, srv, http.MethodGet, fmt.Sprintf("/api/sites/crons/%d", aID), env.bCookie, "", nil)
		assert.True(t, status == http.StatusNotFound || status == http.StatusForbidden, "got %d", status)

		status, data = call(t, srv, http.MethodGet, "/api/sites/crons", env.bCookie, "", nil)
		require.Equal(t, http.StatusOK, status, "%s", data)
		assert.NotContains(t, string(data), "clienta-cron.example.com/secret")
	})

	t.Run("client cannot use admin parent", func(t *testing.T) {
		status, _ := call(t, srv, http.MethodPost, "/api/sites/crons", env.aCookie, env.aCSRF,
			map[string]any{
				"parent_domain_id": adminDomainID,
				"command":          "https://admin-cron.example.com/x",
				"run_min":          "*", "run_hour": "*", "run_mday": "*", "run_month": "*", "run_wday": "*",
				"active": "y",
			})
		assert.True(t, status == http.StatusNotFound || status == http.StatusForbidden || status == http.StatusUnprocessableEntity,
			"got %d", status)
	})

	t.Run("delete journals", func(t *testing.T) {
		status, data := call(t, srv, http.MethodDelete, fmt.Sprintf("/api/sites/crons/%d", int(cronID)),
			env.adminCookie, env.adminCSRF, nil)
		require.Equal(t, http.StatusNoContent, status, "%s", data)
		var dl model.SysDatalog
		require.NoError(t, db.Where("dbtable = 'cron' AND action = 'd'").
			Order("datalog_id DESC").First(&dl).Error)
		assert.Equal(t, fmt.Sprintf("id:%d", int(cronID)), dl.DBIdx)
	})
}

// seedClientVhost creates a vhost owned by the named client user's group.
func seedClientVhost(t *testing.T, env *sitesTestEnv, domain, username string) model.WebDomain {
	t.Helper()
	var user model.SysUser
	require.NoError(t, env.db.Where("username = ?", username).Take(&user).Error)
	wd := model.WebDomain{
		SysUserID: user.UserID, SysGroupID: user.DefaultGroup,
		SysPermUser: "riud", SysPermGroup: "ru", SysPermOther: "",
		ServerID: 1, IPAddress: "*", Domain: domain, Type: "vhost",
		DocumentRoot: "/var/www/clients/client1/web99",
		SystemUser:   "web99", SystemGroup: "client1",
		CGI: "n", SSI: "n", Suexec: "y", Ruby: "n", Python: "n", Perl: "n",
		SSLLetsencryptExclude: "n", PHPFPMChroot: "n", BackupEncrypt: "n",
		TrafficQuotaLock: "n", EnablePagespeed: "n", ProxyProtocol: "n",
		DeleteUnusedJailkit: "n", DisableSymlinknotowner: "n",
		PHP: "php-fpm", PHPFPMUseSocket: "y", PM: "dynamic",
		PMMaxChildren: 10, PMStartServers: 2, PMMinSpareServers: 1, PMMaxSpareServers: 5,
		PMProcessIdleTimeout: 10,
		SSL:                  "n", SSLLetsencrypt: "n", RewriteToHTTPS: "n",
		SeoRedirect: "non_www_to_www", Subdomain: "www", Active: "y",
		HTTPPort: 80, HTTPSPort: 443,
	}
	require.NoError(t, env.db.Create(&wd).Error)
	return wd
}
