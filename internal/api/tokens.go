package api

// API token management (spec add-api-tokens): the System → Remote Users
// surface. Tokens are stored in remote_user (design D1); the secret is shown
// once at creation and only its SHA-256 digest is kept.

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v5"
	"gorm.io/gorm"

	"go-ispconfig/internal/apitoken"
	"go-ispconfig/internal/auth"
	"go-ispconfig/internal/model"
)

// registerTokenRoutes mounts /api/tokens (admin only, gated by the
// admin_allow_remote_users security policy) plus the exchange endpoint,
// which authenticates with a token rather than a session.
func registerTokenRoutes(g *echo.Group, d *Deps) {
	policy := auth.RequirePolicy(d.DB, "admin_allow_remote_users")
	g.GET("/tokens", tokenListHandler(d), requireAdmin, policy)
	g.POST("/tokens", tokenCreateHandler(d), requireAdmin, policy)
	g.PUT("/tokens/:id", tokenUpdateHandler(d), requireAdmin, policy)
	g.DELETE("/tokens/:id", tokenDeleteHandler(d), requireAdmin, policy)
	g.POST("/tokens/exchange", tokenExchangeHandler(d))
	g.GET("/tokens/scopes", tokenScopesHandler())
}

// TokenView is the safe representation of a token: everything an operator
// needs to manage it and nothing that could authenticate as it.
type TokenView struct {
	// ID is the token id, the public half of the credential.
	ID uint32 `json:"id" example:"3"`
	// Label names the token ("terraform-prod").
	Label string `json:"label" example:"terraform-prod"`
	// Owner is the sys_user login the token acts as.
	Owner string `json:"owner" example:"admin"`
	// OwnerID is that user's sys_user id.
	OwnerID uint32 `json:"owner_id" example:"1"`
	// Scopes are the granted `<resource>:<action>` strings.
	Scopes []string `json:"scopes"`
	// AllowedIPs is the CSV of IPs/CIDRs the token may be used from; empty
	// means any address.
	AllowedIPs string `json:"allowed_ips" example:"10.0.0.0/8"`
	// Enabled is false for a revoked token.
	Enabled bool `json:"enabled"`
	// ExpiresAt is RFC3339, empty when the token never expires.
	ExpiresAt string `json:"expires_at,omitempty"`
	// LastUsedAt is RFC3339, empty when the token has never authenticated.
	LastUsedAt string `json:"last_used_at,omitempty"`
}

// TokenCreateRequest is the POST /api/tokens body.
type TokenCreateRequest struct {
	// Label names the token. Required.
	Label string `json:"label" example:"terraform-prod"`
	// Owner is the sys_user login the token acts as. Defaults to the caller.
	Owner string `json:"owner" example:"admin"`
	// Scopes are the grants. At least one is required.
	Scopes []string `json:"scopes"`
	// AllowedIPs is an optional CSV of IPs/CIDRs.
	AllowedIPs string `json:"allowed_ips" example:"10.0.0.0/8"`
	// ExpiresAt is an optional RFC3339 expiry.
	ExpiresAt string `json:"expires_at" example:"2027-01-01T00:00:00Z"`
}

// TokenCreateResponse carries the plaintext credential. It is the only time
// the secret exists outside the caller's hands.
type TokenCreateResponse struct {
	// Token is the full credential to send as `Authorization: Bearer`.
	Token string `json:"token" example:"goisp_3_Vd8…"`
	// TokenView is the stored representation.
	TokenView
}

// TokenUpdateRequest is the PUT /api/tokens/{id} body. Every field is
// optional; the secret can never be changed — mint a new token instead.
type TokenUpdateRequest struct {
	Label      *string   `json:"label"`
	Scopes     *[]string `json:"scopes"`
	AllowedIPs *string   `json:"allowed_ips"`
	ExpiresAt  *string   `json:"expires_at"`
	Enabled    *bool     `json:"enabled"`
}

// TokenExchangeResponse is the POST /api/tokens/exchange body.
type TokenExchangeResponse struct {
	// AccessToken is the signed JWT.
	AccessToken string `json:"access_token"`
	// TokenType is always "Bearer".
	TokenType string `json:"token_type" example:"Bearer"`
	// ExpiresIn is the lifetime in seconds.
	ExpiresIn int `json:"expires_in" example:"900"`
	// Scopes are the grants the JWT carries, copied from the token.
	Scopes []string `json:"scopes"`
}

// tokenView builds the safe representation of a stored row.
func tokenView(row model.RemoteUser, ownerName string) TokenView {
	meta := apitoken.ParseMeta(row.RemoteFunctions)
	v := TokenView{
		ID: row.RemoteUserID, Label: row.RemoteUsername,
		Owner: ownerName, OwnerID: row.SysUserID,
		Scopes: meta.Scopes, AllowedIPs: row.RemoteIPs,
		Enabled: row.RemoteAccess == "y",
	}
	if !meta.Expires.IsZero() {
		v.ExpiresAt = meta.Expires.UTC().Format(time.RFC3339)
	}
	if !meta.LastUsed.IsZero() {
		v.LastUsedAt = meta.LastUsed.UTC().Format(time.RFC3339)
	}
	return v
}

// tokenListHandler implements GET /api/tokens.
//
//	@Summary		List API tokens
//	@Description	Every token with its label, owner, scopes, IP allow-list, expiry, last use and enabled state. The secret and its digest are never returned. Admin only, gated by the admin_allow_remote_users security policy. Scope: system:read.
//	@Tags			tokens
//	@Produce		json
//	@Success		200	{array}		TokenView
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse
//	@Router			/tokens [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func tokenListHandler(d *Deps) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var rows []model.RemoteUser
		db := d.DB.WithContext(c.Request().Context())
		if err := db.Order("remote_userid").Find(&rows).Error; err != nil {
			return err
		}
		names, err := ownerNames(db, rows)
		if err != nil {
			return err
		}
		out := make([]TokenView, 0, len(rows))
		for _, r := range rows {
			out = append(out, tokenView(r, names[r.SysUserID]))
		}
		return c.JSON(http.StatusOK, out)
	}
}

// tokenCreateHandler implements POST /api/tokens.
//
//	@Summary		Create an API token
//	@Description	Mints a token and returns the plaintext credential **once** — it is stored only as a SHA-256 digest and can never be retrieved again. At least one scope is required; scopes only ever narrow what the owner may already do. Admin only, gated by admin_allow_remote_users. Scope: system:write.
//	@Tags			tokens
//	@Accept			json
//	@Produce		json
//	@Param			body	body		TokenCreateRequest	true	"Token definition"
//	@Success		201		{object}	TokenCreateResponse
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Failure		422		{object}	ErrorResponse	"Per-field validation errors"
//	@Router			/tokens [post]
//	@Security		CookieAuth
//	@Security		BearerAuth
func tokenCreateHandler(d *Deps) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var body TokenCreateRequest
		if err := c.Bind(&body); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid body")
		}
		fields := map[string][]string{}
		if strings.TrimSpace(body.Label) == "" {
			fields["label"] = []string{"token_error_label_empty"}
		}
		if err := apitoken.ValidateScopes(body.Scopes); err != nil {
			fields["scopes"] = []string{"token_error_scopes_invalid"}
		}
		expires, err := parseOptionalTime(body.ExpiresAt)
		if err != nil {
			fields["expires_at"] = []string{"token_error_expiry_invalid"}
		}
		if err := validateIPList(body.AllowedIPs); err != nil {
			fields["allowed_ips"] = []string{"token_error_ips_invalid"}
		}
		if len(fields) > 0 {
			return &ValidationError{Fields: fields}
		}

		db := d.DB.WithContext(c.Request().Context())
		owner, err := resolveTokenOwner(c, db, body.Owner)
		if err != nil {
			return err
		}

		// The digest depends on the row id, which only exists after the
		// insert — so the row is created with a placeholder digest that can
		// never match a presented secret, then updated in the same
		// transaction with the real one.
		placeholder, err := randomHex()
		if err != nil {
			return err
		}
		meta := apitoken.Meta{Scopes: body.Scopes, Expires: expires}
		row := model.RemoteUser{
			SysUserID: owner.UserID, SysGroupID: owner.DefaultGroup,
			SysPermUser: "riud", SysPermGroup: "riud",
			RemoteUsername: strings.TrimSpace(body.Label),
			RemotePassword: placeholder,
			RemoteAccess:   "y",
			RemoteIPs:      strings.TrimSpace(body.AllowedIPs),
			// Scopes are stored even for the placeholder row so a crash
			// between insert and update leaves an unusable but well-formed
			// token rather than a scopeless one.
			RemoteFunctions: meta.String(),
		}

		var plaintext string
		err = db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
			pt, digest, err := apitoken.Mint(row.RemoteUserID)
			if err != nil {
				return err
			}
			plaintext = pt
			row.RemotePassword = digest
			return tx.Model(&model.RemoteUser{}).
				Where("remote_userid = ?", row.RemoteUserID).
				Update("remote_password", digest).Error
		})
		if err != nil {
			return err
		}
		return c.JSON(http.StatusCreated, TokenCreateResponse{
			Token: plaintext, TokenView: tokenView(row, owner.Username),
		})
	}
}

// tokenUpdateHandler implements PUT /api/tokens/{id}.
//
//	@Summary		Update, revoke or re-enable an API token
//	@Description	Changes the label, scopes, IP allow-list, expiry or enabled state. The secret can never be changed — mint a new token instead. Revoking takes effect on the next request. Admin only, gated by admin_allow_remote_users. Scope: system:write.
//	@Tags			tokens
//	@Accept			json
//	@Produce		json
//	@Param			id		path		int					true	"Token id"
//	@Param			body	body		TokenUpdateRequest	true	"Fields to change"
//	@Success		200		{object}	TokenView
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Failure		404		{object}	ErrorResponse
//	@Failure		422		{object}	ErrorResponse
//	@Router			/tokens/{id} [put]
//	@Security		CookieAuth
//	@Security		BearerAuth
func tokenUpdateHandler(d *Deps) echo.HandlerFunc {
	return func(c *echo.Context) error {
		id, err := strconv.ParseUint(c.Param("id"), 10, 32)
		if err != nil || id == 0 {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid token id")
		}
		var body TokenUpdateRequest
		if err := c.Bind(&body); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid body")
		}

		db := d.DB.WithContext(c.Request().Context())
		var row model.RemoteUser
		if err := db.Where("remote_userid = ?", id).Take(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return echo.NewHTTPError(http.StatusNotFound, "token not found")
			}
			return err
		}

		meta := apitoken.ParseMeta(row.RemoteFunctions)
		fields := map[string][]string{}
		if body.Label != nil {
			if strings.TrimSpace(*body.Label) == "" {
				fields["label"] = []string{"token_error_label_empty"}
			} else {
				row.RemoteUsername = strings.TrimSpace(*body.Label)
			}
		}
		if body.Scopes != nil {
			if err := apitoken.ValidateScopes(*body.Scopes); err != nil {
				fields["scopes"] = []string{"token_error_scopes_invalid"}
			} else {
				meta.Scopes = *body.Scopes
			}
		}
		if body.AllowedIPs != nil {
			if err := validateIPList(*body.AllowedIPs); err != nil {
				fields["allowed_ips"] = []string{"token_error_ips_invalid"}
			} else {
				row.RemoteIPs = strings.TrimSpace(*body.AllowedIPs)
			}
		}
		if body.ExpiresAt != nil {
			expires, err := parseOptionalTime(*body.ExpiresAt)
			if err != nil {
				fields["expires_at"] = []string{"token_error_expiry_invalid"}
			} else {
				meta.Expires = expires
			}
		}
		if body.Enabled != nil {
			row.RemoteAccess = "n"
			if *body.Enabled {
				row.RemoteAccess = "y"
			}
		}
		if len(fields) > 0 {
			return &ValidationError{Fields: fields}
		}

		row.RemoteFunctions = meta.String()
		err = db.Model(&model.RemoteUser{}).Where("remote_userid = ?", row.RemoteUserID).
			Updates(map[string]any{
				"remote_username":  row.RemoteUsername,
				"remote_access":    row.RemoteAccess,
				"remote_ips":       row.RemoteIPs,
				"remote_functions": row.RemoteFunctions,
			}).Error
		if err != nil {
			return err
		}
		names, err := ownerNames(db, []model.RemoteUser{row})
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, tokenView(row, names[row.SysUserID]))
	}
}

// tokenDeleteHandler implements DELETE /api/tokens/{id}.
//
//	@Summary		Delete an API token
//	@Description	Removes the token irreversibly. Prefer revoking (PUT with enabled=false) when the credential may still be in use somewhere. Admin only, gated by admin_allow_remote_users. Scope: system:write.
//	@Tags			tokens
//	@Param			id	path	int	true	"Token id"
//	@Success		204	"Deleted"
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse
//	@Router			/tokens/{id} [delete]
//	@Security		CookieAuth
//	@Security		BearerAuth
func tokenDeleteHandler(d *Deps) echo.HandlerFunc {
	return func(c *echo.Context) error {
		id, err := strconv.ParseUint(c.Param("id"), 10, 32)
		if err != nil || id == 0 {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid token id")
		}
		db := d.DB.WithContext(c.Request().Context())
		if err := db.Where("remote_userid = ?", id).Delete(&model.RemoteUser{}).Error; err != nil {
			return err
		}
		// Outstanding JWTs of a deleted token still expire on their own; drop
		// their bookkeeping rows now rather than leaving them for the sweep.
		_ = db.Where("remote_userid = ?", id).Delete(&model.RemoteSession{}).Error
		return c.NoContent(http.StatusNoContent)
	}
}

// tokenScopesHandler implements GET /api/tokens/scopes.
//
//	@Summary		List the scopes a token may be granted
//	@Description	The resource groups and actions of the scope grammar, for the token form. Scope: system:read.
//	@Tags			tokens
//	@Produce		json
//	@Success		200	{array}		string
//	@Failure		401	{object}	ErrorResponse
//	@Router			/tokens/scopes [get]
//	@Security		CookieAuth
//	@Security		BearerAuth
func tokenScopesHandler() echo.HandlerFunc {
	return func(c *echo.Context) error {
		out := make([]string, 0, len(apitoken.Resources)*2+1)
		for _, r := range apitoken.Resources {
			out = append(out, r+":"+apitoken.ActionRead, r+":"+apitoken.ActionWrite)
		}
		return c.JSON(http.StatusOK, out)
	}
}

// tokenExchangeHandler implements POST /api/tokens/exchange.
//
//	@Summary		Exchange an API token for a short-lived JWT
//	@Description	Authenticated by an API token (not by a session): returns an HS256 JWT carrying the same owner and scopes, expiring after [auth] jwt_ttl (default 15 minutes, hard cap 1 hour, never beyond the token's own expiry). A JWT issued before the token was revoked stays valid until it expires.
//	@Tags			tokens
//	@Produce		json
//	@Success		200	{object}	TokenExchangeResponse
//	@Failure		401	{object}	ErrorResponse	"Not authenticated by an API token, or the token is revoked or expired"
//	@Failure		503	{object}	ErrorResponse	"No [auth] jwt_secret configured"
//	@Router			/tokens/exchange [post]
//	@Security		BearerAuth
func tokenExchangeHandler(d *Deps) echo.HandlerFunc {
	return func(c *echo.Context) error {
		cred := auth.CredentialFromContext(c)
		sess := auth.FromContext(c)
		if cred == nil || sess == nil || cred.JWT {
			// A session cannot mint a machine credential, and a JWT cannot
			// extend its own life by exchanging itself.
			return echo.NewHTTPError(http.StatusUnauthorized, "an API token is required")
		}
		if d.Config.Auth.JWTSecret == "" {
			return echo.NewHTTPError(http.StatusServiceUnavailable,
				"no [auth] jwt_secret configured: set one in config.toml and restart")
		}

		now := time.Now()
		ttl := apitoken.ClampTTL(d.Config.Auth.JWTTTL)
		exp := now.Add(ttl)

		db := d.DB.WithContext(c.Request().Context())
		var row model.RemoteUser
		if err := db.Where("remote_userid = ?", cred.TokenID).Take(&row).Error; err != nil {
			return echo.NewHTTPError(http.StatusUnauthorized, "an API token is required")
		}
		if meta := apitoken.ParseMeta(row.RemoteFunctions); !meta.Expires.IsZero() && meta.Expires.Before(exp) {
			// A JWT must never outlive the token that issued it.
			exp = meta.Expires
		}

		jti, err := randomHex()
		if err != nil {
			return err
		}
		signed, err := apitoken.SignJWT(d.Config.Auth.JWTSecret, apitoken.Claims{
			Sub: sess.UserID, TID: cred.TokenID, Scope: cred.Scopes,
			JTI: jti, Exp: exp.Unix(), Iat: now.Unix(),
		})
		if err != nil {
			return err
		}
		// Bookkeeping only — verification never reads it (design D5).
		_ = db.Create(&model.RemoteSession{
			RemoteSession: jti, RemoteUserID: cred.TokenID,
			RemoteFunctions: strings.Join(cred.Scopes, ","),
			Tstamp:          uint32(exp.Unix()), //nolint:gosec // unix seconds, positive and far from overflow
			RemoteIP:        clientIP(c, d),
		}).Error

		return c.JSON(http.StatusOK, TokenExchangeResponse{
			AccessToken: signed, TokenType: "Bearer",
			ExpiresIn: int(time.Until(exp).Seconds()), Scopes: cred.Scopes,
		})
	}
}

// resolveTokenOwner returns the sys_user a new token acts as: the named one,
// or the caller when no owner is given.
func resolveTokenOwner(c *echo.Context, db *gorm.DB, name string) (*model.SysUser, error) {
	sess := auth.FromContext(c)
	if strings.TrimSpace(name) == "" {
		if sess == nil {
			return nil, echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
		}
		name = sess.Username
	}
	var owner model.SysUser
	if err := db.Where("username = ?", name).Take(&owner).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, &ValidationError{Fields: map[string][]string{
				"owner": {"token_error_owner_unknown"},
			}}
		}
		return nil, err
	}
	if owner.Active != 1 {
		return nil, &ValidationError{Fields: map[string][]string{
			"owner": {"token_error_owner_inactive"},
		}}
	}
	return &owner, nil
}

// ownerNames resolves the sys_user login of every token owner in one query.
func ownerNames(db *gorm.DB, rows []model.RemoteUser) (map[uint32]string, error) {
	ids := make([]uint32, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.SysUserID)
	}
	out := map[uint32]string{}
	if len(ids) == 0 {
		return out, nil
	}
	var users []model.SysUser
	if err := db.Where("userid IN ?", ids).Find(&users).Error; err != nil {
		return nil, err
	}
	for _, u := range users {
		out[u.UserID] = u.Username
	}
	return out, nil
}

// parseOptionalTime accepts an empty string as "no expiry".
func parseOptionalTime(s string) (time.Time, error) {
	if strings.TrimSpace(s) == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, strings.TrimSpace(s))
}

// validateIPList rejects an allow-list the matcher could never satisfy — an
// entry that parses as neither an address nor a CIDR would silently lock the
// token out of every address.
func validateIPList(list string) error {
	if strings.TrimSpace(list) == "" {
		return nil
	}
	for entry := range strings.SplitSeq(list, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if err := apitoken.ValidIPEntry(entry); err != nil {
			return err
		}
	}
	return nil
}

// randomHex returns 32 bytes of entropy hex-encoded, used for the placeholder
// digest and for JWT ids.
func randomHex() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
