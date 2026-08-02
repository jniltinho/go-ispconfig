//go:build integration

package api_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/clients"
	"go-ispconfig/internal/model"
	"go-ispconfig/internal/repository"
)

// TestClientLimitEnforcement covers the client limit hook end to end
// through the API (task 2.4): count vetoes with 403 error.limit_* and no
// datalog row, zero-limit veto, unlimited pass, admin bypass, and the
// limit_client child count. (Lives in the api test package: the clients
// package cannot import api from its own tests since api depends on it.)
func TestClientLimitEnforcement(t *testing.T) {
	db, srv, adminCookie, adminCSRF := newClientsTestEnv(t)
	ctx := context.Background()

	// Client with tight limits: 1 zone, 0 websites, unlimited slaves.
	status, data := call(t, srv, http.MethodPost, "/api/clients", adminCookie, adminCSRF,
		map[string]any{
			"contact_name": "Limited", "username": "limited", "password": "limit-pw-X9!x",
			"limit_dns_zone": 1, "limit_web_domain": 0, "limit_dns_slave_zone": -1,
		})
	require.Equal(t, http.StatusCreated, status, "%s", data)
	cookie, csrf := login(t, srv, "limited", "limit-pw-X9!x")

	zoneBody := map[string]any{
		"server_id": 1, "origin": "one.example.com.", "ns": "ns1.example.com.",
		"mbox": "hostmaster.example.com.", "active": "Y",
	}

	t.Run("first zone within limit is created", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/dns/zones", cookie, csrf, zoneBody)
		require.Equal(t, http.StatusCreated, status, "%s", data)
	})

	t.Run("second zone at limit is vetoed 403 without journaling", func(t *testing.T) {
		body := map[string]any{}
		for k, v := range zoneBody {
			body[k] = v
		}
		body["origin"] = "two.example.com."
		status, data := call(t, srv, http.MethodPost, "/api/dns/zones", cookie, csrf, body)
		require.Equal(t, http.StatusForbidden, status, "%s", data)
		require.Contains(t, string(data), "error.limit_dns_zone")
		var n int64
		require.NoError(t, db.Model(&model.SysDatalog{}).
			Where("dbtable = ? AND data LIKE ?", "dns_soa", "%two.example.com%").Count(&n).Error)
		require.Zero(t, n, "vetoed create must not journal")
	})

	t.Run("zero limit blocks websites entirely", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/sites/web-domains", cookie, csrf,
			map[string]any{"server_id": 1, "domain": "blocked.example.com", "type": "vhost"})
		require.Equal(t, http.StatusForbidden, status, "%s", data)
		require.Contains(t, string(data), "error.limit_web_domain")
	})

	t.Run("unlimited slave zones are not vetoed", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/dns/slave-zones", cookie, csrf,
			map[string]any{"server_id": 1, "origin": "slave.example.net.", "ns": "192.0.2.53", "active": "Y"})
		require.Equal(t, http.StatusCreated, status, "%s", data)
	})

	t.Run("admin bypasses count limits", func(t *testing.T) {
		body := map[string]any{}
		for k, v := range zoneBody {
			body[k] = v
		}
		body["origin"] = "admin.example.com."
		status, data := call(t, srv, http.MethodPost, "/api/dns/zones", adminCookie, adminCSRF, body)
		require.Equal(t, http.StatusCreated, status, "%s", data)
	})

	t.Run("child client counting under a reseller", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/resellers", adminCookie, adminCSRF,
			map[string]any{
				"contact_name": "Cap Re", "username": "capre",
				"password": "capre-pw-long1", "limit_client": 2,
			})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		rCookie, rCSRF := login(t, srv, "capre", "capre-pw-long1")
		for _, kid := range []string{"kid1", "kid2"} {
			status, data := call(t, srv, http.MethodPost, "/api/clients", rCookie, rCSRF,
				map[string]any{"contact_name": kid, "username": kid, "password": kid + "-pw-long1"})
			require.Equal(t, http.StatusCreated, status, "%s", data)
		}
		status, data = call(t, srv, http.MethodPost, "/api/clients", rCookie, rCSRF,
			map[string]any{"contact_name": "kid3", "username": "kid3", "password": "kid3-pw-long1"})
		require.Equal(t, http.StatusForbidden, status, "third child at limit_client=2 must veto: %s", data)
		require.Contains(t, string(data), "error.limit_client")

		// Unlimited reseller may create again.
		var reseller model.Client
		require.NoError(t, db.Where("username = ?", "capre").Take(&reseller).Error)
		require.NoError(t, db.Model(&model.Client{}).Where("client_id = ?", reseller.ClientID).
			Update("limit_client", -1).Error)
		status, data = call(t, srv, http.MethodPost, "/api/clients", rCookie, rCSRF,
			map[string]any{"contact_name": "kid3", "username": "kid3", "password": "kid3-pw-long1"})
		require.Equal(t, http.StatusCreated, status, "%s", data)
	})

	t.Run("unknown entity is never vetoed", func(t *testing.T) {
		var u model.SysUser
		require.NoError(t, db.Where("username = ?", "limited").Take(&u).Error)
		uid := &repository.Identity{UserID: u.UserID, Username: u.Username, Typ: "user"}
		require.NoError(t, clients.LimitHook(db)(ctx, "mail-domains", uid, nil))
	})
}
