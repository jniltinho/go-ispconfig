package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

func TestFormMetaEndpoint(t *testing.T) {
	ent := serverIPEntity()
	registryMu.Lock()
	registry[ent.Name] = ent
	registryMu.Unlock()
	t.Cleanup(func() {
		registryMu.Lock()
		delete(registry, ent.Name)
		registryMu.Unlock()
	})

	e := echo.New()
	e.HTTPErrorHandler = ErrorHandler()
	registerMetaRoutes(e.Group("/api"))

	t.Run("returns tabs, fields, defaults and validator hints", func(t *testing.T) {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/meta/forms/server_ip", nil))
		require.Equal(t, http.StatusOK, rec.Code)

		var meta FormMeta
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &meta))
		require.Equal(t, "server_ip", meta.Name)
		require.Len(t, meta.Tabs, 1)

		byName := map[string]FormFieldMeta{}
		for _, f := range meta.Tabs[0].Fields {
			byName[f.Name] = f
		}
		require.Equal(t, "select", byName["ip_type"].Type)
		require.Equal(t, "IPv4", byName["ip_type"].Default)
		require.Equal(t, "checkbox", byName["virtualhost"].Type)
		require.Equal(t, "text", byName["ip_address"].Type)
		require.Len(t, byName["ip_address"].Validators, 2)
		require.Equal(t, "UNIQUE", byName["ip_address"].Validators[1].Type)
		require.Equal(t, "80,443", byName["virtualhost_port"].Default)
	})

	t.Run("unknown entity is 404", func(t *testing.T) {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/meta/forms/nope", nil))
		require.Equal(t, http.StatusNotFound, rec.Code)
	})
}
