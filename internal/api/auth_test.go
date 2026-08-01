package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/auth"
)

// stubSessions is an in-memory auth.SessionGetter for handler tests.
type stubSessions map[string]*auth.SessionData

// Get implements auth.SessionGetter.
func (s stubSessions) Get(id string) (*auth.SessionData, error) {
	if data, ok := s[id]; ok {
		return data, nil
	}
	return nil, auth.ErrSessionNotFound
}

func TestSessionEndpoint(t *testing.T) {
	e := echo.New()
	e.HTTPErrorHandler = ErrorHandler()
	sessions := stubSessions{"tok1": {
		UserID:   1,
		Username: "admin",
		Typ:      "admin",
		Groups:   []uint32{1},
		Language: "en",
	}}
	g := e.Group("/api", auth.Middleware(sessions))
	g.GET("/session", sessionHandler(), auth.RequireAuth())

	t.Run("without session returns 401", func(t *testing.T) {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/session", nil))
		require.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("bearer session returns user info", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/session", nil)
		req.Header.Set("Authorization", "Bearer tok1")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		var info SessionInfo
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &info))
		require.Equal(t, "admin", info.Username)
		require.Equal(t, "admin", info.Typ)
		require.Equal(t, []uint32{1}, info.Groups)
	})
}
