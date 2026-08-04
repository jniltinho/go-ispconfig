package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/config"
)

func TestSwaggerUI(t *testing.T) {
	e := echo.New()
	RegisterSwagger(e, stubSessions{}, config.SwaggerConfig{Public: true})

	t.Run("index served at /swagger/", func(t *testing.T) {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/swagger/", nil))
		require.Equal(t, http.StatusOK, rec.Code)
		require.Contains(t, rec.Body.String(), "swagger-ui")
	})

	t.Run("doc.json is the generated spec", func(t *testing.T) {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/swagger/doc.json", nil))
		require.Equal(t, http.StatusOK, rec.Code)
		require.Contains(t, rec.Body.String(), `"/server_ip"`)
		require.Contains(t, rec.Body.String(), `"/login"`)
	})

	t.Run("initializer points at doc.json", func(t *testing.T) {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/swagger/swagger-initializer.js", nil))
		require.Equal(t, http.StatusOK, rec.Code)
		require.Contains(t, rec.Body.String(), "/swagger/doc.json")
	})

	t.Run("bare /swagger redirects", func(t *testing.T) {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/swagger", nil))
		require.Equal(t, http.StatusMovedPermanently, rec.Code)
	})
}

// TestSwaggerGated covers the default (non-public) mode: the UI and spec
// require an admin session.
func TestSwaggerGated(t *testing.T) {
	e := echo.New()
	sessions := stubSessions{
		"adm": {UserID: 1, Username: "admin", Typ: "admin"},
		"usr": {UserID: 2, Username: "client1", Typ: "user"},
	}
	RegisterSwagger(e, sessions, config.SwaggerConfig{})

	get := func(bearer string) int {
		req := httptest.NewRequest(http.MethodGet, "/swagger/doc.json", nil)
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec.Code
	}

	require.Equal(t, http.StatusUnauthorized, get(""), "anonymous access must be rejected")
	require.Equal(t, http.StatusForbidden, get("usr"), "non-admin session must be rejected")
	require.Equal(t, http.StatusOK, get("adm"))
}

// TestSwaggerConfig covers the [swagger] knobs: disabled drops the route,
// path moves it (initializer included).
func TestSwaggerConfig(t *testing.T) {
	get := func(cfg config.SwaggerConfig, target string) *httptest.ResponseRecorder {
		e := echo.New()
		RegisterSwagger(e, stubSessions{}, cfg)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		return rec
	}

	t.Run("disabled removes the route", func(t *testing.T) {
		require.Equal(t, http.StatusNotFound, get(config.SwaggerConfig{Disabled: true, Public: true}, "/swagger/").Code)
	})

	t.Run("custom path", func(t *testing.T) {
		cfg := config.SwaggerConfig{Public: true, Path: "/docs"}
		require.Equal(t, http.StatusNotFound, get(cfg, "/swagger/").Code)
		require.Contains(t, get(cfg, "/docs/").Body.String(), "swagger-ui")
		require.Contains(t, get(cfg, "/docs/swagger-initializer.js").Body.String(), `"/docs/doc.json"`)
		require.Equal(t, http.StatusMovedPermanently, get(cfg, "/docs").Code)
	})

	t.Run("path is normalized", func(t *testing.T) {
		require.Equal(t, "/swagger", swaggerPath(""))
		require.Equal(t, "/swagger", swaggerPath("/"))
		require.Equal(t, "/docs", swaggerPath("docs/"))
		require.Equal(t, "/api/docs", swaggerPath(" /api/docs "))
	})
}
