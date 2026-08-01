package nginx

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeLocationsReplace(t *testing.T) {
	conf := `server {
        location / {
            try_files $uri $uri/;
        }
        location / {
            try_files $uri /index.php?$args;
        }
}`
	out, warnings := mergeLocations(conf)
	assert.Empty(t, warnings)
	// The later block replaces the earlier one at the earlier position.
	assert.Equal(t, 1, strings.Count(out, "location / {"))
	assert.Contains(t, out, "try_files $uri /index.php?$args;")
	assert.NotContains(t, out, "try_files $uri $uri/;")
}

func TestMergeLocationsMerge(t *testing.T) {
	conf := `server {
        location /admin/ {
            auth_basic "Members Only";
        }
        location /admin/ { ##merge##
            deny 10.0.0.0/8;
        }
}`
	out, warnings := mergeLocations(conf)
	assert.Empty(t, warnings)
	assert.Equal(t, 1, strings.Count(out, "location /admin/ {"))
	assert.Contains(t, out, `auth_basic "Members Only";`)
	assert.Contains(t, out, "deny 10.0.0.0/8;")
	assert.NotContains(t, out, "##merge##")
}

func TestMergeLocationsDelete(t *testing.T) {
	conf := `server {
        location /stats/ {
            auth_basic "Members Only";
        }
        location /stats/ { ##delete##
            x;
        }
}`
	out, _ := mergeLocations(conf)
	assert.NotContains(t, out, "location /stats/")
	assert.NotContains(t, out, "auth_basic")
}

func TestMergeLocationsOneLineAndComments(t *testing.T) {
	conf := `server {
        # a comment line disappears
        location ~ \.php$ { try_files /dummy.htm @php; }
        location ~ \.php$ { ##merge## fastcgi_read_timeout 300; }
}`
	out, _ := mergeLocations(conf)
	assert.NotContains(t, out, "# a comment line")
	assert.Equal(t, 1, strings.Count(out, `location ~ \.php$ {`))
	assert.Contains(t, out, "try_files /dummy.htm @php;")
	assert.Contains(t, out, "fastcgi_read_timeout 300;")
}

func TestMergeLocationsNestedAndSecondServer(t *testing.T) {
	conf := `server {
        location / {
            location ~ \.php$ {
                deny all;
            }
        }
}

server {
        location / {
            rewrite ^ https://example.com$request_uri? permanent;
        }
}`
	out, _ := mergeLocations(conf)
	// Nested block stays inside its parent.
	assert.Contains(t, out, "deny all;")
	// The second server block is left untouched.
	assert.Contains(t, out, "rewrite ^ https://example.com$request_uri? permanent;")
	assert.Equal(t, 2, strings.Count(out, "server {"))
}

func TestMergeLocationsSubroot(t *testing.T) {
	conf := "server {\n        root /var/www/example.com/web;\n##subroot public ##\n}"
	out, warnings := mergeLocations(conf)
	assert.Empty(t, warnings)
	assert.Contains(t, out, "root /var/www/example.com/web/public;")

	out, warnings = mergeLocations("server {\n        root /x;\n##subroot ../../etc ##\n}")
	require.Len(t, warnings, 1)
	assert.Contains(t, out, "root /x;")
}

// TestRenderVhostCustomLocationMerged covers the nginx-vhost spec scenario:
// a custom location block from nginx_directives replaces/extends the
// template block after the merge pass, and the {FASTCGIPASS} placeholder is
// substituted.
func TestRenderVhostCustomLocationMerged(t *testing.T) {
	d := goldenDomain()
	d["nginx_directives"] = "location / { ##merge## try_files $uri /index.php?$args; }\n" +
		"location @php2 { {FASTCGIPASS} }"
	content, warnings, err := renderVhost(vhostInput{
		cfg: goldenCfg(), d: d, nginxVersion: "1.26.1", dummyFile: "/d.htm",
	}, "")
	require.NoError(t, err)
	assert.Empty(t, warnings)

	merged, mergeWarnings := mergeLocations(content)
	assert.Empty(t, mergeWarnings)
	assert.Contains(t, merged, "try_files $uri /index.php?$args;")
	assert.Contains(t, merged, "fastcgi_pass unix:/var/lib/php8.3-fpm/web1.sock;")
	assert.NotContains(t, merged, "{FASTCGIPASS}")
	assert.NotContains(t, merged, "##merge##")
}

// TestRenderVhostBlacklistedDirective covers the spec scenario "Blacklisted
// directive is stripped and reported": the line never reaches the output and
// a warning is produced for the datalog error.
func TestRenderVhostBlacklistedDirective(t *testing.T) {
	d := goldenDomain()
	d["nginx_directives"] = "load_module /tmp/evil.so;\nclient_max_body_size 100M;"
	content, warnings, err := renderVhost(vhostInput{
		cfg: goldenCfg(), d: d, nginxVersion: "1.26.1", dummyFile: "/d.htm",
	}, "")
	require.NoError(t, err)
	require.Len(t, warnings, 1)
	assert.ErrorContains(t, warnings[0], "load_module")
	assert.NotContains(t, content, "load_module")
	assert.Contains(t, content, "client_max_body_size 100M;")
}
