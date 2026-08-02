//go:build integration

package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
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
	"go-ispconfig/internal/model"
	"go-ispconfig/internal/repository"
)

// bootAPI starts the API server with the client limit hook registered.
func bootAPI(t *testing.T, db *gorm.DB) *httptest.Server {
	t.Helper()
	RegisterLimits(db)
	t.Cleanup(func() {
		api.RegisterLimitHook(func(context.Context, string, *repository.Identity, map[string]any) error { return nil })
	})
	e := echo.New()
	e.Use(echoMiddleware.Recover())
	deps := &api.Deps{DB: db, Sessions: auth.NewStore(db, 0), Config: &config.Config{}}
	require.NoError(t, api.Register(e, deps))
	srv := httptest.NewServer(e)
	t.Cleanup(srv.Close)
	return srv
}

// login returns session cookie + csrf token for a username/password.
func login(t *testing.T, srv *httptest.Server, username, password string) (string, string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	resp, err := srv.Client().Post(srv.URL+"/api/login", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var out struct {
		CSRFToken string `json:"csrf_token"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	var cookie string
	for _, ck := range resp.Cookies() {
		if ck.Name == auth.SessionCookieName {
			cookie = ck.Value
		}
	}
	require.NotEmpty(t, cookie)
	return cookie, out.CSRFToken
}

// post issues an authenticated JSON POST and returns status + body.
func post(t *testing.T, srv *httptest.Server, path, cookie, csrf string, body any) (int, string) {
	t.Helper()
	payload, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, srv.URL+path, bytes.NewReader(payload))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: cookie})
	req.Header.Set(auth.CSRFHeaderName, csrf)
	resp, err := srv.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(data)
}

func TestLimitHookEnforcement(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	// Client with tight limits: 1 zone, 0 websites, unlimited slaves.
	hash, err := auth.HashPassword("limit-pw-X9!")
	require.NoError(t, err)
	c := insertClient(t, db, "limited", 0, 0)
	require.NoError(t, db.Model(&model.Client{}).Where("client_id = ?", c.ClientID).Updates(map[string]any{
		"limit_dns_zone": 1, "limit_web_domain": 0, "limit_dns_slave_zone": -1,
	}).Error)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return ProvisionIdentity(ctx, tx, c, hash, "admin")
	}))

	srv := bootAPI(t, db)
	cookie, csrf := login(t, srv, "limited", "limit-pw-X9!")

	zoneBody := map[string]any{
		"server_id": 1, "origin": "one.example.com.", "ns": "ns1.example.com.",
		"mbox": "hostmaster.example.com.", "active": "Y",
	}

	t.Run("first zone within limit is created", func(t *testing.T) {
		status, data := post(t, srv, "/api/dns/zones", cookie, csrf, zoneBody)
		require.Equal(t, http.StatusCreated, status, "%s", data)
	})

	t.Run("second zone at limit is vetoed 403", func(t *testing.T) {
		body := map[string]any{}
		for k, v := range zoneBody {
			body[k] = v
		}
		body["origin"] = "two.example.com."
		status, data := post(t, srv, "/api/dns/zones", cookie, csrf, body)
		require.Equal(t, http.StatusForbidden, status, "%s", data)
		require.Contains(t, data, "error.limit_dns_zone")
		var n int64
		require.NoError(t, db.Model(&model.SysDatalog{}).
			Where("dbtable = ? AND data LIKE ?", "dns_soa", "%two.example.com%").Count(&n).Error)
		require.Zero(t, n, "vetoed create must not journal")
	})

	t.Run("zero limit blocks websites entirely", func(t *testing.T) {
		status, data := post(t, srv, "/api/sites/web-domains", cookie, csrf, map[string]any{
			"server_id": 1, "domain": "blocked.example.com", "type": "vhost",
		})
		require.Equal(t, http.StatusForbidden, status, "%s", data)
		require.Contains(t, data, "error.limit_web_domain")
	})

	t.Run("unlimited slave zones are not vetoed", func(t *testing.T) {
		status, data := post(t, srv, "/api/dns/slave-zones", cookie, csrf, map[string]any{
			"server_id": 1, "origin": "slave.example.net.", "ns": "192.0.2.53", "active": "Y",
		})
		require.Equal(t, http.StatusCreated, status, "%s", data)
	})

	t.Run("admin bypasses count limits", func(t *testing.T) {
		adminCookie, adminCSRF := seedAdminLogin(t, db, srv)
		body := map[string]any{}
		for k, v := range zoneBody {
			body[k] = v
		}
		body["origin"] = "admin.example.com."
		status, data := post(t, srv, "/api/dns/zones", adminCookie, adminCSRF, body)
		require.Equal(t, http.StatusCreated, status, "%s", data)
	})

	t.Run("child client counting under a reseller", func(t *testing.T) {
		hook := LimitHook(db)
		reseller := insertClient(t, db, "capre", 2, 0)
		require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
			return ProvisionIdentity(ctx, tx, reseller, hash, "admin")
		}))
		for _, kid := range []string{"kid1", "kid2"} {
			k := insertClient(t, db, kid, 0, reseller.ClientID)
			require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
				return ProvisionIdentity(ctx, tx, k, hash, "admin")
			}))
		}
		var ru model.SysUser
		require.NoError(t, db.Where("client_id = ?", reseller.ClientID).Take(&ru).Error)
		rid := &repository.Identity{UserID: ru.UserID, Username: ru.Username, Typ: "user"}

		err := hook(ctx, "clients", rid, map[string]any{})
		var limErr *api.LimitError
		require.ErrorAs(t, err, &limErr, "third child at limit_client=2 must veto")
		require.Equal(t, "error.limit_client", limErr.Key)

		require.NoError(t, db.Model(&model.Client{}).Where("client_id = ?", reseller.ClientID).
			Update("limit_client", -1).Error)
		require.NoError(t, hook(ctx, "clients", rid, map[string]any{}), "unlimited reseller may create")
	})

	t.Run("unknown entity is never vetoed", func(t *testing.T) {
		var u model.SysUser
		require.NoError(t, db.Where("username = ?", "limited").Take(&u).Error)
		uid := &repository.Identity{UserID: u.UserID, Username: u.Username, Typ: "user"}
		require.NoError(t, LimitHook(db)(ctx, "mail-domains", uid, nil))
	})
}

// seedAdminLogin sets a known admin password and logs in.
func seedAdminLogin(t *testing.T, db *gorm.DB, srv *httptest.Server) (string, string) {
	t.Helper()
	hash, err := auth.HashPassword("admin-pw-Z8!")
	require.NoError(t, err)
	require.NoError(t, db.Model(&model.SysUser{}).Where("userid = 1").
		Update("passwort", hash).Error)
	return login(t, srv, "admin", "admin-pw-Z8!")
}
