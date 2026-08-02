package nginx

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// AcmeWebroot is the directory every vhost serves under
// /.well-known/acme-challenge (matches the acme location root in
// nginx_vhost.conf.master). The Let's Encrypt client writes challenge files
// here and the reachability probe reads them back over HTTP.
const AcmeWebroot = "/usr/local/ispconfig/interface/acme"

// leProbeTimeout bounds one domain-reachability HTTP request.
const leProbeTimeout = 8 * time.Second

// leClientKind identifies the detected ACME client.
type leClientKind int

const (
	leNone leClientKind = iota
	leAcme
	leCertbot
)

// leClient wraps the detected acme.sh / certbot binary (Go port of
// letsencrypt.inc.php, nginx paths only). It never installs a client:
// installation belongs to the installer change; a missing client is a clear
// error recorded to the datalog.
type leClient struct {
	plugin  *Plugin
	kind    leClientKind
	script  string
	version string
	webroot string
	// httpGet is the reachability probe, overridable in tests.
	httpGet func(url string) (string, error)
}

// acmeScriptCandidates / certbotCandidates mirror the PHP `which` lists.
var acmeScriptCandidates = []string{"acme.sh", "/root/.acme.sh/acme.sh"}
var certbotCandidates = []string{"certbot", "/opt/eff.org/certbot/venv/bin/certbot", "letsencrypt"}

// newLEClient detects an ACME client (acme.sh preferred, certbot fallback).
// It returns a client with kind leNone when neither is found.
func (p *Plugin) newLEClient(ctx context.Context) *leClient {
	c := &leClient{plugin: p, webroot: p.acmeWebroot(), httpGet: httpGetString}
	if script := p.whichExecutable(ctx, acmeScriptCandidates); script != "" {
		c.kind, c.script = leAcme, script
		c.version = p.probeClientVersion(ctx, script)
	} else if script := p.whichExecutable(ctx, certbotCandidates); script != "" {
		c.kind, c.script = leCertbot, script
		c.version = p.probeClientVersion(ctx, script)
	}
	return c
}

// acmeWebroot returns the configured webroot (test override or default).
func (p *Plugin) acmeWebroot() string {
	if p.leWebroot != "" {
		return p.leWebroot
	}
	return AcmeWebroot
}

// whichExecutable runs `which` over the candidates and returns the first
// executable path.
func (p *Plugin) whichExecutable(ctx context.Context, candidates []string) string {
	out, err := p.runner.Run(ctx, "which", candidates...)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if info, err := os.Stat(line); err == nil && info.Mode()&0o111 != 0 {
			return line
		}
	}
	return ""
}

// versionRe extracts a dotted version from `<client> --version` output.
var versionRe = regexp.MustCompile(`(\d+(?:\.\d+)+)`)

// probeClientVersion returns the client's version string ("" when unknown).
func (p *Plugin) probeClientVersion(ctx context.Context, script string) string {
	out, err := p.runner.Run(ctx, script, "--version")
	if err != nil {
		return ""
	}
	if m := versionRe.FindSubmatch(out); m != nil {
		return string(m[1])
	}
	return ""
}

// certType picks ECDSA vs RSA for a client version (port of the version
// gates): acme.sh >= 2.6.4 or certbot >= 2.0 support ec-256 when the site
// asked for ECDSA; otherwise RSA 4096.
func (c *leClient) certType(desired string) string {
	if desired != "ECDSA" {
		return "RSA"
	}
	switch c.kind {
	case leAcme:
		if versionGE(c.version, "2.6.4") {
			return "ECDSA"
		}
	case leCertbot:
		if versionGE(c.version, "2.0") {
			return "ECDSA"
		}
	case leNone:
	}
	return "RSA"
}

// assembleDomains ports assemble_domains_to_request: main domain (+www when
// the site is www/wildcard), active subdomains and aliases not excluded via
// ssl_letsencrypt_exclude, de-duplicated, optionally filtered by an HTTP
// reachability probe and capped at 100.
func (c *leClient) assembleDomains(ctx context.Context, d row, mainDomain string, doCheck bool) ([]string, error) {
	domains := []string{mainDomain}
	if !strings.HasPrefix(mainDomain, "www.") && (d.str("subdomain") == "www" || d.str("subdomain") == "*") {
		domains = append(domains, "www."+mainDomain)
	}

	p := c.plugin
	if p.db != nil {
		var subs []string
		if err := p.db.Table("web_domain").
			Where("parent_domain_id = ? AND active = 'y' AND type = 'subdomain' AND ssl_letsencrypt_exclude != 'y'", d.num("domain_id")).
			Pluck("domain", &subs).Error; err != nil {
			return nil, fmt.Errorf("nginx: loading LE subdomains: %w", err)
		}
		domains = append(domains, subs...)

		var aliases []map[string]any
		if err := p.db.Table("web_domain").
			Select("domain", "subdomain").
			Where("parent_domain_id = ? AND active = 'y' AND type = 'alias' AND ssl_letsencrypt_exclude != 'y'", d.num("domain_id")).
			Find(&aliases).Error; err != nil {
			return nil, fmt.Errorf("nginx: loading LE aliases: %w", err)
		}
		for _, a := range aliases {
			ar := row(a)
			domains = append(domains, ar.str("domain"))
			if !strings.HasPrefix(ar.str("domain"), "www.") && (ar.str("subdomain") == "www" || ar.str("subdomain") == "*") {
				domains = append(domains, "www."+ar.str("domain"))
			}
		}
	}

	domains = dedupStrings(domains)
	if doCheck {
		domains = c.filterReachable(domains)
	}
	if len(domains) > 100 {
		domains = domains[:100]
	}
	return domains, nil
}

// filterReachable keeps only the domains that serve a challenge token written
// to the webroot (port of the reachability check that avoids LE validation
// failures).
func (c *leClient) filterReachable(domains []string) []string {
	token := fmt.Sprintf("le-%d.txt", time.Now().UnixNano())
	hash := fmt.Sprintf("le-%x", time.Now().UnixNano())
	challengeDir := filepath.Join(c.webroot, ".well-known", "acme-challenge")
	if err := os.MkdirAll(challengeDir, 0o755); err != nil {
		c.plugin.log.Warn("nginx: LE reachability check skipped, webroot unwritable", "error", err)
		return domains
	}
	tokenFile := filepath.Join(challengeDir, token)
	if err := os.WriteFile(tokenFile, []byte(hash), 0o644); err != nil {
		c.plugin.log.Warn("nginx: LE reachability check skipped", "error", err)
		return domains
	}
	defer func() { _ = os.Remove(tokenFile) }()

	var reachable []string
	for _, domain := range domains {
		url := "http://" + domain + "/.well-known/acme-challenge/" + token
		if got, err := c.httpGet(url); err == nil && strings.TrimSpace(got) == hash {
			reachable = append(reachable, domain)
		} else {
			c.plugin.log.Warn("nginx: excluding unreachable domain from LE request", "domain", domain)
		}
	}
	return reachable
}

// acmeIssueArgs builds the acme.sh --issue argv (port of get_acme_command
// issue line); useSocket-style shell sequencing is replaced by separate
// runner calls in requestCert.
func (c *leClient) acmeIssueArgs(domains []string, certType string) []string {
	args := []string{"--issue"}
	for _, d := range domains {
		args = append(args, "-d", d)
	}
	args = append(args, "-w", c.webroot, "--always-force-new-domain-key")
	if certType == "ECDSA" {
		args = append(args, "--ecc", "--keylength", "ec-256")
	} else {
		args = append(args, "--keylength", "4096")
	}
	return args
}

// acmeInstallArgs builds the acme.sh --install-cert argv (nginx: fullchain
// only), pointing at the site's -le files with the nginx reload hook.
func (c *leClient) acmeInstallArgs(domains []string, certType, keyFile, crtFile string) []string {
	args := []string{"--install-cert"}
	for _, d := range domains {
		args = append(args, "-d", d)
	}
	if certType == "ECDSA" {
		args = append(args, "--ecc")
	}
	args = append(args, "--key-file", keyFile, "--fullchain-file", crtFile,
		"--reloadcmd", "nginx -s reload")
	return args
}

// certbotArgs builds the certbot certonly argv for a modern certbot
// (>=0.30: --cert-name + --webroot-map). Older certbot is not supported on
// the target distros.
//
// ponytail: modern-certbot only; add the pre-0.30 --expand/--domains branch
// if a legacy distro ever matters.
func (c *leClient) certbotArgs(domains []string, certType string) []string {
	primary := domains[0]
	name := primary
	keyArgs := []string{"--rsa-key-size", "4096"}
	if certType == "ECDSA" {
		keyArgs = []string{"--elliptic-curve", "secp256r1"}
		name += "_ecc"
	}
	args := []string{
		"certonly", "-n", "--text", "--agree-tos",
		"--cert-name", name, "--authenticator", "webroot",
		"--server", "https://acme-v02.api.letsencrypt.org/directory",
	}
	args = append(args, keyArgs...)
	args = append(args, "--email", "webmaster@"+primary)
	for _, d := range domains {
		args = append(args, "-d", d)
	}
	args = append(args, "--webroot-path", c.webroot)
	return args
}

// leCertPaths returns the -le suffixed key/crt/bundle paths for a domain's
// ssl dir (port of get_website_certificate_paths for ssl_letsencrypt=y).
func leCertPaths(docroot, domain string) (key, crt, bundle string) {
	dir := filepath.Join(docroot, "ssl")
	return filepath.Join(dir, domain+"-le.key"),
		filepath.Join(dir, domain+"-le.crt"),
		filepath.Join(dir, domain+"-le.bundle")
}

// leSSLDomain returns the certificate main domain for a site (wildcard
// stripped, ssl_domain honored) — port of get_ssl_domain.
func leSSLDomain(d row) string {
	domain := d.str("ssl_domain")
	if domain == "" {
		domain = d.str("domain")
	}
	if d.str("ssl") == "y" && d.str("ssl_letsencrypt") == "y" {
		domain = d.str("domain")
		domain = strings.TrimPrefix(domain, "*.")
	}
	return domain
}

// requestCert issues a certificate for the site (port of
// request_certificates, nginx server_type). It returns true when a cert is in
// place. acme.sh installs the files directly; certbot's output is linked into
// the site ssl dir. A missing client or an issuance failure returns false
// with the reason, never an on-disk change that breaks the vhost.
func (p *Plugin) requestCert(ctx context.Context, cfg webLEConfig, d row) (bool, error) {
	c := p.newLEClient(ctx)
	if c.kind == leNone {
		return false, fmt.Errorf("nginx: no Let's Encrypt client (acme.sh/certbot) found for %s", d.str("domain"))
	}

	desired := cfg.signatureType
	certType := c.certType(desired)
	mainDomain := leSSLDomain(d)
	keyFile, crtFile, _ := leCertPaths(d.str("document_root"), mainDomain)

	domains, err := c.assembleDomains(ctx, d, mainDomain, !cfg.skipCheck)
	if err != nil {
		return false, err
	}
	if len(domains) == 0 {
		return false, fmt.Errorf("nginx: no reachable domains for LE request on %s", d.str("domain"))
	}

	switch c.kind {
	case leAcme:
		// acme.sh installs copies directly at key/crt via --install-cert.
		for _, f := range []string{keyFile, crtFile} {
			_ = removeLinkOnly(f)
		}
		if out, err := p.runner.Run(ctx, c.script, c.acmeIssueArgs(domains, certType)...); err != nil {
			return false, fmt.Errorf("nginx: acme.sh issue failed for %s: %w: %s", mainDomain, err, out)
		}
		if out, err := p.runner.Run(ctx, c.script, c.acmeInstallArgs(domains, certType, keyFile, crtFile)...); err != nil {
			return false, fmt.Errorf("nginx: acme.sh install-cert failed for %s: %w: %s", mainDomain, err, out)
		}
		return true, nil
	case leCertbot:
		if out, err := p.runner.Run(ctx, c.script, c.certbotArgs(domains, certType)...); err != nil {
			return false, fmt.Errorf("nginx: certbot failed for %s: %w: %s", mainDomain, err, out)
		}
		live := filepath.Join(certbotLiveDir, mainDomain)
		if certType == "ECDSA" {
			live += "-ecc"
		}
		if err := linkFile(keyFile, filepath.Join(live, "privkey.pem")); err != nil {
			return false, err
		}
		if err := linkFile(crtFile, filepath.Join(live, "fullchain.pem")); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, fmt.Errorf("nginx: no LE client")
}

// certbotLiveDir is where certbot stores issued certificates.
const certbotLiveDir = "/etc/letsencrypt/live"

// webLEConfig carries the LE-relevant server config values.
type webLEConfig struct {
	signatureType string // le_signature_type ("RSA"/"ECDSA")
	skipCheck     bool   // skip_le_check == 'y'
}

// linkFile symlinks target -> source, replacing a stale link and backing up a
// real file (port of link_file).
func linkFile(target, source string) error {
	if info, err := os.Lstat(target); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			if existing, _ := os.Readlink(target); existing == source {
				return nil
			}
			if err := os.Remove(target); err != nil {
				return fmt.Errorf("nginx: replacing link %s: %w", target, err)
			}
		} else {
			backup := target + ".old." + time.Now().Format("20060102150405")
			if err := copyFileMode(target, backup, 0o400); err != nil {
				return fmt.Errorf("nginx: backing up %s: %w", target, err)
			}
			if err := os.Remove(target); err != nil {
				return fmt.Errorf("nginx: removing %s: %w", target, err)
			}
		}
	}
	if err := os.Symlink(source, target); err != nil {
		return fmt.Errorf("nginx: linking %s -> %s: %w", target, source, err)
	}
	return nil
}

// removeLinkOnly removes target only when it is a symlink (acme.sh needs a
// real file at the install path).
func removeLinkOnly(target string) error {
	if info, err := os.Lstat(target); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return os.Remove(target)
	}
	return nil
}

// dedupStrings returns the input with duplicates removed, order preserved.
func dedupStrings(in []string) []string {
	seen := map[string]bool{}
	out := in[:0:0]
	for _, s := range in {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// httpGetString GETs url and returns the body as a string.
func httpGetString(url string) (string, error) {
	client := &http.Client{Timeout: leProbeTimeout}
	resp, err := client.Get(url) //nolint:noctx // short bounded probe with its own timeout
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	return string(body), err
}
