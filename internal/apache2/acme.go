package apache2

import (
	"context"
	"fmt"

	"go-ispconfig/internal/acme"
	"go-ispconfig/internal/getconf"
)

// webLEConfig carries the LE-relevant server config values.
type webLEConfig struct {
	signatureType string
	skipCheck     bool
}

// maybeRequestLE issues a Let's Encrypt certificate when the site turned LE
// on or changed its domain (port of request_certificates in the Apache plugin).
func (p *Plugin) maybeRequestLE(ctx context.Context, oldRow, newRow row, cfg *getconf.WebConfig) []error {
	if newRow.str("ssl") != "y" || newRow.str("ssl_letsencrypt") != "y" {
		return nil
	}
	changed := oldRow.str("ssl") == "n" || oldRow.str("ssl_letsencrypt") == "n" ||
		oldRow.str("domain") != newRow.str("domain") || oldRow.str("subdomain") != newRow.str("subdomain")
	if !changed {
		return nil
	}
	leCfg := webLEConfig{
		signatureType: cfg.LeSignatureType,
		skipCheck:     cfg.SkipLeCheck == "y",
	}
	ok, err := p.requestCert(ctx, leCfg, newRow)
	if err != nil {
		return []error{err}
	}
	_ = ok
	return nil
}

// requestCert issues a certificate via the native ACME client and links it
// into the site's ssl directory.
func (p *Plugin) requestCert(ctx context.Context, cfg webLEConfig, d row) (bool, error) {
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
		return false, fmt.Errorf("apache2: no reachable domains for LE request on %s", d.str("domain"))
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
		return false, fmt.Errorf("apache2: LE issuance failed for %s: %w", mainDomain, err)
	}
	return true, nil
}
