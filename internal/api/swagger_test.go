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
	RegisterSwagger(e)

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
