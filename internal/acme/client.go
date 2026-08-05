package acme

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-acme/lego/v4/certcrypto"
	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/registration"
)

// renewWindow is when a certificate becomes eligible for renewal, matching the
// legacy cron's threshold.
const renewWindow = 30 * 24 * time.Hour

// Config configures a Client. Zero values are the production defaults.
type Config struct {
	// Root is the certificate tree; empty means /etc/letsencrypt.
	Root string
	// Webroot is where http-01 tokens are written; empty means the shared
	// ISPConfig acme directory.
	Webroot string
	// ServerID keys the account directory in a multi-server install.
	ServerID uint32
	// Email is the account contact.
	Email string
	// CADirURL overrides the CA. Empty means Let's Encrypt production; this
	// is the only place that URL appears.
	CADirURL string
	// KeyType is "rsa" (default) or "ecdsa".
	KeyType string
	Log     *slog.Logger
}

// Client issues and renews certificates. One per server is enough; Issue is
// safe to call concurrently for different domains.
type Client struct {
	cfg   Config
	store *Store
	log   *slog.Logger
	dns   DNSProviderConfig
	// challenge names the solver for the next issuance (http-01 or a dns provider).
	challenge string

	mu      sync.Mutex             // guards lego, and serialises registration
	issueMu sync.Mutex             // serialises solver selection + obtain (see IssueWith)
	lego    *lego.Client           // built lazily: no network at construction
	locks   map[string]*sync.Mutex // per-lineage, so two applies of one site do not both call the CA
}

// New returns a Client. It touches neither the network nor the account
// directory, so constructing one on a node that never issues costs nothing.
func New(cfg Config) *Client {
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	return &Client{cfg: cfg, store: NewStore(cfg.Root), log: cfg.Log, locks: map[string]*sync.Mutex{}}
}

// keyType maps the config to lego's, defaulting to RSA-2048 as the legacy does.
func (c *Client) keyType() certcrypto.KeyType {
	if strings.EqualFold(c.cfg.KeyType, "ecdsa") || strings.EqualFold(c.cfg.KeyType, "ec") {
		return certcrypto.EC256
	}
	return certcrypto.RSA2048
}

// caURL is the directory this client talks to.
func (c *Client) caURL() string {
	if c.cfg.CADirURL != "" {
		return c.cfg.CADirURL
	}
	return lego.LEDirectoryProduction
}

// client builds the lego client on first use and registers the account if it
// has not been. Serialised: two first-time issuances would otherwise each
// register and the second would orphan the first's rate-limit history.
func (c *Client) client() (*lego.Client, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lego != nil {
		return c.lego, nil
	}

	dir := AccountDir(c.cfg.Root, c.cfg.ServerID)
	acc, err := LoadOrCreateAccount(dir, c.cfg.Email)
	if err != nil {
		return nil, err
	}

	lc := lego.NewConfig(acc)
	lc.CADirURL = c.caURL()
	lc.Certificate.KeyType = c.keyType()
	client, err := lego.NewClient(lc)
	if err != nil {
		return nil, fmt.Errorf("acme: building client: %w", err)
	}
	dns := c.dns
	if err := ConfigureChallenge(client, c.cfg.Webroot, dns, c.challenge); err != nil {
		return nil, fmt.Errorf("acme: registering challenge solver: %w", err)
	}

	if acc.Registration == nil {
		reg, err := client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
		if err != nil {
			return nil, fmt.Errorf("acme: registering account: %w", err)
		}
		acc.Registration = reg
		if err := acc.Save(dir); err != nil {
			return nil, err
		}
		c.log.Info("acme account registered", "server_id", c.cfg.ServerID, "ca", c.caURL())
	}
	c.lego = client
	return client, nil
}

// lineageLock returns the mutex guarding one lineage's check-then-issue.
func (c *Client) lineageLock(lineage string) *sync.Mutex {
	c.mu.Lock()
	defer c.mu.Unlock()
	m, ok := c.locks[lineage]
	if !ok {
		m = &sync.Mutex{}
		c.locks[lineage] = m
	}
	return m
}

// Result names what Issue produced.
type Result struct {
	Lineage string
	// Paths of the four live/ symlinks, which is what vhosts reference.
	Cert, Chain, Fullchain, Privkey string
	// Reused is true when a valid certificate already covered the request
	// and no CA call was made.
	Reused bool
}

// IssueWith selects the solver and issues under one lock, so a concurrent
// issuance cannot change the solver between the two.
//
// SetChallenge mutates client-wide state and Issue reads it, so calling them
// in sequence is a race the per-lineage locks do not cover: site A selects
// http-01, site B selects dns-01 before A reaches Obtain, and A's certificate
// is then validated with B's solver — which fails, or worse succeeds against
// the wrong zone. Issuance is rare and network-bound, so serialising it
// node-wide costs nothing worth measuring.
func (c *Client) IssueWith(provider string, dns DNSProviderConfig, domains []string, keyType string) (*Result, error) {
	c.issueMu.Lock()
	defer c.issueMu.Unlock()
	c.SetChallenge(provider, dns)
	return c.Issue(domains, keyType)
}

// SetChallenge selects the solver for the next issuance and rebuilds the
// lego client if the choice changed. Prefer IssueWith: on its own this is
// only safe when no other issuance can run concurrently.
func (c *Client) SetChallenge(provider string, dns DNSProviderConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.challenge == provider && c.dns == dns {
		return
	}
	c.challenge = provider
	c.dns = dns
	c.lego = nil
}

// Issue obtains a certificate for domains, the first being the main one.
func (c *Client) Issue(domains []string, keyType string) (*Result, error) {
	if len(domains) == 0 {
		return nil, errors.New("acme: no domains")
	}
	if keyType == "" {
		keyType = c.cfg.KeyType
	}
	lineage, err := Lineage(domains[0], keyType)
	if err != nil {
		return nil, err
	}

	lock := c.lineageLock(lineage)
	lock.Lock()
	defer lock.Unlock()

	if ok, err := c.covered(lineage, domains); err != nil {
		return nil, err
	} else if ok {
		return c.result(lineage, true), nil
	}

	// Refuse locally while a previous failure's backoff holds (D8), before
	// anything reaches the CA.
	if blocked, until := c.blocked(lineage, time.Now()); blocked {
		return nil, fmt.Errorf("acme: %s failed recently, not retrying until %s",
			lineage, until.Format(time.RFC3339))
	}

	client, err := c.client()
	if err != nil {
		c.recordFailure(lineage, time.Now(), err.Error())
		return nil, err
	}
	res, err := client.Certificate.Obtain(certificate.ObtainRequest{
		Domains: domains,
		Bundle:  true,
	})
	if err != nil {
		c.recordFailure(lineage, time.Now(), err.Error())
		return nil, fmt.Errorf("acme: obtaining certificate for %s: %w", domains[0], err)
	}

	leaf, chain := splitChain(res.Certificate)
	err = c.store.Save(lineage, Material{
		Cert:      leaf,
		Chain:     chain,
		Fullchain: res.Certificate,
		Privkey:   res.PrivateKey,
	}, domains, c.caURL())
	if err != nil {
		c.recordFailure(lineage, time.Now(), err.Error())
		return nil, fmt.Errorf("acme: persisting certificate for %s: %w", domains[0], err)
	}
	c.clearFailure(lineage)
	c.log.Info("certificate issued", "lineage", lineage, "domains", len(domains))
	return c.result(lineage, false), nil
}

// result fills the live paths for a lineage.
func (c *Client) result(lineage string, reused bool) *Result {
	cert, chain, fullchain, privkey := c.store.LivePaths(lineage)
	return &Result{
		Lineage: lineage, Cert: cert, Chain: chain,
		Fullchain: fullchain, Privkey: privkey, Reused: reused,
	}
}

// covered reports whether the stored certificate already satisfies the
// request: same domain set, more than the renew window left.
func (c *Client) covered(lineage string, domains []string) (bool, error) {
	_, _, fullchain, _ := c.store.LivePaths(lineage)
	raw, err := os.ReadFile(fullchain)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return false, nil
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		// An unreadable certificate is replaced, not diagnosed.
		return false, nil //nolint:nilerr
	}
	if time.Until(leaf.NotAfter) <= renewWindow {
		return false, nil
	}
	return sameDomains(leaf, domains), nil
}

// sameDomains compares the certificate's names with the request's, order and
// duplicates ignored.
func sameDomains(leaf *x509.Certificate, want []string) bool {
	have := map[string]bool{}
	for _, n := range leaf.DNSNames {
		have[strings.ToLower(n)] = true
	}
	for _, n := range want {
		if !have[strings.ToLower(n)] {
			return false
		}
	}
	return len(have) == len(uniqueLower(want))
}

func uniqueLower(in []string) map[string]bool {
	out := map[string]bool{}
	for _, s := range in {
		out[strings.ToLower(s)] = true
	}
	return out
}

// splitChain separates the leaf from the intermediates of a bundled PEM, which
// is how certbot stores cert.pem and chain.pem separately.
func splitChain(bundle []byte) (leaf, chain []byte) {
	rest := bundle
	first := true
	for {
		block, remainder := pem.Decode(rest)
		if block == nil {
			break
		}
		encoded := pem.EncodeToMemory(block)
		if first {
			leaf = encoded
			first = false
		} else {
			chain = append(chain, encoded...)
		}
		rest = remainder
	}
	if leaf == nil {
		// Not PEM: store it whole rather than losing it.
		return bundle, nil
	}
	return leaf, chain
}
