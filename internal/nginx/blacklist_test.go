package nginx

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/mastertpl"
)

func TestBlacklistCompiles(t *testing.T) {
	require.NotEmpty(t, blacklist, "embedded blacklist must contain at least one pattern")
}

func TestFilterBlacklistedDirectives(t *testing.T) {
	kept, rejected := filterBlacklistedDirectives(
		"client_max_body_size 100M;\n" +
			"load_module /tmp/evil.so;\n" +
			"  LOAD_MODULE /tmp/evil2.so;\n" + // case-insensitive, leading space
			"fastcgi_read_timeout 300;")
	assert.Equal(t, "client_max_body_size 100M;\nfastcgi_read_timeout 300;", kept)
	require.Len(t, rejected, 2)
	assert.ErrorContains(t, rejected[0], "load_module /tmp/evil.so;")

	// load_module as part of another word is not a directive and passes.
	kept, rejected = filterBlacklistedDirectives("# comment about load_module usage")
	assert.Empty(t, rejected)
	assert.Equal(t, "# comment about load_module usage", kept)

	kept, rejected = filterBlacklistedDirectives("")
	assert.Empty(t, rejected)
	assert.Empty(t, kept)
}

func TestBlockedDirectives(t *testing.T) {
	assert.Empty(t, BlockedDirectives(""))
	assert.Empty(t, BlockedDirectives("gzip on;\nfastcgi_read_timeout 300;"))
	blocked := BlockedDirectives("gzip on;\n  load_module /tmp/evil.so;")
	require.Equal(t, []string{"load_module /tmp/evil.so;"}, blocked)
}

// TestEmbeddedWebTemplatesParse pins task 1.2: the vhost and pool templates
// shipped in mastertpl must parse and render without error (detailed output
// is pinned by the mastertpl golden tests).
func TestEmbeddedWebTemplatesParse(t *testing.T) {
	for _, name := range []string{"nginx_vhost.conf.master", "php_fpm_pool.conf.master"} {
		src, source, err := mastertpl.Load(name, "")
		require.NoError(t, err)
		assert.Equal(t, mastertpl.SourceEmbedded, source)
		_, err = mastertpl.New(src).Render()
		require.NoErrorf(t, err, "template %s must parse", name)
	}
}
