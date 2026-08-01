package api

import (
	"log/slog"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"

	"go-ispconfig/internal/auth"
)

// requestLogger returns the structured slog request-logging middleware
// applied to every /api request.
func requestLogger() echo.MiddlewareFunc {
	return middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		// HandleError runs the central error handler before logging so the
		// logged status is the one sent to the client (the handler's
		// committed-response check keeps Echo from handling it twice).
		HandleError: true,
		LogStatus:   true,
		LogMethod:   true,
		LogURIPath:  true,
		LogLatency:  true,
		LogRemoteIP: true,
		LogValuesFunc: func(c *echo.Context, v middleware.RequestLoggerValues) error {
			attrs := []any{
				"method", v.Method,
				"path", v.URIPath,
				"status", v.Status,
				"latency_ms", v.Latency.Milliseconds(),
				"remote_ip", v.RemoteIP,
			}
			if sess := auth.FromContext(c); sess != nil {
				attrs = append(attrs, "user", sess.Username)
			}
			if v.Error != nil {
				attrs = append(attrs, "error", v.Error.Error())
			}
			slog.Info("api request", attrs...)
			return nil
		},
	})
}
