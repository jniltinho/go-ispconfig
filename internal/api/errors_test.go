package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"go-ispconfig/internal/repository"
)

// fire runs the central error handler for err and returns the recorded
// response.
func fire(t *testing.T, err error) *httptest.ResponseRecorder {
	t.Helper()
	e := echo.New()
	e.HTTPErrorHandler = ErrorHandler()
	e.GET("/api/boom", func(c *echo.Context) error { return err })
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/boom", nil))
	return rec
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) ErrorResponse {
	t.Helper()
	var body ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body
}

func TestErrorHandler(t *testing.T) {
	t.Run("echo's own sentinels keep their status", func(t *testing.T) {
		// Regression: these are an unexported type implementing only
		// HTTPStatusCoder, so an errors.As on *echo.HTTPError missed them
		// and every route miss was served as 500 error.internal.
		for _, tc := range []struct {
			err  error
			code int
			key  string
		}{
			{echo.ErrNotFound, http.StatusNotFound, "error.not_found"},
			{echo.ErrMethodNotAllowed, http.StatusMethodNotAllowed, "error.method_not_allowed"},
			{echo.ErrStatusRequestEntityTooLarge, http.StatusRequestEntityTooLarge, "error.request_too_large"},
			{echo.ErrTooManyRequests, http.StatusTooManyRequests, "error.too_many_requests"},
		} {
			rec := fire(t, tc.err)
			require.Equal(t, tc.code, rec.Code, tc.err)
			require.Equal(t, tc.key, decodeError(t, rec).Error.Key)
		}
	})

	t.Run("an unrouted path is 404, not 500", func(t *testing.T) {
		e := echo.New()
		e.HTTPErrorHandler = ErrorHandler()
		e.GET("/api/known", func(c *echo.Context) error { return nil })
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/nope", nil))
		require.Equal(t, http.StatusNotFound, rec.Code)
		require.Equal(t, "error.not_found", decodeError(t, rec).Error.Key)
	})

	t.Run("permission denied is 403", func(t *testing.T) {
		rec := fire(t, repository.ErrPermissionDenied)
		require.Equal(t, http.StatusForbidden, rec.Code)
		require.Equal(t, "error.permission_denied", decodeError(t, rec).Error.Key)
	})

	t.Run("validation error is 422 with field map", func(t *testing.T) {
		rec := fire(t, &ValidationError{Fields: map[string][]string{
			"ip_address": {"ip_error_wrong"},
		}})
		require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
		body := decodeError(t, rec)
		require.Equal(t, "error.validation_failed", body.Error.Key)
		require.Equal(t, []string{"ip_error_wrong"}, body.Error.Fields["ip_address"])
	})

	t.Run("limit veto is 403 with hook key", func(t *testing.T) {
		rec := fire(t, &LimitError{Key: "error.limit_reached"})
		require.Equal(t, http.StatusForbidden, rec.Code)
		require.Equal(t, "error.limit_reached", decodeError(t, rec).Error.Key)
	})

	t.Run("record not found is 404", func(t *testing.T) {
		rec := fire(t, gorm.ErrRecordNotFound)
		require.Equal(t, http.StatusNotFound, rec.Code)
		require.Equal(t, "error.not_found", decodeError(t, rec).Error.Key)
	})

	t.Run("http error keeps status with mapped key", func(t *testing.T) {
		rec := fire(t, echo.NewHTTPError(http.StatusUnauthorized, "authentication required"))
		require.Equal(t, http.StatusUnauthorized, rec.Code)
		require.Equal(t, "error.unauthorized", decodeError(t, rec).Error.Key)
	})

	t.Run("unknown error is 500 internal", func(t *testing.T) {
		rec := fire(t, echo.ErrInternalServerError)
		require.Equal(t, http.StatusInternalServerError, rec.Code)
		require.Equal(t, "error.internal", decodeError(t, rec).Error.Key)
	})
}
