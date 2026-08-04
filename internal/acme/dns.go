package acme

import (
	"fmt"
	"strings"

	"github.com/go-acme/lego/v4/challenge"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/providers/dns/cloudflare"
	"github.com/go-acme/lego/v4/providers/dns/digitalocean"
	"github.com/go-acme/lego/v4/providers/dns/route53"
)

// DNSProvider names the supported dns-01 backends. http-01 is the default and
// needs no provider entry.
const (
	ChallengeHTTP = "http-01"
	ChallengeDNS  = "dns-01"
)

// DNSProviderConfig carries credentials for an external dns-01 solver. Only
// one provider is active per issuance; empty means http-01.
type DNSProviderConfig struct {
	Provider string
	// Cloudflare: API token or email+key via env (CLOUDFLARE_DNS_API_TOKEN).
	CloudflareToken string
	// Route53: uses the standard AWS credential chain.
	Route53Region string
	// DigitalOcean: API token (DIGITALOCEAN_TOKEN).
	DigitalOceanToken string
}

// ConfigureChallenge registers the solver on client. challenge selects http-01
// or a dns provider name (cloudflare, route53, digitalocean).
func ConfigureChallenge(client *lego.Client, webroot string, dns DNSProviderConfig, challenge string) error {
	provider := strings.ToLower(strings.TrimSpace(challenge))
	if provider == "" {
		provider = strings.ToLower(strings.TrimSpace(dns.Provider))
	}
	switch provider {
	case "", "http", "http-01", "webroot":
		return client.Challenge.SetHTTP01Provider(NewWebrootSolver(webroot))
	case "dns-01", "dns":
		provider = strings.ToLower(strings.TrimSpace(dns.Provider))
		if provider == "" || provider == "dns" || provider == "dns-01" {
			return fmt.Errorf("acme: dns-01 selected but no provider configured")
		}
	}
	p, err := newDNSProvider(dns, provider)
	if err != nil {
		return err
	}
	return client.Challenge.SetDNS01Provider(p)
}

func newDNSProvider(cfg DNSProviderConfig, provider string) (challenge.Provider, error) {
	switch provider {
	case "cloudflare":
		if cfg.CloudflareToken != "" {
			return cloudflare.NewDNSProviderConfig(&cloudflare.Config{
				AuthToken: cfg.CloudflareToken,
			})
		}
		return cloudflare.NewDNSProvider()
	case "route53", "aws":
		c := &route53.Config{}
		if cfg.Route53Region != "" {
			c.Region = cfg.Route53Region
		}
		return route53.NewDNSProviderConfig(c)
	case "digitalocean", "do":
		if cfg.DigitalOceanToken != "" {
			return digitalocean.NewDNSProviderConfig(&digitalocean.Config{
				AuthToken: cfg.DigitalOceanToken,
			})
		}
		return digitalocean.NewDNSProvider()
	default:
		return nil, fmt.Errorf("acme: unsupported dns provider %q", provider)
	}
}
