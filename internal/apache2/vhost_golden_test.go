package apache2

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/getconf"
)

var update = flag.Bool("update", false, "rewrite golden files with current render output")

// testWebConfig is the [web] section of an Apache server, matching the lab
// VM at 192.168.56.21 (Debian 12, Apache 2.4.58).
func testWebConfig() *getconf.WebConfig {
	return &getconf.WebConfig{
		ServerType:            "apache",
		WebsiteBasedir:        "/var/www",
		VhostConfDir:          "/etc/apache2/sites-available",
		VhostConfEnabledDir:   "/etc/apache2/sites-enabled",
		SecurityLevel:         "20",
		HtaccessAllowOverride: "All",
		PHPFPMPoolDir:         "/etc/php/8.2/fpm/pool.d",
		PHPFPMSocketDir:       "/var/lib/php8.2-fpm",
		PHPFPMStartPort:       "9010",
		PHPFPMInitScript:      "php8.2-fpm",
	}
}

// testDomain is a representative web_domain row: a PHP-FPM site over a unix
// socket, suexec on, error documents and SSI enabled.
func testDomain() row {
	return row{
		"domain_id":          int64(3),
		"server_id":          int64(1),
		"domain":             "site.example.com",
		"type":               "vhost",
		"active":             "y",
		"document_root":      "/var/www/clients/client1/web3",
		"system_user":        "web3",
		"system_group":       "client1",
		"ip_address":         "*",
		"php":                "php-fpm",
		"php_fpm_use_socket": "y",
		"suexec":             "y",
		"ssi":                "y",
		"errordocs":          "1",
		"subdomain":          "www",
		"allow_override":     "All",
		"php_open_basedir":   "/var/www/clients/client1/web3/web:/var/www/clients/client1/web3/private:/var/www/clients/client1/web3/tmp",
		"ssl":                "n",
		"logging":            "yes",
		"seo_redirect":       "",
		"redirect_type":      "",
	}
}

func renderForTest(t *testing.T, in vhostInput) string {
	t.Helper()
	out, _, err := renderVhost(in, "")
	require.NoError(t, err)
	return out
}

func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		require.NoError(t, os.MkdirAll("testdata", 0o755))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
		return
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "missing golden file; run: go test ./internal/apache2 -run TestVhostGolden -update")
	require.Equal(t, string(want), got)
}

// TestVhostGolden pins the rendered Apache vhost. Any change to the template
// or the variable vector shows up here as a diff.
func TestVhostGolden(t *testing.T) {
	got := renderForTest(t, vhostInput{
		cfg:           testWebConfig(),
		d:             testDomain(),
		apacheVersion: "2.4.58",
		ips:           defaultListeners(testDomain(), false),
	})
	checkGolden(t, "vhost_phpfpm_socket", got)
}

// TestVhostApache24Syntax guards the version gate: rendering against 2.4
// must emit the modern authorization syntax, never the 2.2 spelling that a
// stock 2.4 server (no mod_access_compat) rejects outright.
func TestVhostApache24Syntax(t *testing.T) {
	got := renderForTest(t, vhostInput{
		cfg:           testWebConfig(),
		d:             testDomain(),
		apacheVersion: "2.4.58",
		ips:           defaultListeners(testDomain(), false),
	})
	require.Contains(t, got, "Require all granted")
	require.Contains(t, got, "Require all denied")
	require.NotContains(t, got, "Order allow,deny")
	require.NotContains(t, got, "Order Deny,Allow")
}

// TestVhostFPMHandlerExclusive checks that exactly one PHP handler wiring is
// emitted: a vhost carrying both the socket and the TCP SetHandler makes
// Apache use whichever came last, silently ignoring the site's setting.
func TestVhostFPMHandlerExclusive(t *testing.T) {
	socket := renderForTest(t, vhostInput{
		cfg: testWebConfig(), d: testDomain(), apacheVersion: "2.4.58",
		ips: defaultListeners(testDomain(), false),
	})
	require.Contains(t, socket, "proxy:unix:/var/lib/php8.2-fpm/web3.sock|fcgi://localhost")
	require.NotContains(t, socket, "proxy:fcgi://127.0.0.1:")

	tcpRow := testDomain()
	tcpRow["php_fpm_use_socket"] = "n"
	tcp := renderForTest(t, vhostInput{
		cfg: testWebConfig(), d: tcpRow, apacheVersion: "2.4.58",
		ips: defaultListeners(tcpRow, false),
	})
	// php_fpm_start_port 9010 + domain_id 3 - 1
	require.Contains(t, tcp, "proxy:fcgi://127.0.0.1:9012")
	require.NotContains(t, tcp, "proxy:unix:")
}

// TestVhostSSLListeners checks that an SSL site gets both listeners and that
// the certificate paths point at the site's ssl dir.
func TestVhostSSLListeners(t *testing.T) {
	d := testDomain()
	d["ssl"] = "y"
	d["ssl_domain"] = "site.example.com"
	got := renderForTest(t, vhostInput{
		cfg: testWebConfig(), d: d, apacheVersion: "2.4.58",
		ips: defaultListeners(d, true),
	})
	require.Equal(t, 2, strings.Count(got, "<VirtualHost "))
	require.Contains(t, got, "<VirtualHost *:80>")
	require.Contains(t, got, "<VirtualHost *:443>")
	require.Contains(t, got, "SSLCertificateFile /var/www/clients/client1/web3/ssl/site.example.com.crt")
	require.Contains(t, got, "SSLCertificateKeyFile /var/www/clients/client1/web3/ssl/site.example.com.key")
}

// TestVhostPHPDisabledDenies checks that php=no actually blocks PHP files
// rather than merely omitting the handler — an omitted handler on a server
// with mod_php loaded would still execute them.
func TestVhostPHPDisabledDenies(t *testing.T) {
	d := testDomain()
	d["php"] = "no"
	got := renderForTest(t, vhostInput{
		cfg: testWebConfig(), d: d, apacheVersion: "2.4.58",
		ips: defaultListeners(d, false),
	})
	require.Contains(t, got, "SetHandler None")
	require.Contains(t, got, "<Files ~ '.php[s3-6]{0,1}$'>")
	require.NotContains(t, got, "proxy:unix:")
}

// TestSafeDomainRejectsTraversal is the path-safety gate: a migrated or
// tampered row must never let the root daemon write outside its conf dir.
func TestSafeDomainRejectsTraversal(t *testing.T) {
	for _, bad := range []string{"", "../../etc/apache2", "a/b.com", "..", "foo bar.com", "-flag.com"} {
		require.Error(t, safeDomain(bad), "expected %q to be rejected", bad)
	}
	for _, good := range []string{"example.com", "*.example.com", "sub.example.co.uk"} {
		require.NoError(t, safeDomain(good), "expected %q to be accepted", good)
	}
}

// TestSafeSitePathRejectsEscape guards every destructive filesystem call.
func TestSafeSitePathRejectsEscape(t *testing.T) {
	const base = "/var/www"
	for _, bad := range []string{"/etc/passwd", "/var/www", "/", "/var/www/../etc", "relative/path", ""} {
		require.Error(t, safeSitePath(bad, base), "expected %q to be rejected", bad)
	}
	require.NoError(t, safeSitePath("/var/www/clients/client1/web3", base))
}
