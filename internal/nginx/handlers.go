package nginx

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"go-ispconfig/internal/engine"
)

// webDomainInsert handles web_domain_insert (port of insert(): same path as
// update with action=insert).
func (p *Plugin) webDomainInsert(ctx context.Context, _ string, data engine.Data) error {
	return p.applyWebDomain(ctx, "insert", row(data.Old), row(data.New))
}

// webDomainUpdate handles web_domain_update.
func (p *Plugin) webDomainUpdate(ctx context.Context, _ string, data engine.Data) error {
	return p.applyWebDomain(ctx, "update", row(data.Old), row(data.New))
}

// applyWebDomain is the shared insert/update path (port of update()): for
// non-vhost records (alias/subdomain) the parent site is re-rendered; for
// vhost-type records the site is provisioned and its vhost written and
// activated.
func (p *Plugin) applyWebDomain(ctx context.Context, action string, oldRow, newRow row) error {
	// Alias and subdomain records render inside their parent's vhost: apply
	// the parent instead (port of the parent_domain_id redirect).
	if !isVhostType(newRow.str("type")) {
		if newRow.num("parent_domain_id") <= 0 {
			p.log.Warn("nginx: ignoring non-vhost domain without parent", "domain", newRow.str("domain"))
			return nil
		}
		var errs []error
		if action == "update" && oldRow.num("parent_domain_id") != newRow.num("parent_domain_id") {
			if err := p.applyParent(ctx, oldRow.num("parent_domain_id")); err != nil {
				errs = append(errs, err)
			}
		}
		errs = append(errs, p.applyParent(ctx, newRow.num("parent_domain_id")))
		return errors.Join(errs...)
	}

	cfg, err := p.webConfig(uint32(newRow.num("server_id")))
	if err != nil {
		return err
	}
	if newRow.str("document_root") == "" {
		return fmt.Errorf("nginx: document_root not set for %s", newRow.str("domain"))
	}

	s := site{
		cfg: cfg, action: action, old: oldRow, new: newRow,
		clientID:    p.clientIDOf(newRow.num("sys_groupid")),
		oldClientID: p.clientIDOf(oldRow.num("sys_groupid")),
	}
	// The ssl handler ran first this event; if it rewrote this domain's certs
	// the activation restores the SSL backups on an nginx -t failure.
	if p.sslChangedDomain == newRow.str("domain") {
		s.sslChanged = true
		p.sslChangedDomain = ""
	}
	if newRow.str("type") != "vhost" {
		s.parentDomain = p.domainName(newRow.num("parent_domain_id"))
		s.oldParentDomain = p.domainName(oldRow.num("parent_domain_id"))
	}
	if err := p.ensureSite(ctx, s); err != nil {
		return err
	}
	// Let's Encrypt issuance runs before the vhost render so the -le cert
	// files exist when the vhost referencing them is written. Failures are
	// non-fatal (surfaced as datalog errors): the site falls back to its
	// previous cert or to non-SSL.
	leWarnings := p.maybeRequestLE(ctx, &s)
	if err := p.applyVhost(ctx, s); err != nil {
		return errors.Join(append(leWarnings, err)...)
	}
	return errors.Join(leWarnings...)
}

// maybeRequestLE issues a Let's Encrypt certificate when the site turned LE
// on, changed its domain/subdomain (port of the request_certificates gate in
// update()). Warnings are returned for the datalog; the certificate files are
// what the vhost render then keys off.
func (p *Plugin) maybeRequestLE(ctx context.Context, s *site) []error {
	d, old := s.new, s.old
	if d.str("ssl") != "y" || d.str("ssl_letsencrypt") != "y" {
		return nil
	}
	changed := old.str("ssl") == "n" || old.str("ssl_letsencrypt") == "n" ||
		old.str("domain") != d.str("domain") || old.str("subdomain") != d.str("subdomain")
	if !changed {
		return nil
	}
	cfg := webLEConfig{
		signatureType: s.cfg.LeSignatureType,
		skipCheck:     s.cfg.SkipLeCheck == "y",
	}
	ok, err := p.requestCert(ctx, cfg, d)
	if err != nil {
		return []error{err}
	}
	if ok {
		s.sslChanged = true
	}
	return nil
}

// applyParent loads the active parent web_domain row and re-applies it as an
// update.
func (p *Plugin) applyParent(ctx context.Context, parentID int64) error {
	parent, err := p.loadDomainRow(parentID)
	if err != nil || parent == nil {
		return err
	}
	return p.applyWebDomain(ctx, "update", parent, parent)
}

// loadDomainRow fetches one active web_domain row as a map (nil when it does
// not exist or is inactive).
func (p *Plugin) loadDomainRow(domainID int64) (row, error) {
	var rec map[string]any
	err := p.db.Table("web_domain").
		Where("domain_id = ? AND active = 'y'", domainID).Take(&rec).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("nginx: loading web_domain %d: %w", domainID, err)
	}
	return rec, nil
}

// clientIDOf resolves the client id owning a sys_group (0 for admin-owned).
func (p *Plugin) clientIDOf(groupID int64) int64 {
	if groupID == 0 {
		return 0
	}
	var id int64
	_ = p.db.Table("sys_group").Where("groupid = ?", groupID).
		Pluck("client_id", &id).Error
	return id
}

// domainName returns the domain of a web_domain row ("" when missing).
func (p *Plugin) domainName(domainID int64) string {
	if domainID == 0 {
		return ""
	}
	var name string
	_ = p.db.Table("web_domain").Where("domain_id = ?", domainID).
		Pluck("domain", &name).Error
	return name
}
