// Package api implements the REST API core (design D4, D5, D11): the /api
// Echo group with session auth, structured request logging, a central error
// handler emitting i18n error keys, the declarative CRUD entity framework
// and the form metadata endpoint consumed by the SPA.
package api

import (
	"fmt"
	"net/netip"

	"github.com/labstack/echo/v5"
	echoMiddleware "github.com/labstack/echo/v5/middleware"
	"gorm.io/gorm"

	"go-ispconfig/internal/auth"
	"go-ispconfig/internal/clients"
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
	// Mailer is the optional SMTP transport for client messaging; nil
	// means sending is not configured.
	Mailer clients.Mailer
	// DNSPub publishes DKIM TXT records into managed zones; nil means
	// the no-op publisher (DNS module disabled).
	DNSPub DNSPublisher

	// trustedProxies is Config.Server.TrustedProxies parsed by Register.
	trustedProxies []netip.Prefix
}

// Register mounts the /api group on e: session middleware, structured
// request logging, a 4M request body limit, the central error handler, the
// auth endpoints and the registered CRUD entities. It replaces Echo's
// default HTTP error handler with the i18n-key JSON one.
func Register(e *echo.Echo, d *Deps) error {
	e.HTTPErrorHandler = ErrorHandler()

	for _, cidr := range d.Config.Server.TrustedProxies {
		p, err := netip.ParsePrefix(cidr)
		if err != nil {
			return fmt.Errorf("api: invalid server.trusted_proxies entry %q: %w", cidr, err)
		}
		d.trustedProxies = append(d.trustedProxies, p)
	}

	g := e.Group("/api", requestLogger(), echoMiddleware.BodyLimit(4<<20), auth.Middleware(d.Sessions))
	registerAuthRoutes(g, d)

	protected := g.Group("", auth.RequireAuth())
	registerMetaRoutes(protected, d)
	registerSystemRoutes(protected, d)
	return registerEntities(protected, d)
}
