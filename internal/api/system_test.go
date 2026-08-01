package api

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/auth"
	"go-ispconfig/internal/config"
)

// TestSchedulerEndpointAdminOnly covers the access gate of
// GET /api/system/scheduler; the DB-backed listing runs in the integration
// suite. The nil-Deps handler proves the gate rejects before any DB access.
func TestSchedulerEndpointAdminOnly(t *testing.T) {
	e := echo.New()
	e.HTTPErrorHandler = ErrorHandler()
	sessions := stubSessions{
		"adm": {UserID: 1, Username: "admin", Typ: "admin"},
		"usr": {UserID: 2, Username: "client1", Typ: "user"},
	}
	g := e.Group("/api", auth.Middleware(sessions))
	protected := g.Group("", auth.RequireAuth())
	registerSystemRoutes(protected, &Deps{})

	get := func(bearer string) int {
		req := httptest.NewRequest(http.MethodGet, "/api/system/scheduler", nil)
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec.Code
	}

	require.Equal(t, http.StatusUnauthorized, get(""))
	require.Equal(t, http.StatusForbidden, get("usr"), "scheduler status is admin-only")
}

// proxyDeps builds Deps with parsed trusted proxy prefixes.
func proxyDeps(t *testing.T, cidrs ...string) *Deps {
	t.Helper()
	d := &Deps{Config: &config.Config{}}
	for _, c := range cidrs {
		p, err := netip.ParsePrefix(c)
		require.NoError(t, err)
		d.trustedProxies = append(d.trustedProxies, p)
	}
	return d
}

func TestClientIPTrustedProxies(t *testing.T) {
	e := echo.New()
	ctx := func(remoteAddr, xff string) *echo.Context {
		req := httptest.NewRequest(http.MethodPost, "/api/login", nil)
		req.RemoteAddr = remoteAddr
		if xff != "" {
			req.Header.Set("X-Forwarded-For", xff)
		}
		return e.NewContext(req, httptest.NewRecorder())
	}

	t.Run("no trusted proxies ignores forwarded headers", func(t *testing.T) {
		d := proxyDeps(t)
		require.Equal(t, "203.0.113.9", clientIP(ctx("203.0.113.9:1234", "6.6.6.6"), d))
	})

	t.Run("untrusted peer cannot spoof via XFF", func(t *testing.T) {
		d := proxyDeps(t, "10.0.0.0/8")
		require.Equal(t, "203.0.113.9", clientIP(ctx("203.0.113.9:1234", "6.6.6.6"), d))
	})

	t.Run("trusted proxy yields rightmost non-proxy hop", func(t *testing.T) {
		d := proxyDeps(t, "10.0.0.0/8")
		require.Equal(t, "198.51.100.7",
			clientIP(ctx("10.0.0.2:9999", "6.6.6.6, 198.51.100.7, 10.0.0.3"), d))
	})

	t.Run("trusted proxy without XFF falls back to peer", func(t *testing.T) {
		d := proxyDeps(t, "10.0.0.0/8")
		require.Equal(t, "10.0.0.2", clientIP(ctx("10.0.0.2:9999", ""), d))
	})
}

func TestIsSecureForwardedProto(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.2:9999"
	req.Header.Set("X-Forwarded-Proto", "https")
	c := e.NewContext(req, httptest.NewRecorder())

	require.False(t, isSecure(c, proxyDeps(t)), "untrusted peer proto header is ignored")
	require.True(t, isSecure(c, proxyDeps(t, "10.0.0.0/8")))
}
