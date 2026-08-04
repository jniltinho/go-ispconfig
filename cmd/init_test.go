package cmd

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var generatedSecretRe = regexp.MustCompile(`(?m)^jwt_secret = "([0-9a-f]{64})"$`)

// TestWithGeneratedJWTSecret pins the reason init does not copy the example
// verbatim: the embedded template ships an empty key so the repository never
// contains one, and every generated config must get its own.
func TestWithGeneratedJWTSecret(t *testing.T) {
	template := []byte("[auth]\nrehash_legacy = false\njwt_secret = \"\"\njwt_ttl = \"15m\"\n")

	out, err := withGeneratedJWTSecret(template)
	require.NoError(t, err)

	m := generatedSecretRe.FindSubmatch(out)
	require.Len(t, m, 2, "no 32-byte hex key was written:\n%s", out)
	assert.Contains(t, string(out), `jwt_ttl = "15m"`, "the rest of the file must survive")
	assert.Contains(t, string(out), "rehash_legacy = false")

	// Two runs must not produce the same key.
	other, err := withGeneratedJWTSecret(template)
	require.NoError(t, err)
	assert.NotEqual(t, string(out), string(other))
}

// A template without the placeholder is returned untouched: a renamed key
// must degrade to "exchange stays disabled", never to a corrupted config.
func TestWithGeneratedJWTSecretWithoutPlaceholder(t *testing.T) {
	template := []byte("[server]\nport = 8080\n")
	out, err := withGeneratedJWTSecret(template)
	require.NoError(t, err)
	assert.Equal(t, template, out)
}
