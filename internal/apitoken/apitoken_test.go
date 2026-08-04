package apitoken

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMintParseVerify(t *testing.T) {
	plaintext, digest, err := Mint(7)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(plaintext, Prefix+"7_"))

	id, secret, err := Parse(plaintext)
	require.NoError(t, err)
	assert.Equal(t, uint32(7), id)
	assert.True(t, VerifyDigest(digest, secret))
	assert.False(t, VerifyDigest(digest, secret+"x"))

	// Two mints of the same id must never collide.
	other, _, err := Mint(7)
	require.NoError(t, err)
	assert.NotEqual(t, plaintext, other)
}

func TestParseRejectsMalformed(t *testing.T) {
	for _, in := range []string{
		"", "goisp_", "goisp_abc_secret", "goisp_0_secret", "goisp_7_",
		"7_secret", "Bearer goisp_7_secret", "goisp_7",
	} {
		_, _, err := Parse(in)
		assert.ErrorIs(t, err, ErrMalformed, "input %q", in)
	}
}

func TestMetaRoundTrip(t *testing.T) {
	exp := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	used := time.Date(2026, 8, 4, 9, 41, 12, 0, time.UTC)
	m := Meta{Scopes: []string{"sites:read", "mail:*"}, Expires: exp, LastUsed: used}

	back := ParseMeta(m.String())
	assert.Equal(t, m.Scopes, back.Scopes)
	assert.True(t, exp.Equal(back.Expires))
	assert.True(t, used.Equal(back.LastUsed))
}

// TestMetaAcceptsLegacyCSV is the compatibility guarantee of design D2: a
// remote_user row written by the PHP panel still parses.
func TestMetaAcceptsLegacyCSV(t *testing.T) {
	m := ParseMeta("sites_web_domain_get,mail_domain_add")
	assert.Equal(t, []string{"sites_web_domain_get", "mail_domain_add"}, m.Scopes)
	assert.True(t, m.Expires.IsZero())
}

func TestMetaEmptyAndGarbage(t *testing.T) {
	assert.Empty(t, ParseMeta("").Scopes)
	// A value with keys but no scopes grants nothing, which denies every
	// request rather than granting all of them.
	assert.Empty(t, ParseMeta("expires=2027-01-01T00:00:00Z").Scopes)
	// A corrupted timestamp reads as "no expiry recorded", never as expired.
	assert.True(t, ParseMeta("scopes=sites:read;expires=not-a-date").Expires.IsZero())
}

func TestMetaExpired(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	assert.False(t, Meta{}.Expired(now), "no expiry never expires")
	assert.True(t, Meta{Expires: now.Add(-time.Second)}.Expired(now))
	assert.False(t, Meta{Expires: now.Add(time.Second)}.Expired(now))
}

func TestValidateScopes(t *testing.T) {
	require.NoError(t, ValidateScopes([]string{"sites:read", "*:write", "dns:*"}))
	assert.Error(t, ValidateScopes(nil), "empty scope list must be refused")
	assert.Error(t, ValidateScopes([]string{"sites:delete"}))
	assert.Error(t, ValidateScopes([]string{"nosuch:read"}))
	assert.Error(t, ValidateScopes([]string{"sites"}))
}

func TestAllows(t *testing.T) {
	tests := []struct {
		scopes   []string
		resource string
		action   string
		want     bool
	}{
		{[]string{"sites:read"}, "sites", ActionRead, true},
		{[]string{"sites:read"}, "sites", ActionWrite, false},
		{[]string{"sites:write"}, "sites", ActionRead, true},
		{[]string{"sites:write"}, "sites", ActionWrite, true},
		{[]string{"sites:*"}, "sites", ActionWrite, true},
		{[]string{"*:read"}, "mail", ActionRead, true},
		{[]string{"*:read"}, "mail", ActionWrite, false},
		{[]string{"*:*"}, "system", ActionWrite, true},
		{[]string{"sites:read"}, "mail", ActionRead, false},
		{nil, "sites", ActionRead, false},
	}
	for _, tc := range tests {
		got := Allows(tc.scopes, tc.resource, tc.action)
		assert.Equal(t, tc.want, got, "%v on %s:%s", tc.scopes, tc.resource, tc.action)
	}
}

func TestActionFor(t *testing.T) {
	assert.Equal(t, ActionRead, ActionFor("GET"))
	assert.Equal(t, ActionWrite, ActionFor("POST"))
	assert.Equal(t, ActionWrite, ActionFor("PUT"))
	assert.Equal(t, ActionWrite, ActionFor("DELETE"))
}

// TestResourceForPath pins the route grouping: an endpoint that stops
// resolving to its resource would silently become unscoped.
func TestResourceForPath(t *testing.T) {
	tests := map[string]string{
		"/api/sites/web-domains":      "sites",
		"/api/mail/domains/3":         "mail",
		"/api/dns/zones":              "dns",
		"/api/clients":                "clients",
		"/api/resellers":              "clients",
		"/api/monitor/state":          "monitor",
		"/api/server":                 "server",
		"/api/servers/1/config/web":   "server",
		"/api/server_ip":              "server",
		"/api/firewall":               "server",
		"/api/cp-users":               "system",
		"/api/tokens":                 "system",
		"/api/tokens/exchange":        "",
		"/api/login":                  "",
		"/api/meta/forms/web-domains": "",
	}
	for path, want := range tests {
		assert.Equal(t, want, ResourceForPath(path), "path %s", path)
	}
}

func TestIPAllowed(t *testing.T) {
	assert.True(t, IPAllowed("", "203.0.113.5"), "empty list allows any address")
	assert.True(t, IPAllowed("10.0.0.0/8", "10.1.2.3"))
	assert.False(t, IPAllowed("10.0.0.0/8", "203.0.113.5"))
	assert.True(t, IPAllowed("203.0.113.5, 10.0.0.0/8", "203.0.113.5"))
	assert.False(t, IPAllowed("10.0.0.0/8", ""), "unknown caller with a restricted list is refused")
	assert.True(t, IPAllowed("fd00::/8", "fd00::1"))
}

func TestValidIPEntry(t *testing.T) {
	require.NoError(t, ValidIPEntry("10.0.0.1"))
	require.NoError(t, ValidIPEntry("10.0.0.0/8"))
	require.NoError(t, ValidIPEntry("fd00::1"))
	assert.Error(t, ValidIPEntry("not-an-ip"))
	assert.Error(t, ValidIPEntry("10.0.0.0/99"))
}

func TestJWTRoundTrip(t *testing.T) {
	now := time.Now()
	claims := Claims{
		Sub: 1, TID: 3, Scope: []string{"sites:read"},
		JTI: "abc", Exp: now.Add(time.Minute).Unix(), Iat: now.Unix(),
	}
	signed, err := SignJWT("s3cret", claims)
	require.NoError(t, err)
	require.True(t, LooksJWT(signed))

	back, err := ParseJWT("s3cret", signed, now)
	require.NoError(t, err)
	assert.Equal(t, claims.Sub, back.Sub)
	assert.Equal(t, claims.TID, back.TID)
	assert.Equal(t, claims.Scope, back.Scope)
}

func TestJWTRejections(t *testing.T) {
	now := time.Now()
	valid := Claims{Sub: 1, TID: 3, Scope: []string{"sites:read"}, JTI: "a", Exp: now.Add(time.Minute).Unix()}
	signed, err := SignJWT("s3cret", valid)
	require.NoError(t, err)

	t.Run("wrong secret", func(t *testing.T) {
		_, err := ParseJWT("other", signed, now)
		assert.ErrorIs(t, err, ErrInvalidJWT)
	})
	t.Run("expired", func(t *testing.T) {
		_, err := ParseJWT("s3cret", signed, now.Add(2*time.Minute))
		assert.ErrorIs(t, err, ErrInvalidJWT)
	})
	t.Run("tampered payload", func(t *testing.T) {
		parts := strings.Split(signed, ".")
		bad := parts[0] + "." + parts[1][:len(parts[1])-2] + "XY." + parts[2]
		_, err := ParseJWT("s3cret", bad, now)
		assert.ErrorIs(t, err, ErrInvalidJWT)
	})
	// alg confusion: a token declaring "none" must not even reach the
	// signature check, because the header is compared, not parsed.
	t.Run("alg none", func(t *testing.T) {
		none := "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0.eyJzdWIiOjF9."
		_, err := ParseJWT("s3cret", none, now)
		assert.ErrorIs(t, err, ErrInvalidJWT)
		assert.False(t, LooksJWT(none))
	})
	t.Run("no secret configured", func(t *testing.T) {
		_, err := ParseJWT("", signed, now)
		assert.ErrorIs(t, err, ErrInvalidJWT)
		_, err = SignJWT("", valid)
		assert.Error(t, err)
	})
	t.Run("empty scope claim", func(t *testing.T) {
		s, err := SignJWT("s3cret", Claims{Sub: 1, TID: 3, Exp: now.Add(time.Minute).Unix()})
		require.NoError(t, err)
		_, err = ParseJWT("s3cret", s, now)
		assert.ErrorIs(t, err, ErrInvalidJWT)
	})
}

func TestClampTTL(t *testing.T) {
	assert.Equal(t, DefaultJWTTTL, ClampTTL(0))
	assert.Equal(t, DefaultJWTTTL, ClampTTL(-time.Hour))
	assert.Equal(t, MaxJWTTTL, ClampTTL(24*time.Hour))
	assert.Equal(t, 5*time.Minute, ClampTTL(5*time.Minute))
}

func TestLooks(t *testing.T) {
	assert.True(t, Looks("goisp_1_abc"))
	assert.False(t, Looks("abc"))
}
