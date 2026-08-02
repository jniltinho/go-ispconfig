package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v5"
	"gorm.io/gorm"

	"go-ispconfig/internal/repository"
)

// ErrorInfo is the payload of every API error response: an i18n message key
// plus, for validation failures, a per-field map of i18n keys.
type ErrorInfo struct {
	// Key is the i18n message key describing the error.
	Key string `json:"key" example:"error.permission_denied"`
	// Fields maps field names to their i18n validation error keys
	// (422 responses only).
	Fields map[string][]string `json:"fields,omitempty"`
}

// ErrorResponse is the JSON body of every non-2xx API response.
type ErrorResponse struct {
	// Error describes what went wrong.
	Error ErrorInfo `json:"error"`
}

// ValidationError carries the per-field i18n error keys of a failed entity
// validation; the error handler renders it as 422.
type ValidationError struct {
	// Fields maps field names to i18n error keys.
	Fields map[string][]string
}

// Error implements the error interface.
func (e *ValidationError) Error() string { return "api: validation failed" }

// LimitError is a limit-hook veto (design D-limit): the error handler
// renders it as 403 with the hook's i18n key.
type LimitError struct {
	// Key is the i18n message key explaining the exceeded limit.
	Key string
}

// Error implements the error interface.
func (e *LimitError) Error() string { return "api: limit exceeded: " + e.Key }

// LimitKey satisfies the error handler's limit-veto interface.
func (e *LimitError) LimitKey() string { return e.Key }

// statusKeys maps HTTP statuses raised via echo.HTTPError (middlewares,
// route misses) to their i18n keys.
var statusKeys = map[int]string{
	http.StatusBadRequest:      "error.bad_request",
	http.StatusUnauthorized:    "error.unauthorized",
	http.StatusForbidden:       "error.forbidden",
	http.StatusNotFound:        "error.not_found",
	http.StatusTooManyRequests: "error.too_many_requests",
}

// ErrorHandler returns the central Echo error handler: it converts known
// errors (permission denied → 403, validation → 422 with a field map,
// record not found → 404, limit veto → 403) into the ErrorResponse JSON
// shape with i18n keys and logs everything else as 500.
func ErrorHandler() echo.HTTPErrorHandler {
	return func(c *echo.Context, err error) {
		if r, _ := echo.UnwrapResponse(c.Response()); r != nil && r.Committed {
			return
		}

		status := http.StatusInternalServerError
		info := ErrorInfo{Key: "error.internal"}

		var (
			valErr   *ValidationError
			limErr   interface{ LimitKey() string }
			httpErr  *echo.HTTPError
			bindErr  *echo.BindingError
			validate = errors.As(err, &valErr)
		)
		switch {
		case validate:
			status = http.StatusUnprocessableEntity
			info = ErrorInfo{Key: "error.validation_failed", Fields: valErr.Fields}
		case errors.As(err, &limErr):
			status = http.StatusForbidden
			info = ErrorInfo{Key: limErr.LimitKey()}
		case errors.Is(err, repository.ErrPermissionDenied):
			status = http.StatusForbidden
			info = ErrorInfo{Key: "error.permission_denied"}
		case errors.Is(err, gorm.ErrRecordNotFound):
			status = http.StatusNotFound
			info = ErrorInfo{Key: "error.not_found"}
		case errors.As(err, &bindErr):
			status = http.StatusBadRequest
			info = ErrorInfo{Key: "error.bad_request"}
		case errors.As(err, &httpErr):
			status = httpErr.Code
			if key, ok := statusKeys[status]; ok {
				info = ErrorInfo{Key: key}
			} else {
				info = ErrorInfo{Key: "error.internal"}
			}
		}

		if status >= http.StatusInternalServerError {
			slog.Error("api: unhandled error",
				"method", c.Request().Method,
				"path", c.Request().URL.Path,
				"error", err)
		}

		var respErr error
		if c.Request().Method == http.MethodHead {
			respErr = c.NoContent(status)
		} else {
			respErr = c.JSON(status, ErrorResponse{Error: info})
		}
		if respErr != nil {
			slog.Error("api: writing error response", "error", respErr)
		}
	}
}
