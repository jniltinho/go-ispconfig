package nginx

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/getconf"
)

var update = flag.Bool("update", false, "rewrite golden files with current render output")

// goldenCfg is a Debian-ish [web] server config fixture.
func goldenCfg() *getconf.WebConfig {
	return &getconf.WebConfig{
		WebsiteBasedir:   "/var/www",
		SecurityLevel:    "20",
		PHPFPMPoolDir:    "/etc/php/8.3/fpm/pool.d",
		PHPFPMSocketDir:  "/var/lib/php8.3-fpm",
		PHPFPMInitScript: "php8.3-fpm",
		PHPFPMStartPort:  "9010",
		Logging:          "yes",
	}
}

// goldenDomain is the base web_domain fixture: a plain PHP-FPM vhost.
func goldenDomain() row {
	return row{
		"domain_id": float64(1), "server_id": float64(1),
		"domain": "example.com", "type": "vhost", "subdomain": "www",
		"document_root": "/var/www/clients/client1/web1",
		"web_folder":    "", "system_user": "web1", "system_group": "client1",
		"ip_address": "192.168.10.5", "ipv6_address": "",
		"http_port": float64(80), "https_port": float64(443),
		"php": "php-fpm", "php_fpm_use_socket": "y", "php_fpm_chroot": "n",
		"cgi": "n", "ssi": "n", "errordocs": float64(1),
		"ssl": "n", "ssl_domain": "", "rewrite_to_https": "n",
		"seo_redirect": "", "redirect_type": "", "redirect_path": "",
		"stats_type": "", "active": "y", "disable_symlinknotowner": "n",
		"nginx_directives": "", "rewrite_rules": "", "proxy_directives": "",
	}
}

func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("golden", name+".golden")
	if *update {
		require.NoError(t, os.MkdirAll("golden", 0o755))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
		return
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "missing golden file; run: go test ./internal/nginx -run TestGolden -update")
	require.Equal(t, string(want), got, "rendered vhost differs from %s", path)
}

func renderGolden(t *testing.T, in vhostInput) string {
	t.Helper()
	if in.nginxVersion == "" {
		in.nginxVersion = "1.26.1"
	}
	in.dummyFile = "/9f86d081884c7d659a2feaa0c55ad015.htm"
	content, warnings, err := renderVhost(in, "")
	require.NoError(t, err)
	assert.Empty(t, warnings)
	return content
}

// TestGoldenVhostPlain: standard PHP-FPM site, no SSL, no redirects.
func TestGoldenVhostPlain(t *testing.T) {
	checkGolden(t, "vhost_plain", renderGolden(t, vhostInput{cfg: goldenCfg(), d: goldenDomain()}))
}

// TestGoldenVhostSSL: ssl=y with existing cert files, rewrite_to_https and
// TLS 1.3 on a modern nginx.
func TestGoldenVhostSSL(t *testing.T) {
	d := goldenDomain()
	d["ssl"] = "y"
	d["ssl_domain"] = "example.com"
	d["rewrite_to_https"] = "y"
	checkGolden(t, "vhost_ssl", renderGolden(t, vhostInput{
		cfg: goldenCfg(), d: d, sslFilesExist: true,
	}))
}

// TestGoldenVhostRedirect: own external redirect plus one alias domain with
// an external redirect server block.
func TestGoldenVhostRedirect(t *testing.T) {
	d := goldenDomain()
	d["redirect_type"] = "permanent"
	d["redirect_path"] = "[scheme]://other.example.org/"
	alias := row{
		"domain": "old-domain.com", "type": "alias", "subdomain": "www",
		"redirect_type": "permanent", "redirect_path": "https://example.com/",
		"seo_redirect": "non_www_to_www", "proxy_directives": "",
	}
	checkGolden(t, "vhost_redirect", renderGolden(t, vhostInput{
		cfg: goldenCfg(), d: d, aliases: []row{alias},
	}))
}

// TestGoldenVhostSeo: SEO redirect non-www to www plus an alias contributing
// server_name entries and an alias SEO redirect.
func TestGoldenVhostSeo(t *testing.T) {
	d := goldenDomain()
	d["seo_redirect"] = "non_www_to_www"
	alias := row{
		"domain": "alias.example.com", "type": "alias", "subdomain": "none",
		"redirect_type": "", "redirect_path": "",
		"seo_redirect": "non_www_to_www",
	}
	checkGolden(t, "vhost_seo", renderGolden(t, vhostInput{
		cfg: goldenCfg(), d: d, aliases: []row{alias},
	}))
}

// TestGoldenVhostSubdomain: vhostsubdomain with its own web folder.
func TestGoldenVhostSubdomain(t *testing.T) {
	d := goldenDomain()
	d["domain_id"] = float64(7)
	d["domain"] = "blog.example.com"
	d["type"] = "vhostsubdomain"
	d["subdomain"] = "none"
	d["web_folder"] = "blog"
	d["errordocs"] = float64(0)
	checkGolden(t, "vhost_vhostsubdomain", renderGolden(t, vhostInput{cfg: goldenCfg(), d: d}))
}

// TestGoldenVhostAlias: vhostalias sharing the parent docroot.
func TestGoldenVhostAlias(t *testing.T) {
	d := goldenDomain()
	d["domain_id"] = float64(9)
	d["domain"] = "shop.example.org"
	d["type"] = "vhostalias"
	d["subdomain"] = "www"
	d["web_folder"] = "shop"
	d["errordocs"] = float64(0)
	checkGolden(t, "vhost_vhostalias", renderGolden(t, vhostInput{cfg: goldenCfg(), d: d}))
}

// TestGoldenVhostNoPHP: php disabled — no @php location, custom directives
// and a protected folder still render.
func TestGoldenVhostNoPHP(t *testing.T) {
	d := goldenDomain()
	d["php"] = "no"
	d["nginx_directives"] = "client_max_body_size 100M;\nfastcgi_read_timeout 300;"
	folders := []row{{"path": "/admin", "active": "y"}}
	checkGolden(t, "vhost_nophp", renderGolden(t, vhostInput{
		cfg: goldenCfg(), d: d, folders: folders,
	}))
}

// TestVhostRewriteToHTTPS pins the spec scenario without golden indirection:
// the HTTP→HTTPS rewrite appears only with ssl+rewrite enabled.
func TestVhostRewriteToHTTPS(t *testing.T) {
	d := goldenDomain()
	d["ssl"] = "y"
	d["rewrite_to_https"] = "y"
	out := renderGolden(t, vhostInput{cfg: goldenCfg(), d: d, sslFilesExist: true})
	assert.Contains(t, out, `if ($scheme != "https") {`)
	assert.Contains(t, out, "listen 192.168.10.5:443 ssl;")

	d["rewrite_to_https"] = "n"
	out = renderGolden(t, vhostInput{cfg: goldenCfg(), d: d, sslFilesExist: true})
	assert.NotContains(t, out, `if ($scheme != "https") {`)
}

// TestVhostSeoRedirectNonWWW pins the spec scenario: non_www_to_www renders
// a permanent redirect from example.com to www.example.com.
func TestVhostSeoRedirectNonWWW(t *testing.T) {
	d := goldenDomain()
	d["seo_redirect"] = "non_www_to_www"
	out := renderGolden(t, vhostInput{cfg: goldenCfg(), d: d})
	assert.Contains(t, out, `if ($http_host = "example.com") {`)
	assert.Contains(t, out, "$scheme://www.example.com$request_uri? permanent;")
}

// TestVhostFastcgiTCP: TCP mode uses 127.0.0.1 with the stable derived port.
func TestVhostFastcgiTCP(t *testing.T) {
	d := goldenDomain()
	d["php_fpm_use_socket"] = "n"
	d["domain_id"] = float64(3)
	out := renderGolden(t, vhostInput{cfg: goldenCfg(), d: d})
	assert.Contains(t, out, "fastcgi_pass 127.0.0.1:9012;")
}

// TestVhostServerPHPOverride: a pinned server_php row moves the socket dir.
func TestVhostServerPHPOverride(t *testing.T) {
	d := goldenDomain()
	php := row{
		"php_fpm_pool_dir":    "/etc/php/8.2/fpm/pool.d",
		"php_fpm_socket_dir":  "/var/lib/php8.2-fpm",
		"php_fpm_init_script": "php8.2-fpm",
	}
	out := renderGolden(t, vhostInput{cfg: goldenCfg(), d: d, serverPHP: php})
	assert.Contains(t, out, "fastcgi_pass unix:/var/lib/php8.2-fpm/web1.sock;")
}
