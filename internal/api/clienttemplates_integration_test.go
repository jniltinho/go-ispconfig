//go:build integration

package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/model"
)

// TestClientTemplatesAPI covers /api/client-templates CRUD, the
// additional-template assignment endpoints with same-transaction
// materialization, and the countries list (task 4.3).
func TestClientTemplatesAPI(t *testing.T) {
	db, srv, adminCookie, adminCSRF := newClientsTestEnv(t)

	var masterID, addonID float64
	var clientID int

	t.Run("template CRUD", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/client-templates", adminCookie, adminCSRF,
			map[string]any{"template_type": "m"})
		require.Equal(t, http.StatusUnprocessableEntity, status, "%s", data)
		require.Contains(t, string(data), "template_name_error_empty")

		status, data = call(t, srv, http.MethodPost, "/api/client-templates", adminCookie, adminCSRF,
			map[string]any{
				"template_name": "Master5", "template_type": "m",
				"limit_web_domain": 5, "limit_dns_zone": 10, "limit_ssl": "y",
			})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		masterID = rec["template_id"].(float64)

		status, data = call(t, srv, http.MethodPost, "/api/client-templates", adminCookie, adminCSRF,
			map[string]any{
				"template_name": "Addon3", "template_type": "a",
				"limit_web_domain": 3, "limit_dns_zone": -1,
			})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		require.NoError(t, json.Unmarshal(data, &rec))
		addonID = rec["template_id"].(float64)

		status, data = call(t, srv, http.MethodGet, "/api/client-templates", adminCookie, "", nil)
		require.Equal(t, http.StatusOK, status)
		require.Contains(t, string(data), "Master5")
		require.Contains(t, string(data), "Addon3")

		status, data = call(t, srv, http.MethodPut,
			fmt.Sprintf("/api/client-templates/%d", int(masterID)), adminCookie, adminCSRF,
			map[string]any{"limit_dns_zone": 12})
		require.Equal(t, http.StatusOK, status, "%s", data)
	})

	t.Run("master template materializes on client save", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/clients", adminCookie, adminCSRF,
			map[string]any{
				"contact_name": "Tpl Client", "username": "tplclient",
				"password": "tpl-pw-longenough", "template_master": masterID,
			})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		clientID = int(rec["client_id"].(float64))

		var row model.Client
		require.NoError(t, db.Take(&row, clientID).Error)
		require.EqualValues(t, 5, row.LimitWebDomain)
		require.EqualValues(t, 12, row.LimitDNSZone)
		require.Equal(t, "y", row.LimitSSL)
	})

	t.Run("non-admin cannot manage the template catalog", func(t *testing.T) {
		cCookie, cCSRF := login(t, srv, "tplclient", "tpl-pw-longenough")
		status, _ := call(t, srv, http.MethodPost, "/api/client-templates", cCookie, cCSRF,
			map[string]any{"template_name": "Sneaky", "template_type": "m"})
		require.Equal(t, http.StatusForbidden, status)
	})

	t.Run("assign additional template re-materializes in the same tx", func(t *testing.T) {
		path := fmt.Sprintf("/api/clients/%d/templates", clientID)
		status, data := call(t, srv, http.MethodPost, path, adminCookie, adminCSRF,
			map[string]any{"template_id": 99999})
		require.Equal(t, http.StatusNotFound, status, "%s", data)

		status, data = call(t, srv, http.MethodPost, path, adminCookie, adminCSRF,
			map[string]any{"template_id": addonID})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		var assigned map[string]any
		require.NoError(t, json.Unmarshal(data, &assigned))
		require.Equal(t, "Addon3", assigned["template_name"])

		var row model.Client
		require.NoError(t, db.Take(&row, clientID).Error)
		require.EqualValues(t, 8, row.LimitWebDomain, "5 + 3")
		require.EqualValues(t, -1, row.LimitDNSZone, "additional -1 promotes")

		status, data = call(t, srv, http.MethodGet, path, adminCookie, "", nil)
		require.Equal(t, http.StatusOK, status)
		var list []map[string]any
		require.NoError(t, json.Unmarshal(data, &list))
		require.Len(t, list, 1)

		// Remove the assignment: limits fall back to the master alone.
		status, _ = call(t, srv, http.MethodDelete,
			fmt.Sprintf("%s/%v", path, list[0]["assigned_template_id"]), adminCookie, adminCSRF, nil)
		require.Equal(t, http.StatusNoContent, status)
		require.NoError(t, db.Take(&row, clientID).Error)
		require.EqualValues(t, 5, row.LimitWebDomain)
		require.EqualValues(t, 12, row.LimitDNSZone)

		status, _ = call(t, srv, http.MethodDelete,
			fmt.Sprintf("%s/%v", path, list[0]["assigned_template_id"]), adminCookie, adminCSRF, nil)
		require.Equal(t, http.StatusNotFound, status, "double delete")
	})

	t.Run("countries list", func(t *testing.T) {
		status, data := call(t, srv, http.MethodGet, "/api/countries", adminCookie, "", nil)
		require.Equal(t, http.StatusOK, status)
		var rows []model.Country
		require.NoError(t, json.Unmarshal(data, &rows))
		require.Greater(t, len(rows), 200, "full ISO list seeded by the schema")
		var de model.Country
		for _, r := range rows {
			if r.ISO == "DE" {
				de = r
			}
		}
		require.Equal(t, "Germany", de.PrintableName)
		require.Equal(t, "y", de.EU)
	})
}
