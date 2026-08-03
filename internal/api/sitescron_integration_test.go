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

// TestSitesCronValidationFailures covers task 4.2: invalid schedule/command
// rejected with 422 and no datalog row; URL forces type=url; @reboot only in
// run_month; admin non-URL derives type=full.
func TestSitesCronValidationFailures(t *testing.T) {
	env := newSitesTestEnv(t, "sitescronval")
	db, srv := env.db, env.srv

	domainID := env.createDomain(t, env.adminCookie, env.adminCSRF, map[string]any{
		"server_id": 1, "domain": "cron-val.example.com", "type": "vhost",
		"ip_address": "*", "active": "y", "hd_quota": -1, "traffic_quota": -1,
	})

	datalogCount := func(t *testing.T) int64 {
		t.Helper()
		var n int64
		require.NoError(t, db.Model(&model.SysDatalog{}).Where("dbtable = 'cron'").Count(&n).Error)
		return n
	}

	t.Run("invalid run_min rejected without datalog", func(t *testing.T) {
		before := datalogCount(t)
		status, data := call(t, srv, http.MethodPost, "/api/sites/crons", env.adminCookie, env.adminCSRF,
			map[string]any{
				"parent_domain_id": domainID,
				"command":          "https://cron-val.example.com/job",
				"run_min":          "60", "run_hour": "*", "run_mday": "*", "run_month": "*", "run_wday": "*",
				"active": "y",
			})
		require.Equal(t, http.StatusUnprocessableEntity, status, "%s", data)
		assert.Contains(t, string(data), "run_min_error_format")
		assert.Equal(t, before, datalogCount(t), "no datalog on validation failure")
	})

	t.Run("@reboot rejected in run_min", func(t *testing.T) {
		before := datalogCount(t)
		status, data := call(t, srv, http.MethodPost, "/api/sites/crons", env.adminCookie, env.adminCSRF,
			map[string]any{
				"parent_domain_id": domainID,
				"command":          "https://cron-val.example.com/job",
				"run_min":          "@reboot", "run_hour": "*", "run_mday": "*", "run_month": "*", "run_wday": "*",
				"active": "y",
			})
		require.Equal(t, http.StatusUnprocessableEntity, status, "%s", data)
		assert.Equal(t, before, datalogCount(t))
	})

	t.Run("@reboot accepted in run_month", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/sites/crons", env.adminCookie, env.adminCSRF,
			map[string]any{
				"parent_domain_id": domainID,
				"command":          "https://cron-val.example.com/reboot",
				"run_min":          "*", "run_hour": "*", "run_mday": "*", "run_month": "@reboot", "run_wday": "*",
				"active": "y", "log": "n",
			})
		require.Equal(t, http.StatusCreated, status, "%s", data)
	})

	t.Run("bad URL host rejected without datalog", func(t *testing.T) {
		before := datalogCount(t)
		status, data := call(t, srv, http.MethodPost, "/api/sites/crons", env.adminCookie, env.adminCSRF,
			map[string]any{
				"parent_domain_id": domainID,
				"command":          "https://not a host/path",
				"run_min":          "*", "run_hour": "*", "run_mday": "*", "run_month": "*", "run_wday": "*",
				"active": "y",
			})
		require.Equal(t, http.StatusUnprocessableEntity, status, "%s", data)
		assert.Contains(t, string(data), "command_error_format")
		assert.Equal(t, before, datalogCount(t))
	})

	t.Run("HTTP command forces type url", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/sites/crons", env.adminCookie, env.adminCSRF,
			map[string]any{
				"parent_domain_id": domainID,
				"command":          "https://cron-val.example.com/forced",
				"run_min":          "0", "run_hour": "*", "run_mday": "*", "run_month": "*", "run_wday": "*",
				"type":   "full", // client-supplied type ignored
				"active": "y",
			})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		assert.Equal(t, "url", rec["type"])
	})

	t.Run("non-URL on admin site derives type full", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/sites/crons", env.adminCookie, env.adminCSRF,
			map[string]any{
				"parent_domain_id": domainID,
				"command":          "/usr/bin/php /web/cron.php",
				"run_min":          "0", "run_hour": "1", "run_mday": "*", "run_month": "*", "run_wday": "*",
				"type":   "url", // overwritten
				"active": "y",
			})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		assert.Equal(t, "full", rec["type"])
	})

	t.Run("client parent with limit_cron_type chrooted", func(t *testing.T) {
		// Link clienta group to a real client row with limit_cron_type=chrooted.
		var user model.SysUser
		require.NoError(t, db.Where("username = ?", "clienta").Take(&user).Error)
		cli := model.Client{
			SysUserID: user.UserID, SysGroupID: user.DefaultGroup,
			SysPermUser: "riud", SysPermGroup: "ru",
			ContactName: "Client A", Username: "clienta",
			LimitCron: -1, LimitCronType: "chrooted", LimitCronFrequency: 1,
		}
		require.NoError(t, db.Create(&cli).Error)
		require.NoError(t, db.Model(&model.SysGroup{}).Where("groupid = ?", user.DefaultGroup).
			Update("client_id", cli.ClientID).Error)

		parent := seedClientVhost(t, env, "clienta-chroot-cron.example.com", "clienta")
		status, data := call(t, srv, http.MethodPost, "/api/sites/crons", env.aCookie, env.aCSRF,
			map[string]any{
				"parent_domain_id": parent.DomainID,
				"command":          "/usr/bin/php /web/job.php",
				"run_min":          "0", "run_hour": "2", "run_mday": "*", "run_month": "*", "run_wday": "*",
				"active": "y",
			})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		assert.Equal(t, "chrooted", rec["type"])
	})
}

// TestSitesCronClientLimits covers task 4.3: limit_cron count, type, frequency
// with admin bypass and no datalog on veto.
func TestSitesCronClientLimits(t *testing.T) {
	env := newSitesTestEnv(t, "sitescronlim")
	db, srv := env.db, env.srv

	// Provision clienta with tight limits: 1 cron, url-only, min freq 5.
	var user model.SysUser
	require.NoError(t, db.Where("username = ?", "clienta").Take(&user).Error)
	cli := model.Client{
		SysUserID: user.UserID, SysGroupID: user.DefaultGroup,
		SysPermUser: "riud", SysPermGroup: "ru",
		ContactName: "Limited Cron", Username: "clienta",
		LimitCron: 1, LimitCronType: "url", LimitCronFrequency: 5,
	}
	require.NoError(t, db.Create(&cli).Error)
	require.NoError(t, db.Model(&model.SysGroup{}).Where("groupid = ?", user.DefaultGroup).
		Update("client_id", cli.ClientID).Error)

	parent := seedClientVhost(t, env, "lim-cron.example.com", "clienta")

	datalogCount := func(t *testing.T) int64 {
		t.Helper()
		var n int64
		require.NoError(t, db.Model(&model.SysDatalog{}).Where("dbtable = 'cron'").Count(&n).Error)
		return n
	}

	t.Run("first url cron within count limit", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/sites/crons", env.aCookie, env.aCSRF,
			map[string]any{
				"parent_domain_id": parent.DomainID,
				"command":          "https://lim-cron.example.com/job1",
				"run_min":          "*/5", "run_hour": "*", "run_mday": "*", "run_month": "*", "run_wday": "*",
				"active": "y",
			})
		require.Equal(t, http.StatusCreated, status, "%s", data)
	})

	t.Run("second cron blocked by limit_cron count", func(t *testing.T) {
		before := datalogCount(t)
		status, data := call(t, srv, http.MethodPost, "/api/sites/crons", env.aCookie, env.aCSRF,
			map[string]any{
				"parent_domain_id": parent.DomainID,
				"command":          "https://lim-cron.example.com/job2",
				"run_min":          "*/5", "run_hour": "*", "run_mday": "*", "run_month": "*", "run_wday": "*",
				"active": "y",
			})
		require.Equal(t, http.StatusForbidden, status, "%s", data)
		assert.Contains(t, string(data), "error.limit_cron")
		assert.Equal(t, before, datalogCount(t))
	})

	t.Run("frequency limit rejects every-minute schedule on update", func(t *testing.T) {
		// Raise count so we can create, then test frequency via update.
		require.NoError(t, db.Model(&model.Client{}).Where("client_id = ?", cli.ClientID).
			Update("limit_cron", int32(-1)).Error)
		// Find existing job.
		var job model.Cron
		require.NoError(t, db.Where("sys_groupid = ?", user.DefaultGroup).First(&job).Error)
		before := datalogCount(t)
		status, data := call(t, srv, http.MethodPut, fmt.Sprintf("/api/sites/crons/%d", job.ID),
			env.aCookie, env.aCSRF,
			map[string]any{
				"command": job.Command,
				"run_min": "*", "run_hour": "*", "run_mday": "*", "run_month": "*", "run_wday": "*",
				"active": "y", "log": "n",
			})
		require.Equal(t, http.StatusForbidden, status, "%s", data)
		assert.Contains(t, string(data), "error.limit_cron_frequency")
		assert.Equal(t, before, datalogCount(t))
	})

	t.Run("url-only client cannot create full jobs", func(t *testing.T) {
		before := datalogCount(t)
		status, data := call(t, srv, http.MethodPost, "/api/sites/crons", env.aCookie, env.aCSRF,
			map[string]any{
				"parent_domain_id": parent.DomainID,
				"command":          "/usr/bin/php /web/job.php",
				"run_min":          "0", "run_hour": "*", "run_mday": "*", "run_month": "*", "run_wday": "*",
				"active": "y",
			})
		require.Equal(t, http.StatusForbidden, status, "%s", data)
		assert.Contains(t, string(data), "error.limit_cron_type")
		assert.Equal(t, before, datalogCount(t))
	})

	t.Run("admin bypasses limits for client site", func(t *testing.T) {
		// Re-tighten count and create as admin against the same parent.
		require.NoError(t, db.Model(&model.Client{}).Where("client_id = ?", cli.ClientID).
			Updates(map[string]any{"limit_cron": int32(1), "limit_cron_type": "url"}).Error)
		status, data := call(t, srv, http.MethodPost, "/api/sites/crons", env.adminCookie, env.adminCSRF,
			map[string]any{
				"parent_domain_id": parent.DomainID,
				"command":          "https://lim-cron.example.com/admin-bypass",
				"run_min":          "*", "run_hour": "*", "run_mday": "*", "run_month": "*", "run_wday": "*",
				"active": "y",
			})
		require.Equal(t, http.StatusCreated, status, "%s", data)
	})
}

// TestSitesCronRunsHistory covers GET /api/sites/crons/:id/runs (task 4.4).
func TestSitesCronRunsHistory(t *testing.T) {
	env := newSitesTestEnv(t, "sitescronruns")
	db, srv := env.db, env.srv

	domainID := env.createDomain(t, env.adminCookie, env.adminCSRF, map[string]any{
		"server_id": 1, "domain": "cron-runs.example.com", "type": "vhost",
		"ip_address": "*", "active": "y", "hd_quota": -1, "traffic_quota": -1,
	})
	status, data := call(t, srv, http.MethodPost, "/api/sites/crons", env.adminCookie, env.adminCSRF,
		map[string]any{
			"parent_domain_id": domainID,
			"command":          "https://cron-runs.example.com/job",
			"run_min":          "0", "run_hour": "*", "run_mday": "*", "run_month": "*", "run_wday": "*",
			"active": "y", "log": "y",
		})
	require.Equal(t, http.StatusCreated, status, "%s", data)
	var rec map[string]any
	require.NoError(t, json.Unmarshal(data, &rec))
	cronID := int(rec["id"].(float64))

	// Seed two sys_log run rows via the package formatter.
	msg1 := "cron_run id=" + fmt.Sprint(cronID) + " parent_domain_id=1 type=url status=ok exit=200 start=1700000000 end=1700000001 output=first"
	msg2 := "cron_run id=" + fmt.Sprint(cronID) + " parent_domain_id=1 type=url status=exit exit=1 start=1700000100 end=1700000102 output=second"
	require.NoError(t, db.Create(&model.SysLog{ServerID: 1, DatalogID: 0, Loglevel: 0, Tstamp: 1700000001, Message: msg1}).Error)
	require.NoError(t, db.Create(&model.SysLog{ServerID: 1, DatalogID: 0, Loglevel: 1, Tstamp: 1700000102, Message: msg2}).Error)
	// Unrelated noise.
	require.NoError(t, db.Create(&model.SysLog{ServerID: 1, Message: "cron_run id=999999 type=url status=ok exit=0 start=1 end=2 output=x"}).Error)

	t.Run("owner lists runs newest first", func(t *testing.T) {
		status, data := call(t, srv, http.MethodGet, fmt.Sprintf("/api/sites/crons/%d/runs", cronID),
			env.adminCookie, "", nil)
		require.Equal(t, http.StatusOK, status, "%s", data)
		var page struct {
			Items []map[string]any `json:"items"`
			Total float64          `json:"total"`
			Page  float64          `json:"page"`
			Limit float64          `json:"limit"`
		}
		require.NoError(t, json.Unmarshal(data, &page))
		require.EqualValues(t, 2, page.Total)
		require.Len(t, page.Items, 2)
		assert.Equal(t, "exit", page.Items[0]["status"])
		assert.Equal(t, "second", page.Items[0]["output"])
		assert.Equal(t, "ok", page.Items[1]["status"])
		assert.Equal(t, "first", page.Items[1]["output"])
	})

	t.Run("client B cannot read admin cron runs", func(t *testing.T) {
		status, _ := call(t, srv, http.MethodGet, fmt.Sprintf("/api/sites/crons/%d/runs", cronID),
			env.bCookie, "", nil)
		assert.True(t, status == http.StatusNotFound || status == http.StatusForbidden, "got %d", status)
	})

	t.Run("missing cron is 404 or 403", func(t *testing.T) {
		// Repo may map missing rows under the read scope to 403 or 404.
		status, _ := call(t, srv, http.MethodGet, "/api/sites/crons/999999/runs",
			env.adminCookie, "", nil)
		assert.True(t, status == http.StatusNotFound || status == http.StatusForbidden, "got %d", status)
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
