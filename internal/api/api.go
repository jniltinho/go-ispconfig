// Package api implements the REST API core (design D4, D5, D11): the /api
// Echo group with session auth, structured request logging, a central error
// handler emitting i18n error keys, the declarative CRUD entity framework
// and the form metadata endpoint consumed by the SPA.
package api

import (
	"github.com/labstack/echo/v5"
	"gorm.io/gorm"

	"go-ispconfig/internal/auth"
	"go-ispconfig/internal/config"
)

// General swagger API info (swag init -g internal/api/api.go).
//
//	@title						go-ispconfig API
//	@version					1.0
//	@description				REST API of go-ispconfig, the Go port of ISPConfig3. Authenticate via POST /api/login: browsers receive an HTTP-only session cookie (mutating requests then require the X-CSRF-Token header returned by login); non-browser clients present the same session id as a bearer token.
//	@BasePath					/api
//
//	@securityDefinitions.apikey	BearerAuth
//	@in							header
//	@name						Authorization
//	@description				Session id as bearer token: "Bearer <session id>". No CSRF token required on this transport.
//
//	@securityDefinitions.apikey	CookieAuth
//	@in							header
//	@name						Cookie
//	@description				Browser transport: goisp_session HTTP-only cookie set by POST /api/login. Mutating requests must also send the X-CSRF-Token header.

// Deps carries the shared dependencies of every API handler.
type Deps struct {
	// DB is the GORM handle to the ISPConfig database.
	DB *gorm.DB
	// Sessions is the sys_session-backed session store.
	Sessions *auth.Store
	// Config is the loaded process configuration.
	Config *config.Config
}

// Register mounts the /api group on e: session middleware, structured
// request logging, the central error handler, the auth endpoints and the
// registered CRUD entities. It replaces Echo's default HTTP error handler
// with the i18n-key JSON one.
func Register(e *echo.Echo, d *Deps) error {
	e.HTTPErrorHandler = ErrorHandler()

	g := e.Group("/api", requestLogger(), auth.Middleware(d.Sessions))
	_ = g // auth endpoints and CRUD entities are mounted by the next tasks
	return nil
}
