package acme

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ManagerConfig configures the high-level orchestrator.
type ManagerConfig struct {
	Client    Config
	StatePath string
	DNS       DNSProviderConfig
	Log       *slog.Logger
}

// Manager orchestrates issuance, site symlinks and the state ledger. One per
// server is enough; IssueForSite is safe across different domains.
type Manager struct {
	client *Client
	state  *StateStore
	dns    DNSProviderConfig
	log    *slog.Logger
}

// NewManager returns a Manager. It does not touch the network.
func NewManager(cfg ManagerConfig) *Manager {
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	return &Manager{
		client: New(cfg.Client),
		state:  NewStateStore(cfg.StatePath),
		dns:    cfg.DNS,
		log:    cfg.Log,
	}
}

// Client returns the underlying ACME client (for renewal jobs and tests).
func (m *Manager) Client() *Client { return m.client }

// IssueForSite obtains a certificate and links it into the site's ssl dir.
// provider is recorded in state.json ("http-01" or the dns provider name).
func (m *Manager) IssueForSite(domains []string, keyType, keyFile, crtFile, provider string) (*Result, error) {
	if len(domains) == 0 {
		return nil, fmt.Errorf("acme: no domains")
	}
	main := domains[0]
	if provider == "" {
		provider = ChallengeHTTP
	}

	res, err := m.client.IssueWith(provider, m.dns, domains, keyType)
	if err != nil {
		m.state.RecordError(main, provider, err.Error())
		return nil, err
	}
	if res.Reused {
		m.log.Debug("certificate reused", "lineage", res.Lineage, "domains", len(domains))
	} else {
		m.log.Info("certificate issued", "lineage", res.Lineage, "domains", len(domains))
	}
	if err := LinkSiteCerts(res.Fullchain, res.Privkey, keyFile, crtFile); err != nil {
		m.state.RecordError(main, provider, err.Error())
		return nil, err
	}
	m.state.RecordSuccess(main, provider, time.Now())
	return res, nil
}

// RenewDue walks renewal/*.conf and re-issues certificates inside the renew
// window. It returns how many lineages were renewed (0 when none needed).
func (m *Manager) RenewDue() (int, error) {
	root := m.client.cfg.Root
	if root == "" {
		root = DefaultRoot
	}
	renewalDir := filepath.Join(root, "renewal")
	entries, err := os.ReadDir(renewalDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	renewed := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".conf") {
			continue
		}
		lineage := strings.TrimSuffix(e.Name(), ".conf")
		domains, err := domainsFromRenewal(filepath.Join(renewalDir, e.Name()))
		if err != nil || len(domains) == 0 {
			m.log.Warn("acme: skipping renewal file", "file", e.Name(), "error", err)
			continue
		}
		ok, err := m.client.covered(lineage, domains)
		if err != nil {
			m.log.Warn("acme: skipping renewal file", "lineage", lineage, "error", err)
			continue
		}
		if ok {
			continue
		}
		provider := m.state.Get(domains[0]).Provider
		if provider == "" {
			provider = ChallengeHTTP
		}
		// A wildcard can only ever be validated by dns-01. Falling back to
		// http-01 because the state lost the provider would fail every night
		// and spend a failed validation each time, which is how an account
		// reaches the CA's rate limit without anyone noticing.
		if provider == ChallengeHTTP && hasWildcard(domains) {
			err := fmt.Errorf("acme: %s covers a wildcard and needs dns-01, but no provider is recorded", lineage)
			m.state.RecordError(domains[0], provider, err.Error())
			m.log.Warn("acme: renewal skipped", "lineage", lineage, "error", err)
			continue
		}
		if _, err := m.client.IssueWith(provider, m.dns, domains, keyTypeFromLineage(lineage)); err != nil {
			m.state.RecordError(domains[0], provider, err.Error())
			m.log.Warn("acme: renewal failed", "lineage", lineage, "error", err)
			continue
		}
		m.state.RecordSuccess(domains[0], provider, time.Now())
		renewed++
	}
	return renewed, nil
}

// domainsFromRenewal parses the domains = line from a certbot renewal config.
func domainsFromRenewal(path string) ([]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "domains = ") {
			continue
		}
		val := strings.TrimPrefix(line, "domains = ")
		parts := strings.Split(val, ", ")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				out = append(out, p)
			}
		}
		return out, nil
	}
	return nil, fmt.Errorf("no domains line in %s", path)
}

func keyTypeFromLineage(lineage string) string {
	if strings.HasSuffix(lineage, "_ecc") {
		return "ecdsa"
	}
	return "rsa"
}

// hasWildcard reports whether any name on the certificate is a wildcard.
func hasWildcard(domains []string) bool {
	for _, d := range domains {
		if strings.HasPrefix(d, "*.") {
			return true
		}
	}
	return false
}
