package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

func TestSwaggerUI(t *testing.T) {
	e := echo.New()
	RegisterSwagger(e, stubSessions{}, true)

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
	RegisterSwagger(e, sessions, false)

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
