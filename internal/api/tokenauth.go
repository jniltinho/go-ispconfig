package api

// API token authentication (spec add-api-tokens): resolves an
// `Authorization: Bearer goisp_…` credential, or a JWT exchanged from one,
// into the sys_user that owns it, and enforces the token's scopes on every
// route it reaches.

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v5"

	"go-ispconfig/internal/apitoken"
	"go-ispconfig/internal/auth"
	"go-ispconfig/internal/model"
	"go-ispconfig/internal/repository"
)

// tokenFailWindow and tokenFailLimit throttle credential guessing per source
// IP. The token id is public by design and the secret is 256 bits, so this is
// belt-and-braces against noise rather than the primary defence.
const (
	tokenFailWindow = time.Minute
	tokenFailLimit  = 20
)

// tokenFailures counts recent failed token verifications per caller IP.
var tokenFailures sync.Map

type failCounter struct {
	mu    sync.Mutex
	count int
	until time.Time
}

// tokenAuthMiddleware authenticates API tokens and JWTs. It runs *before*
// auth.Middleware and deliberately does nothing when a session cookie is
// present: a cookie request stays a cookie request, keeping its CSRF
// requirement, even if it also carries an Authorization header (design D6).
func tokenAuthMiddleware(d *Deps) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			bearer := bearerValue(c.Request())
			if bearer == "" {
				return next(c)
			}
			if ck, err := c.Request().Cookie(auth.SessionCookieName); err == nil && ck.Value != "" {
				return next(c)
			}
			isJWT := apitoken.LooksJWT(bearer)
			if !isJWT && !apitoken.Looks(bearer) {
				// A plain session id: auth.Middleware owns it.
				return next(c)
			}
			if !d.remoteAPIAllowed(c) {
				return echo.NewHTTPError(http.StatusForbidden, "the remote API is disabled")
			}

			ip := clientIP(c, d)
			if throttled(ip) {
				return echo.NewHTTPError(http.StatusTooManyRequests, "too many failed token attempts")
			}

			var (
				data *auth.SessionData
				cred *auth.Credential
				err  error
				now  = time.Now()
			)
			if isJWT {
				data, cred, err = d.authenticateJWT(c, bearer, now)
			} else {
				data, cred, err = d.authenticateToken(c, bearer, ip, now)
			}
			if err != nil {
				recordFailure(ip)
				return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
			}
			clearFailures(ip)

			auth.SetSession(c, data)
			auth.SetCredential(c, cred)
			return next(c)
		}
	}
}

// authenticateToken resolves an opaque API token.
func (d *Deps) authenticateToken(c *echo.Context, bearer, ip string, now time.Time) (*auth.SessionData, *auth.Credential, error) {
	v, err := apitoken.Verify(c.Request().Context(), d.DB, bearer, ip, now)
	if err != nil {
		return nil, nil, err
	}
	return d.sessionForOwner(c, v.Owner),
		&auth.Credential{TokenID: v.ID, Label: v.Label, Scopes: v.Scopes},
		nil
}

// authenticateJWT resolves an exchanged JWT. Verification is signature and
// expiry only — no database read — which is exactly what the short TTL buys
// (design D5); the owner is then loaded to build the identity.
func (d *Deps) authenticateJWT(c *echo.Context, bearer string, now time.Time) (*auth.SessionData, *auth.Credential, error) {
	claims, err := apitoken.ParseJWT(d.Config.Auth.JWTSecret, bearer, now)
	if err != nil {
		return nil, nil, err
	}
	var owner model.SysUser
	err = d.DB.WithContext(c.Request().Context()).Where("userid = ?", claims.Sub).Take(&owner).Error
	if err != nil || owner.Active != 1 {
		return nil, nil, apitoken.ErrDenied
	}
	return d.sessionForOwner(c, owner),
		&auth.Credential{TokenID: claims.TID, Label: "jwt", Scopes: claims.Scope, JWT: true},
		nil
}

// sessionForOwner builds the same SessionData a login would produce for the
// owning user, so every downstream permission check — WithPerm, requireAdmin,
// AdminOnly entities, security policies — behaves identically.
func (d *Deps) sessionForOwner(c *echo.Context, owner model.SysUser) *auth.SessionData {
	id, err := repository.ResolveIdentity(d.DB.WithContext(c.Request().Context()), &owner)
	if err != nil {
		// Group resolution failed: fall back to the user's own default group
		// so the request is scoped narrowly rather than broadly.
		id = &repository.Identity{DefaultGroup: owner.DefaultGroup}
	}
	return &auth.SessionData{
		UserID:       owner.UserID,
		Username:     owner.Username,
		Typ:          owner.Typ,
		Groups:       id.Groups,
		DefaultGroup: id.DefaultGroup,
		Language:     owner.Language,
		Modules:      owner.Modules,
	}
}

// requireScope enforces the token's grants. It is a no-op for session
// credentials: a human login is already bounded by its own permissions, and
// making the SPA carry scopes would only be a second copy of the same rules.
func requireScope() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			cred := auth.CredentialFromContext(c)
			if cred == nil {
				return next(c)
			}
			resource := apitoken.ResourceForPath(c.Request().URL.Path)
			if resource == "" {
				// Session/metadata endpoints describe the surface the caller
				// may already reach; they carry no resource of their own.
				return next(c)
			}
			action := apitoken.ActionFor(c.Request().Method)
			if !apitoken.Allows(cred.Scopes, resource, action) {
				return &ScopeError{Resource: resource, Action: action}
			}
			return next(c)
		}
	}
}

// ScopeError is returned when a valid credential lacks the route's scope. It
// is a distinct error from unauthenticated and from record permission denial
// so a caller can tell "wrong credential" from "insufficient grant".
type ScopeError struct {
	// Resource is the resource group the route belongs to.
	Resource string
	// Action is the action the method requires (read or write).
	Action string
}

// Error implements error.
func (e *ScopeError) Error() string {
	return "missing scope " + e.Resource + ":" + e.Action
}

// Scope returns the scope string the caller would need.
func (e *ScopeError) Scope() string { return e.Resource + ":" + e.Action }

// remoteAPIAllowed reports whether the token front door is enabled at all
// (the ISPConfig3 remote_api_allowed security flag).
func (d *Deps) remoteAPIAllowed(c *echo.Context) bool {
	value, err := auth.GetPolicy(d.DB.WithContext(c.Request().Context()), "remote_api_allowed")
	return err == nil && value == "yes"
}

// bearerValue extracts the Authorization bearer value, empty when absent.
func bearerValue(r *http.Request) string {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(h, prefix))
}

func throttled(ip string) bool {
	v, ok := tokenFailures.Load(ip)
	if !ok {
		return false
	}
	f, ok := v.(*failCounter)
	if !ok {
		return false
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if time.Now().After(f.until) {
		f.count = 0
		return false
	}
	return f.count >= tokenFailLimit
}

func recordFailure(ip string) {
	v, _ := tokenFailures.LoadOrStore(ip, &failCounter{})
	f, ok := v.(*failCounter)
	if !ok {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now()
	if now.After(f.until) {
		f.count = 0
		f.until = now.Add(tokenFailWindow)
	}
	f.count++
}

func clearFailures(ip string) { tokenFailures.Delete(ip) }
