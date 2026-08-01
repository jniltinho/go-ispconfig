package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

// fakeStore is an in-memory SessionGetter for middleware tests; the real
// sys_session-backed Store is covered by the repository integration suite.
type fakeStore map[string]*SessionData

// Get implements SessionGetter.
func (f fakeStore) Get(id string) (*SessionData, error) {
	if d, ok := f[id]; ok {
		return d, nil
	}
	return nil, ErrSessionNotFound
}

func newTestApp(store SessionGetter) *echo.Echo {
	e := echo.New()
	e.Use(Middleware(store))
	protected := e.Group("/api", RequireAuth())
	handler := func(c *echo.Context) error {
		return c.String(http.StatusOK, FromContext(c).Username)
	}
	protected.GET("/me", handler)
	protected.POST("/things", handler)
	protected.DELETE("/things", handler)
	return e
}

func do(e *echo.Echo, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestSessionMiddleware(t *testing.T) {
	store := fakeStore{
		"sid-1": {UserID: 2, Username: "client1", Typ: "user", CSRFToken: "csrf-1"},
	}
	e := newTestApp(store)

	t.Run("no session gets 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
		require.Equal(t, http.StatusUnauthorized, do(e, req).Code)
	})

	t.Run("invalid session id gets 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
		req.AddCookie(Cookie("nope", false))
		require.Equal(t, http.StatusUnauthorized, do(e, req).Code)
	})

	t.Run("cookie GET ok without CSRF token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
		req.AddCookie(Cookie("sid-1", false))
		rec := do(e, req)
		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, "client1", rec.Body.String())
	})

	t.Run("cookie POST without CSRF token gets 403", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/things", nil)
		req.AddCookie(Cookie("sid-1", false))
		require.Equal(t, http.StatusForbidden, do(e, req).Code)
	})

	t.Run("cookie DELETE with wrong CSRF token gets 403", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/things", nil)
		req.AddCookie(Cookie("sid-1", false))
		req.Header.Set(CSRFHeaderName, "wrong")
		require.Equal(t, http.StatusForbidden, do(e, req).Code)
	})

	t.Run("cookie POST with valid CSRF token ok", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/things", nil)
		req.AddCookie(Cookie("sid-1", false))
		req.Header.Set(CSRFHeaderName, "csrf-1")
		require.Equal(t, http.StatusOK, do(e, req).Code)
	})

	t.Run("bearer transport skips CSRF", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/things", nil)
		req.Header.Set("Authorization", "Bearer sid-1")
		rec := do(e, req)
		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, "client1", rec.Body.String())
	})
}

func TestCookieAttributes(t *testing.T) {
	ck := Cookie("abc", true)
	require.Equal(t, SessionCookieName, ck.Name)
	require.True(t, ck.HttpOnly)
	require.True(t, ck.Secure)
	require.Equal(t, http.SameSiteStrictMode, ck.SameSite)
	require.Equal(t, "/", ck.Path)

	cleared := ClearCookie(false)
	require.Equal(t, -1, cleared.MaxAge)
	require.Empty(t, cleared.Value)
}

func TestRandomToken(t *testing.T) {
	a, err := randomToken()
	require.NoError(t, err)
	b, err := randomToken()
	require.NoError(t, err)
	require.Len(t, a, 64) // fits varchar(64) session_id exactly
	require.NotEqual(t, a, b)
}
