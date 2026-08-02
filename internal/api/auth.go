package api

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/labstack/echo/v5"
	"gorm.io/gorm"

	"go-ispconfig/internal/auth"
	"go-ispconfig/internal/model"
	"go-ispconfig/internal/repository"
)

// LoginRequest is the POST /api/login body.
type LoginRequest struct {
	// Username is the sys_user login name.
	Username string `json:"username" example:"admin"`
	// Password is the cleartext password.
	Password string `json:"password" example:"secret"`
	// StayLoggedIn requests a long-lived session (30 days sliding
	// expiration, sys_session.permanent) instead of the 1-hour idle TTL.
	StayLoggedIn bool `json:"stay_logged_in"`
}

// LoginResponse is the POST /api/login success body.
type LoginResponse struct {
	// Username is the authenticated login name.
	Username string `json:"username" example:"admin"`
	// Typ is the access level ("admin" or "user").
	Typ string `json:"typ" example:"admin"`
	// CSRFToken must be sent as X-CSRF-Token on every mutating
	// cookie-authenticated request.
	CSRFToken string `json:"csrf_token"`
	// SessionID is the session id, also set as the HTTP-only cookie.
	// Non-browser clients present it as "Authorization: Bearer <id>".
	SessionID string `json:"session_id"`
}

// SessionInfo is the GET /api/session body describing the logged-in user.
type SessionInfo struct {
	// Username is the sys_user login name.
	Username string `json:"username" example:"admin"`
	// Typ is the access level ("admin" or "user").
	Typ string `json:"typ" example:"admin"`
	// Groups is the effective sys_group id list of the user.
	Groups []uint32 `json:"groups"`
	// Modules is the list of panel modules the user may see (sys_user
	// .modules); the SPA hides top-nav entries not listed here (admins
	// see everything regardless).
	Modules []string `json:"modules"`
	// Language is the panel language of the user.
	Language string `json:"language" example:"en"`
	// CSRFToken is the per-session token for the X-CSRF-Token header, so a
	// reloaded SPA can recover it without logging in again.
	CSRFToken string `json:"csrf_token"`
}

// registerAuthRoutes mounts login/logout/session on the /api group.
func registerAuthRoutes(g *echo.Group, d *Deps) {
	g.POST("/login", loginHandler(d))
	g.POST("/logout", logoutHandler(d))
	g.GET("/session", sessionHandler(), auth.RequireAuth())
}

// loginHandler implements POST /api/login.
//
//	@Summary		Log in
//	@Description	Verifies sys_user credentials (bcrypt or legacy ISPConfig3 crypt hashes) with brute-force lockout, creates a sys_session and returns the CSRF token and session id. The session id is also set as an HTTP-only cookie; non-browser clients may present it as a bearer token instead. With stay_logged_in the session lives 30 days (sliding) instead of the 1-hour idle TTL.
//	@Tags			auth
//	@Accept			json
//	@Produce		json
//	@Param			credentials	body		LoginRequest	true	"Login credentials"
//	@Success		200			{object}	LoginResponse
//	@Failure		401			{object}	ErrorResponse	"Invalid credentials"
//	@Failure		429			{object}	ErrorResponse	"Too many failed attempts"
//	@Router			/login [post]
func loginHandler(d *Deps) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var req LoginRequest
		if err := c.Bind(&req); err != nil {
			return err
		}
		remote := clientIP(c, d)

		blocked, err := repository.TooManyLoginAttempts(d.DB, remote)
		if err != nil {
			return err
		}
		if blocked {
			return echo.NewHTTPError(http.StatusTooManyRequests, "too many failed login attempts")
		}

		var user model.SysUser
		err = d.DB.Where("username = ? AND active = 1", req.Username).First(&user).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		ok, newHash, hashErr := auth.VerifyAndMaybeRehash(req.Password, user.Passwort, d.Config.Auth.RehashLegacy)
		if hashErr != nil {
			return hashErr
		}
		if errors.Is(err, gorm.ErrRecordNotFound) || !ok {
			if recErr := repository.RecordFailedLogin(d.DB, remote); recErr != nil {
				return recErr
			}
			return echo.NewHTTPError(http.StatusUnauthorized, "invalid credentials")
		}
		if newHash != "" {
			if err := d.DB.Model(&user).Update("passwort", newHash).Error; err != nil {
				return err
			}
		}
		if err := repository.ClearLoginAttempts(d.DB, remote); err != nil {
			return err
		}

		ident, err := repository.ResolveIdentity(d.DB, &user)
		if err != nil {
			return err
		}
		data := &auth.SessionData{
			UserID:       ident.UserID,
			Username:     ident.Username,
			Typ:          ident.Typ,
			Groups:       ident.Groups,
			DefaultGroup: ident.DefaultGroup,
			Language:     user.Language,
			Modules:      user.Modules,
			Permanent:    req.StayLoggedIn,
		}

		// Anti-fixation: any session id presented by the client — valid or
		// stale — is replaced by a fresh one on login.
		var sessionID string
		if oldID := requestSessionID(c); oldID != "" {
			sessionID, err = d.Sessions.Regenerate(oldID, data)
		} else {
			sessionID, err = d.Sessions.Create(data)
		}
		if err != nil {
			return err
		}
		c.SetCookie(auth.Cookie(sessionID, isSecure(c, d)))
		return c.JSON(http.StatusOK, LoginResponse{
			Username:  data.Username,
			Typ:       data.Typ,
			CSRFToken: data.CSRFToken,
			SessionID: sessionID,
		})
	}
}

// logoutHandler implements POST /api/logout.
//
//	@Summary		Log out
//	@Description	Deletes the current sys_session and clears the session cookie. Succeeds with 204 even without a valid session.
//	@Tags			auth
//	@Success		204	"Logged out"
//	@Router			/logout [post]
//	@Security		CookieAuth
//	@Security		BearerAuth
func logoutHandler(d *Deps) echo.HandlerFunc {
	return func(c *echo.Context) error {
		if id := auth.SessionIDFromContext(c); id != "" {
			if err := d.Sessions.Delete(id); err != nil {
				return err
			}
		}
		c.SetCookie(auth.ClearCookie(isSecure(c, d)))
		return c.NoContent(http.StatusNoContent)
	}
}

// sessionHandler implements GET /api/session.
//
//	@Summary		Current session
//	@Description	Returns the logged-in user: username, access level, effective groups, language and the session CSRF token (so a reloaded SPA can resume without logging in again).
//	@Tags			auth
//	@Produce		json
//	@Success		200	{object}	SessionInfo
//	@Failure		401	{object}	ErrorResponse
//	@Router			/session [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
//
// splitModules splits the sys_user.modules CSV into a list ([] when empty).
func splitModules(csv string) []string {
	out := []string{}
	for _, m := range strings.Split(csv, ",") {
		if m = strings.TrimSpace(m); m != "" {
			out = append(out, m)
		}
	}
	return out
}

func sessionHandler() echo.HandlerFunc {
	return func(c *echo.Context) error {
		sess := auth.FromContext(c)
		return c.JSON(http.StatusOK, SessionInfo{
			Username:  sess.Username,
			Typ:       sess.Typ,
			Groups:    sess.Groups,
			Modules:   splitModules(sess.Modules),
			Language:  sess.Language,
			CSRFToken: sess.CSRFToken,
		})
	}
}

// requestSessionID extracts the session id the client presented (cookie or
// bearer header), regardless of validity — login uses it to regenerate any
// pre-existing session id against fixation.
func requestSessionID(c *echo.Context) string {
	if ck, err := c.Request().Cookie(auth.SessionCookieName); err == nil {
		return ck.Value
	}
	const prefix = "Bearer "
	if h := c.Request().Header.Get("Authorization"); len(h) > len(prefix) && h[:len(prefix)] == prefix {
		return h[len(prefix):]
	}
	return ""
}

// isSecure reports whether the session cookie must carry the Secure flag:
// TLS terminated by this process, configured certificates, or an
// https-forwarding trusted reverse proxy.
func isSecure(c *echo.Context, d *Deps) bool {
	if c.Request().TLS != nil || d.Config.Server.TLSCert != "" {
		return true
	}
	return d.fromTrustedProxy(c) &&
		strings.EqualFold(c.Request().Header.Get("X-Forwarded-Proto"), "https")
}

// clientIP resolves the client address used for login lockout. Requests
// arriving from a configured trusted proxy (server.trusted_proxies) use the
// rightmost non-proxy entry of X-Forwarded-For; everything else — including
// any forwarded header sent by an untrusted peer — uses the TCP peer.
func clientIP(c *echo.Context, d *Deps) string {
	peer := peerHost(c.Request().RemoteAddr)
	if !d.isTrustedProxy(peer) {
		return peer
	}
	fwd := strings.Split(c.Request().Header.Get("X-Forwarded-For"), ",")
	for i := len(fwd) - 1; i >= 0; i-- {
		hop := strings.TrimSpace(fwd[i])
		if hop != "" && !d.isTrustedProxy(hop) {
			return hop
		}
	}
	return peer
}

// fromTrustedProxy reports whether the TCP peer is a configured proxy.
func (d *Deps) fromTrustedProxy(c *echo.Context) bool {
	return d.isTrustedProxy(peerHost(c.Request().RemoteAddr))
}

// isTrustedProxy reports whether host falls inside one of the configured
// server.trusted_proxies CIDRs (parsed once in Register).
func (d *Deps) isTrustedProxy(host string) bool {
	if len(d.trustedProxies) == 0 {
		return false
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	for _, p := range d.trustedProxies {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// peerHost strips the port from an "ip:port" RemoteAddr; a bare host is
// returned as-is.
func peerHost(remoteAddr string) string {
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return host
	}
	return remoteAddr
}
