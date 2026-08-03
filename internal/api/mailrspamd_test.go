package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateScores(t *testing.T) {
	require.NoError(t, validateScores(6, 15, map[string]float64{"greylisting_level": 4}))

	err := validateScores(20, 15, nil)
	var ve *ValidationError
	require.ErrorAs(t, err, &ve)
	assert.Contains(t, ve.Fields, "spam_tag_level")

	err = validateScores(6, 500, nil)
	require.ErrorAs(t, err, &ve)
	assert.Contains(t, ve.Fields, "spam_kill_level")

	err = validateScores(6, 15, map[string]float64{"greylisting_level": -1})
	require.ErrorAs(t, err, &ve)
	assert.Contains(t, ve.Fields, "greylisting_level")
}

func TestNormalizeList(t *testing.T) {
	out, err := normalizeList("whitelist", []string{
		" Good@Example.com ", "@example.org", "", "good@example.com",
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"good@example.com", "example.org"}, out)

	_, err = normalizeList("blacklist", []string{"not a host"})
	var ve *ValidationError
	require.ErrorAs(t, err, &ve)
	assert.Contains(t, ve.Fields, "blacklist")

	// A comma-separated blob is a common paste; it must not become one row.
	_, err = normalizeList("blacklist", []string{"a@b.com,c@d.com"})
	require.ErrorAs(t, err, &ve)
}

func TestParseScoreAndFormat(t *testing.T) {
	assert.Equal(t, 6.0, parseScore("", 6))
	assert.Equal(t, 6.0, parseScore("nonsense", 6))
	assert.Equal(t, 7.5, parseScore(" 7.5 ", 6))
	assert.Equal(t, "7.5", formatScore(7.5))
	assert.Equal(t, "15", formatScore(15))
}

func TestRspamdServerID(t *testing.T) {
	newCtx := func(query string) *echo.Context {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/?"+query, nil)
		return e.NewContext(req, httptest.NewRecorder())
	}
	id, err := rspamdServerID(newCtx(""))
	require.NoError(t, err)
	assert.Equal(t, uint32(1), id)

	id, err = rspamdServerID(newCtx("server_id=3"))
	require.NoError(t, err)
	assert.Equal(t, uint32(3), id)

	_, err = rspamdServerID(newCtx("server_id=0"))
	require.Error(t, err)
	_, err = rspamdServerID(newCtx("server_id=x"))
	require.Error(t, err)
}

func TestDomainPolicyName(t *testing.T) {
	assert.Equal(t, "rspamd_domain_example.com", domainPolicyName("example.com"))
}
