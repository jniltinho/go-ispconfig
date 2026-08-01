package api

import (
	"errors"
	"net/http"

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
	// StayLoggedIn is accepted for SPA compatibility; sessions currently
	// always use the server-side idle TTL.
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
}

// SessionInfo is the GET /api/session body describing the logged-in user.
type SessionInfo struct {
	// Username is the sys_user login name.
	Username string `json:"username" example:"admin"`
	// Typ is the access level ("admin" or "user").
	Typ string `json:"typ" example:"admin"`
	// Groups is the effective sys_group id list of the user.
	Groups []uint32 `json:"groups"`
	// Language is the panel language of the user.
	Language string `json:"language" example:"en"`
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
//	@Description	Verifies sys_user credentials (bcrypt or legacy ISPConfig3 crypt hashes) with brute-force lockout, creates a sys_session and returns the CSRF token. The session id is set as an HTTP-only cookie; non-browser clients may present it as a bearer token instead.
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
		remote := c.Request().RemoteAddr

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
//	@Description	Returns the logged-in user: username, access level, effective groups and language.
//	@Tags			auth
//	@Produce		json
//	@Success		200	{object}	SessionInfo
//	@Failure		401	{object}	ErrorResponse
//	@Router			/session [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func sessionHandler() echo.HandlerFunc {
	return func(c *echo.Context) error {
		sess := auth.FromContext(c)
		return c.JSON(http.StatusOK, SessionInfo{
			Username: sess.Username,
			Typ:      sess.Typ,
			Groups:   sess.Groups,
			Language: sess.Language,
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

// isSecure reports whether the session cookie must carry the Secure flag
// (TLS termination by this process or configured certificates).
func isSecure(c *echo.Context, d *Deps) bool {
	return c.Request().TLS != nil || d.Config.Server.TLSCert != ""
}
