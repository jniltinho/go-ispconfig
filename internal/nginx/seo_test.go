package nginx

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSeoRedirects pins the get_seo_redirects() port for every variant.
func TestSeoRedirects(t *testing.T) {
	web := func(sub, seo string) row {
		return row{"domain": "example.com", "subdomain": sub, "seo_redirect": seo}
	}

	got := seoRedirects(web("www", "non_www_to_www"), "", "")
	assert.Equal(t, map[string]any{
		"seo_redirect_origin_domain": "example.com",
		"seo_redirect_target_domain": "www.example.com",
		"seo_redirect_operator":      "=",
	}, got)

	got = seoRedirects(web("none", "www_to_non_www"), "alias_", "")
	assert.Equal(t, "www.example.com", got["alias_seo_redirect_origin_domain"])
	assert.Equal(t, "example.com", got["alias_seo_redirect_target_domain"])
	assert.Equal(t, "=", got["alias_seo_redirect_operator"])

	got = seoRedirects(web("*", "*_domain_tld_to_www_domain_tld"), "", "")
	assert.Equal(t, `^(example\.com|((?:\w+(?:-\w+)*\.)*)((?!www\.)\w+(?:-\w+)*)(\.example\.com))$`,
		got["seo_redirect_origin_domain"])
	assert.Equal(t, "~*", got["seo_redirect_operator"])

	got = seoRedirects(web("none", "*_domain_tld_to_domain_tld"), "", "")
	assert.Equal(t, `^(.+)\.example\.com$`, got["seo_redirect_origin_domain"])

	got = seoRedirects(web("www", "*_to_www_domain_tld"), "", "")
	assert.Equal(t, "!=", got["seo_redirect_operator"])

	// forceSubdomain=none suppresses the non-www variants.
	got = seoRedirects(web("none", "www_to_non_www"), "", "none")
	assert.Empty(t, got)

	// A *.wildcard domain behaves as subdomain '*'.
	w := web("none", "non_www_to_www")
	w["domain"] = "*.example.com"
	got = seoRedirects(w, "", "")
	assert.Equal(t, "www.*.example.com", got["seo_redirect_target_domain"])
}

// TestValidRewriteRules pins the all-or-nothing whitelist port.
func TestValidRewriteRules(t *testing.T) {
	ok := validRewriteRules("rewrite ^/old$ /new permanent;\n# comment\n\nreturn 301 https://example.com/x;")
	assert.Len(t, ok, 4)

	// if-block with balanced braces.
	ok = validRewriteRules("if ($http_user_agent ~* bot) {\nreturn 403;\n}")
	assert.Len(t, ok, 3)

	// Unbalanced braces drop everything.
	assert.Nil(t, validRewriteRules("if ($host = x) {\nreturn 403;"))
	// A non-whitelisted line drops everything.
	assert.Nil(t, validRewriteRules("rewrite ^/a /b;\nproxy_pass http://evil;"))
	assert.Nil(t, validRewriteRules(""))
}
