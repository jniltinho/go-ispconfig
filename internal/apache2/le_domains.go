package apache2

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

const acmeWebroot = "/usr/local/ispconfig/interface/acme"

const leProbeTimeout = 8 * time.Second

func (p *Plugin) acmeWebroot() string {
	if p.leWebroot != "" {
		return p.leWebroot
	}
	return acmeWebroot
}

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

func leCertPaths(docroot, domain string) (key, crt, bundle string) {
	dir := filepath.Join(docroot, "ssl")
	return filepath.Join(dir, domain+"-le.key"),
		filepath.Join(dir, domain+"-le.crt"),
		filepath.Join(dir, domain+"-le.bundle")
}

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
			return nil, fmt.Errorf("apache2: loading LE subdomains: %w", err)
		}
		domains = append(domains, subs...)

		var aliases []map[string]any
		if err := p.db.Table("web_domain").
			Select("domain", "subdomain").
			Where("parent_domain_id = ? AND active = 'y' AND type = 'alias' AND ssl_letsencrypt_exclude != 'y'", d.num("domain_id")).
			Find(&aliases).Error; err != nil {
			return nil, fmt.Errorf("apache2: loading LE aliases: %w", err)
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

func (p *Plugin) filterReachable(domains []string) []string {
	httpGet := httpGetString
	if p.leHTTPGet != nil {
		httpGet = p.leHTTPGet
	}
	token := fmt.Sprintf("le-%d.txt", time.Now().UnixNano())
	hash := fmt.Sprintf("le-%x", time.Now().UnixNano())
	challengeDir := filepath.Join(p.acmeWebroot(), ".well-known", "acme-challenge")
	if err := os.MkdirAll(challengeDir, 0o755); err != nil {
		p.log.Warn("apache2: LE reachability check skipped, webroot unwritable", "error", err)
		return domains
	}
	tokenFile := filepath.Join(challengeDir, token)
	if err := os.WriteFile(tokenFile, []byte(hash), 0o644); err != nil {
		p.log.Warn("apache2: LE reachability check skipped", "error", err)
		return domains
	}
	defer func() { _ = os.Remove(tokenFile) }()

	var reachable []string
	for _, domain := range domains {
		url := "http://" + domain + "/.well-known/acme-challenge/" + token
		if got, err := httpGet(url); err == nil && strings.TrimSpace(got) == hash {
			reachable = append(reachable, domain)
		} else {
			p.log.Warn("apache2: excluding unreachable domain from LE request", "domain", domain)
		}
	}
	return reachable
}

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

func httpGetString(url string) (string, error) {
	client := &http.Client{Timeout: leProbeTimeout}
	resp, err := client.Get(url) //nolint:noctx
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
