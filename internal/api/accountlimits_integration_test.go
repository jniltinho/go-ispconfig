//go:build integration

package api_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/api"
)

// TestAccountLimitsEndpoint covers GET /api/monitor/limits (dashlet_limits
// port): admins are unlimited, a client sees its own limits with live usage,
// and zero-limit rows are omitted the way limits.php omits them.
func TestAccountLimitsEndpoint(t *testing.T) {
	_, srv, adminCookie, adminCSRF := newClientsTestEnv(t)

	get := func(cookie, csrf string) api.AccountLimits {
		status, data := call(t, srv, http.MethodGet, "/api/monitor/limits", cookie, csrf, nil)
		require.Equal(t, http.StatusOK, status, "%s", data)
		var out api.AccountLimits
		require.NoError(t, json.Unmarshal(data, &out))
		return out
	}

	t.Run("admin is unlimited", func(t *testing.T) {
		out := get(adminCookie, adminCSRF)
		require.True(t, out.Unlimited)
		require.Empty(t, out.Limits)
	})

	status, data := call(t, srv, http.MethodPost, "/api/clients", adminCookie, adminCSRF,
		map[string]any{
			"contact_name": "Dashlet", "username": "dashlet", "password": "dash-pw-X9!x",
			"limit_dns_zone": 2, "limit_web_domain": 0, "limit_mailbox": -1,
			"limit_web_quota": 500,
		})
	require.Equal(t, http.StatusCreated, status, "%s", data)
	cookie, csrf := login(t, srv, "dashlet", "dash-pw-X9!x")

	t.Run("client sees its own limits with usage", func(t *testing.T) {
		out := get(cookie, csrf)
		require.False(t, out.Unlimited)

		rows := map[string]int64{}
		limits := map[string]int32{}
		quota := map[string]bool{}
		for _, r := range out.Limits {
			rows[r.Field], limits[r.Field], quota[r.Field] = r.Usage, r.Limit, r.Quota
		}

		require.Contains(t, limits, "limit_dns_zone")
		require.Equal(t, int32(2), limits["limit_dns_zone"])
		require.Zero(t, rows["limit_dns_zone"], "no zone created yet")

		require.Equal(t, int32(-1), limits["limit_mailbox"], "unlimited rows stay visible")
		require.True(t, quota["limit_web_quota"], "web quota is a MB row")
		require.NotContains(t, limits, "limit_web_domain", "zero limits are hidden")
	})

	t.Run("usage tracks created records", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/dns/zones", cookie, csrf, map[string]any{
			"server_id": 1, "origin": "dashlet.example.com.", "ns": "ns1.example.com.",
			"mbox": "hostmaster.example.com.", "active": "Y",
		})
		require.Equal(t, http.StatusCreated, status, "%s", data)

		for _, r := range get(cookie, csrf).Limits {
			if r.Field == "limit_dns_zone" {
				require.Equal(t, int64(1), r.Usage)
				return
			}
		}
		t.Fatal("limit_dns_zone row missing after creating a zone")
	})
}
