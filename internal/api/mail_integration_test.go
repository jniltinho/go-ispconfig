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
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"

	"go-ispconfig/internal/api"
	"go-ispconfig/internal/auth"
	"go-ispconfig/internal/config"
	"go-ispconfig/internal/database"
	"go-ispconfig/internal/model"
)

func newMailTestEnv(t *testing.T) (*gorm.DB, *httptest.Server, string, string) {
	t.Helper()
	dsnPrefix, container := database.StartMariaDB(t, "mailapi")
	database.MariaDBExec(t, container, "CREATE DATABASE ispconfig CHARACTER SET utf8mb4")
	db, err := database.Open(dsnPrefix + "/ispconfig?charset=utf8mb4&parseTime=True&loc=Local")
	require.NoError(t, err)
	needSeed, err := database.Migrate(db)
	require.NoError(t, err)
	require.True(t, needSeed)
	_, err = database.Seed(db, "mail1.example.com", "smoke-test-pw")
	require.NoError(t, err)
	// The seeded server must be a mail server for the domain form.
	require.NoError(t, db.Exec("UPDATE server SET mail_server = 1 WHERE server_id = 1").Error)

	e := echo.New()
	e.Use(echoMiddleware.Recover())
	deps := &api.Deps{DB: db, Sessions: auth.NewStore(db, 0), Config: &config.Config{}, DNSPub: api.NewDNSPublisher(db)}
	require.NoError(t, api.Register(e, deps))
	srv := httptest.NewServer(e)
	t.Cleanup(srv.Close)
	cookie, csrf := login(t, srv, "admin", "smoke-test-pw")
	return db, srv, cookie, csrf
}

func TestMailDomainAPI(t *testing.T) {
	db, srv, cookie, csrf := newMailTestEnv(t)

	var domainID float64
	t.Run("create generates a DKIM key and journals", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/mail/domains", cookie, csrf,
			map[string]any{"server_id": 1, "domain": "Example.COM", "active": "y", "dkim": "y"})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		domainID = rec["domain_id"].(float64)
		assert.Equal(t, "example.com", rec["domain"], "lowercased + IDN")
		assert.NotContains(t, rec, "dkim_private", "private key redacted in responses")
		assert.NotEmpty(t, rec["dkim_public"], "public key generated")
		assert.Equal(t, "default", rec["dkim_selector"])

		var row model.MailDomain
		require.NoError(t, db.Take(&row, domainID).Error)
		assert.Contains(t, row.DKIMPrivate, "PRIVATE KEY", "private key stored")

		var dl model.SysDatalog
		require.NoError(t, db.Where("dbtable = 'mail_domain' AND action = 'i'").
			Order("datalog_id DESC").First(&dl).Error)
		require.Equal(t, fmt.Sprintf("domain_id:%d", int(domainID)), dl.DBIdx)
		// The datalog row carries the full row incl. the private key so the
		// daemon can write the key file — that is expected (not a leak to
		// API clients, which never see the datalog).
	})

	t.Run("validation: bad domain and transport collision", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/mail/domains", cookie, csrf,
			map[string]any{"server_id": 1, "domain": "notadomain"})
		require.Equal(t, http.StatusUnprocessableEntity, status, "%s", data)
		require.Contains(t, string(data), "domain_error_regex")

		require.NoError(t, db.Create(&model.MailTransport{
			SysUserID: 1, SysGroupID: 1, SysPermUser: "riud", SysPermGroup: "riud",
			ServerID: 1, Domain: "relay.example.com", Transport: "smtp:x", SortOrder: 5, Active: "y",
		}).Error)
		status, data = call(t, srv, http.MethodPost, "/api/mail/domains", cookie, csrf,
			map[string]any{"server_id": 1, "domain": "relay.example.com"})
		require.Equal(t, http.StatusUnprocessableEntity, status, "%s", data)
		require.Contains(t, string(data), "domain_is_transport")
	})

	t.Run("list omits the private key", func(t *testing.T) {
		status, data := call(t, srv, http.MethodGet, "/api/mail/domains?limit=100", cookie, "", nil)
		require.Equal(t, http.StatusOK, status)
		assert.NotContains(t, string(data), "PRIVATE KEY")
		assert.Contains(t, string(data), "example.com")
	})

	t.Run("get-by-domain", func(t *testing.T) {
		status, data := call(t, srv, http.MethodGet, "/api/mail/domains/by-domain/example.com", cookie, "", nil)
		require.Equal(t, http.StatusOK, status)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		assert.EqualValues(t, domainID, rec["domain_id"])
		assert.NotContains(t, rec, "dkim_private")

		status, _ = call(t, srv, http.MethodGet, "/api/mail/domains/by-domain/ghost.example", cookie, "", nil)
		require.Equal(t, http.StatusNotFound, status)
	})

	t.Run("set-status flips active and journals", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost,
			fmt.Sprintf("/api/mail/domains/%d/set-status", int(domainID)), cookie, csrf,
			map[string]any{"active": "n"})
		require.Equal(t, http.StatusNoContent, status, "%s", data)
		var row model.MailDomain
		require.NoError(t, db.Take(&row, domainID).Error)
		assert.Equal(t, "n", row.Active)
	})

	t.Run("generate-dkim returns a fresh pair", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/mail/domains/generate-dkim", cookie, csrf, nil)
		require.Equal(t, http.StatusOK, status, "%s", data)
		var rec map[string]string
		require.NoError(t, json.Unmarshal(data, &rec))
		assert.Contains(t, rec["dkim_private"], "PRIVATE KEY")
		assert.Contains(t, rec["dkim_public"], "PUBLIC KEY")
	})

	t.Run("DKIM TXT published when a managed zone exists", func(t *testing.T) {
		soa := model.DNSSoa{
			SysUserID: 1, SysGroupID: 1, SysPermUser: "riud", SysPermGroup: "riud",
			ServerID: 1, Origin: "dkimzone.test.", NS: "ns1.dkimzone.test.",
			Mbox: "hostmaster.dkimzone.test.", Serial: 2026080101,
			Refresh: 7200, Retry: 540, Expire: 604800, Minimum: 3600, TTL: 3600,
			Active: "Y", DNSSECWanted: "N", DNSSECInitialized: "N",
		}
		require.NoError(t, db.Create(&soa).Error)
		status, data := call(t, srv, http.MethodPost, "/api/mail/domains", cookie, csrf,
			map[string]any{"server_id": 1, "domain": "dkimzone.test", "active": "y", "dkim": "y"})
		require.Equal(t, http.StatusCreated, status, "%s", data)

		var n int64
		require.NoError(t, db.Model(&model.DNSRr{}).
			Where("name = ? AND type = 'TXT'", "default._domainkey.dkimzone.test.").Count(&n).Error)
		assert.EqualValues(t, 1, n, "DKIM TXT published into the managed zone")
	})
}
