package apitoken

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// A deliberately minimal HS256 implementation instead of a JWT library
// (design D5): the panel is both the only issuer and the only verifier, the
// algorithm is fixed, and the header is never trusted to choose it. That
// closes the alg-confusion class of bug by construction — a token claiming
// `"alg":"none"` or RS256 is rejected before any signature work — and costs
// less code than wiring and pinning a dependency.

// DefaultJWTTTL is the lifetime applied when none is configured.
const DefaultJWTTTL = 15 * time.Minute

// MaxJWTTTL caps whatever is configured. A stateless credential is only safe
// because it expires soon; the cap is what makes "revoked tokens stop working
// within a bounded window" true regardless of configuration.
const MaxJWTTTL = time.Hour

// ErrInvalidJWT is returned for any malformed, mis-signed or expired JWT.
var ErrInvalidJWT = errors.New("apitoken: invalid jwt")

// Claims is the claim set of an issued JWT.
type Claims struct {
	// Sub is the owner sys_user id the request executes as.
	Sub uint32 `json:"sub"`
	// TID is the token that issued this JWT.
	TID uint32 `json:"tid"`
	// Scope carries the issuing token's scopes verbatim.
	Scope []string `json:"scope"`
	// JTI identifies this JWT in remote_session.
	JTI string `json:"jti"`
	// Exp is the expiry, seconds since the epoch.
	Exp int64 `json:"exp"`
	// Iat is the issue time, seconds since the epoch.
	Iat int64 `json:"iat"`
}

// jwtHeader is the only header this package ever emits or accepts.
var jwtHeader = base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))

// ClampTTL applies the default and the hard cap.
func ClampTTL(ttl time.Duration) time.Duration {
	switch {
	case ttl <= 0:
		return DefaultJWTTTL
	case ttl > MaxJWTTTL:
		return MaxJWTTTL
	default:
		return ttl
	}
}

// SignJWT returns the compact serialization of the claims.
func SignJWT(secret string, c Claims) (string, error) {
	if secret == "" {
		return "", errors.New("apitoken: no jwt secret configured")
	}
	payload, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	signing := jwtHeader + "." + base64.RawURLEncoding.EncodeToString(payload)
	return signing + "." + sign(secret, signing), nil
}

// ParseJWT verifies the signature and expiry and returns the claims. The
// header is compared against the only one we issue rather than parsed, so no
// value inside the token can influence how it is verified.
func ParseJWT(secret, token string, now time.Time) (*Claims, error) {
	if secret == "" {
		return nil, ErrInvalidJWT
	}
	header, rest, ok := strings.Cut(token, ".")
	if !ok || header != jwtHeader {
		return nil, ErrInvalidJWT
	}
	payload, signature, ok := strings.Cut(rest, ".")
	if !ok {
		return nil, ErrInvalidJWT
	}
	if !hmac.Equal([]byte(signature), []byte(sign(secret, header+"."+payload))) {
		return nil, ErrInvalidJWT
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return nil, ErrInvalidJWT
	}
	var c Claims
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, ErrInvalidJWT
	}
	if c.Exp <= 0 || now.Unix() >= c.Exp {
		return nil, ErrInvalidJWT
	}
	if c.Sub == 0 || c.TID == 0 || len(c.Scope) == 0 {
		return nil, ErrInvalidJWT
	}
	return &c, nil
}

// LooksJWT reports whether a bearer value is shaped like a compact JWT this
// package issued: three segments and our exact header.
func LooksJWT(bearer string) bool {
	return strings.HasPrefix(bearer, jwtHeader+".") && strings.Count(bearer, ".") == 2
}

func sign(secret, signing string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signing))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
