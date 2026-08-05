package nginx

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go-ispconfig/internal/acme"
)

// AcmeWebroot is the directory every vhost serves under
// /.well-known/acme-challenge (matches the acme location root in
// nginx_vhost.conf.master). The Let's Encrypt client writes challenge files
// here and the reachability probe reads them back over HTTP.
const AcmeWebroot = "/usr/local/ispconfig/interface/acme"

// leProbeTimeout bounds one domain-reachability HTTP request.
const leProbeTimeout = 8 * time.Second

// acmeManager returns the native ACME manager for this server.
func (p *Plugin) acmeManager(serverID uint32, signatureType string) *acme.Manager {
	return acme.NewManager(acme.ManagerConfig{
		Client: acme.Config{
			Webroot:  p.acmeWebroot(),
			ServerID: serverID,
			Email:    "webmaster@localhost",
			KeyType:  signatureType,
			Log:      p.log,
		},
		Log: p.log,
	})
}

// acmeWebroot returns the configured webroot (test override or default).
func (p *Plugin) acmeWebroot() string {
	if p.leWebroot != "" {
		return p.leWebroot
	}
	return AcmeWebroot
}

// assembleDomains ports assemble_domains_to_request: main domain (+www when
// the site is www/wildcard), active subdomains and aliases not excluded via
// ssl_letsencrypt_exclude, de-duplicated, optionally filtered by an HTTP
// reachability probe and capped at 100.
func (p *Plugin) assembleDomains(ctx context.Context, d row, mainDomain string, doCheck bool) ([]string, error) {
	domains := []string{mainDomain}
	if !strings.HasPrefix(mainDomain, "www.") && (d.str("subdomain") == "www" || d.str("subdomain") == "*") {
		domains = append(domains, "www."+mainDomain)
	}

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
		domains = p.filterReachable(domains)
	}
	if len(domains) > 100 {
		domains = domains[:100]
	}
	return domains, nil
}

// filterReachable keeps only the domains that serve a challenge token written
// to the webroot (port of the reachability check that avoids LE validation
// failures).
func (p *Plugin) filterReachable(domains []string) []string {
	httpGet := httpGetString
	if p.leHTTPGet != nil {
		httpGet = p.leHTTPGet
	}
	token := fmt.Sprintf("le-%d.txt", time.Now().UnixNano())
	hash := fmt.Sprintf("le-%x", time.Now().UnixNano())
	challengeDir := filepath.Join(p.acmeWebroot(), ".well-known", "acme-challenge")
	if err := os.MkdirAll(challengeDir, 0o755); err != nil {
		p.log.Warn("nginx: LE reachability check skipped, webroot unwritable", "error", err)
		return domains
	}
	tokenFile := filepath.Join(challengeDir, token)
	if err := os.WriteFile(tokenFile, []byte(hash), 0o644); err != nil {
		p.log.Warn("nginx: LE reachability check skipped", "error", err)
		return domains
	}
	defer func() { _ = os.Remove(tokenFile) }()

	var reachable []string
	for _, domain := range domains {
		url := "http://" + domain + "/.well-known/acme-challenge/" + token
		if got, err := httpGet(url); err == nil && strings.TrimSpace(got) == hash {
			reachable = append(reachable, domain)
		} else {
			p.log.Warn("nginx: excluding unreachable domain from LE request", "domain", domain)
		}
	}
	return reachable
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

// requestCert issues a certificate for the site via the native ACME client.
// It returns true when a cert is in place. An issuance failure returns false
// with the reason, never an on-disk change that breaks the vhost.
func (p *Plugin) requestCert(ctx context.Context, cfg webLEConfig, d row) (bool, error) {
	_ = ctx
	if p.leIssue != nil {
		mainDomain := leSSLDomain(d)
		keyFile, crtFile, _ := leCertPaths(d.str("document_root"), mainDomain)
		return p.leIssue(mainDomain, keyFile, crtFile)
	}

	mainDomain := leSSLDomain(d)
	keyFile, crtFile, _ := leCertPaths(d.str("document_root"), mainDomain)

	domains, err := p.assembleDomains(ctx, d, mainDomain, !cfg.skipCheck)
	if err != nil {
		return false, err
	}
	if len(domains) == 0 {
		return false, fmt.Errorf("nginx: no reachable domains for LE request on %s", d.str("domain"))
	}

	serverID := p.serverID
	if serverID == 0 {
		serverID = uint32(d.num("server_id"))
	}
	mgr := p.acmeManager(serverID, cfg.signatureType)
	if p.acmeMgr != nil {
		mgr = p.acmeMgr
	}

	_, err = mgr.IssueForSite(domains, cfg.signatureType, keyFile, crtFile, acme.ChallengeHTTP)
	if err != nil {
		return false, fmt.Errorf("nginx: LE issuance failed for %s: %w", mainDomain, err)
	}
	return true, nil
}

// webLEConfig carries the LE-relevant server config values.
type webLEConfig struct {
	signatureType string // le_signature_type ("RSA"/"ECDSA")
	skipCheck     bool   // skip_le_check == 'y'
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
