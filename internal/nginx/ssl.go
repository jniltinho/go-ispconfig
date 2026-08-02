package nginx

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go-ispconfig/internal/engine"
)

// sslSelfSignedDays is the validity of a generated self-signed certificate
// (PHP parity: 3650 days).
const sslSelfSignedDays = "3650"

// ssl is the certificate handler (port of ssl()): it runs before the
// insert/update/delete handler of the same web_domain event and applies the
// ssl_action (create / save / del). Only vhost-type records have a cert. DB
// writes here are deliberate plain updates that generate NO datalog row
// (PHP parity: the interface must not re-trigger on the server's own write).
func (p *Plugin) ssl(ctx context.Context, _ string, data engine.Data) error {
	d := row(data.New)
	if !isVhostType(d.str("type")) {
		return nil
	}
	action := d.str("ssl_action")
	if action == "" {
		return nil
	}

	docroot := d.str("document_root")
	old := row(data.Old)
	if docroot == "" {
		docroot = old.str("document_root")
	}
	if docroot == "" {
		return nil
	}
	sslDir := filepath.Join(docroot, "ssl")
	domain := d.str("ssl_domain")
	if domain == "" {
		domain = d.str("domain")
	}
	if err := safeDomain(strings.TrimPrefix(domain, "*.")); err != nil {
		return fmt.Errorf("nginx: ssl for unsafe domain: %w", err)
	}
	key := filepath.Join(sslDir, domain+".key")
	csr := filepath.Join(sslDir, domain+".csr")
	crt := filepath.Join(sslDir, domain+".crt")

	switch action {
	case "create":
		return p.sslCreate(ctx, d, sslDir, domain, key, csr, crt)
	case "save":
		return p.sslSave(ctx, d, key, csr, crt)
	case "del":
		return p.sslDelete(ctx, d, csr, crt)
	}
	return nil
}

// sslCreate generates a self-signed key/CSR/cert and stores them back on the
// row without a datalog write (port of ssl_action=create).
func (p *Plugin) sslCreate(ctx context.Context, d row, sslDir, domain, key, csr, crt string) error {
	if err := os.MkdirAll(sslDir, 0o755); err != nil {
		return fmt.Errorf("nginx: creating %s: %w", sslDir, err)
	}
	// Back up existing files (PHP renames to .bak before regenerating).
	for _, f := range []string{key, csr, crt} {
		if _, err := os.Stat(f); err == nil {
			_ = os.Rename(f, f+".bak")
		}
	}
	subj := opensslSubject(d, domain)
	// ponytail: -subj instead of the PHP openssl.conf file — argv-only, no
	// shell, empty DN fields simply omitted. Add a config file if a CA-signed
	// path (web_config CA_path) is ever ported.
	if out, err := p.runner.Run(ctx, "openssl", "req", "-nodes", "-newkey", "rsa:4096",
		"-x509", "-days", sslSelfSignedDays, "-keyout", key, "-out", crt,
		"-sha256", "-subj", subj); err != nil {
		return fmt.Errorf("nginx: openssl self-signed cert for %s: %w: %s", domain, err, out)
	}
	if out, err := p.runner.Run(ctx, "openssl", "req", "-new", "-sha256",
		"-key", key, "-out", csr, "-subj", subj); err != nil {
		return fmt.Errorf("nginx: openssl csr for %s: %w: %s", domain, err, out)
	}
	if err := os.Chmod(key, 0o400); err != nil {
		return fmt.Errorf("nginx: chmod key %s: %w", key, err)
	}
	p.sslChangedDomain = d.str("domain")
	return p.storeCert(d.str("domain"), csr, crt, key)
}

// sslSave persists a pasted certificate, rejecting an encrypted key and a
// cert containing a .acme.invalid domain (port of ssl_action=save).
func (p *Plugin) sslSave(ctx context.Context, d row, key, csr, crt string) error {
	if strings.Contains(d.str("ssl_key"), "Proc-Type: 4,ENCRYPTED") {
		p.clearSSLAction(d.str("domain"))
		return fmt.Errorf("nginx: SSL key for %s is encrypted, not saved", d.str("domain"))
	}
	// Reject a certificate that is actually a Let's Encrypt challenge cert.
	if cert := d.str("ssl_cert"); cert != "" {
		if strings.Contains(strings.ToLower(cert), ".acme.invalid") {
			p.clearSSLAction(d.str("domain"))
			return fmt.Errorf("nginx: SSL cert for %s contains .acme.invalid, not saved", d.str("domain"))
		}
	}

	// Back up the current files to ~ so the vhost activation can restore them
	// if nginx -t fails after this cert change (key backup stays 0400).
	for _, f := range []string{key, csr, crt} {
		if _, err := os.Stat(f); err == nil {
			mode := os.FileMode(0o644)
			if f == key {
				mode = 0o400
			}
			_ = copyFileMode(f, f+"~", mode)
		}
	}
	p.sslChangedDomain = d.str("domain")

	if v := strings.TrimSpace(d.str("ssl_request")); v != "" {
		if err := os.WriteFile(csr, []byte(d.str("ssl_request")), 0o644); err != nil {
			return fmt.Errorf("nginx: writing csr: %w", err)
		}
	}
	if v := strings.TrimSpace(d.str("ssl_cert")); v != "" {
		content := v
		if bundle := strings.TrimSpace(d.str("ssl_bundle")); bundle != "" {
			content += "\n" + bundle
		}
		if err := os.WriteFile(crt, []byte(content+"\n"), 0o644); err != nil {
			return fmt.Errorf("nginx: writing crt: %w", err)
		}
	}
	if v := strings.TrimSpace(d.str("ssl_key")); v != "" {
		// The previous key is 0400 (unwritable even for the owner); drop it
		// first — the ~ backup above preserved it.
		if err := removeFileIfExists(key); err != nil {
			return err
		}
		if err := os.WriteFile(key, []byte(d.str("ssl_key")), 0o400); err != nil {
			return fmt.Errorf("nginx: writing key: %w", err)
		}
	}
	p.clearSSLAction(d.str("domain"))
	return nil
}

// sslDelete removes the CSR/cert files and clears the DB fields (port of
// ssl_action=del).
func (p *Plugin) sslDelete(_ context.Context, d row, csr, crt string) error {
	for _, f := range []string{csr, crt} {
		if err := removeFileIfExists(f); err != nil {
			return err
		}
	}
	if p.db != nil {
		err := p.db.Table("web_domain").
			Where("domain = ? AND server_id = ?", d.str("domain"), d.num("server_id")).
			Updates(map[string]any{"ssl_request": "", "ssl_cert": "", "ssl_action": ""}).Error
		if err != nil {
			return fmt.Errorf("nginx: clearing ssl fields: %w", err)
		}
	}
	return nil
}

// opensslSubject builds the certificate subject DN, omitting empty fields.
func opensslSubject(d row, cn string) string {
	var b strings.Builder
	add := func(k, v string) {
		if v = strings.TrimSpace(v); v != "" {
			// '/' and '=' would break the DN; drop any that sneak in.
			v = strings.NewReplacer("/", "", "=", "").Replace(v)
			fmt.Fprintf(&b, "/%s=%s", k, v)
		}
	}
	add("C", d.str("ssl_country"))
	add("ST", d.str("ssl_state"))
	add("L", d.str("ssl_locality"))
	add("O", d.str("ssl_organisation"))
	add("OU", d.str("ssl_organisation_unit"))
	b.WriteString("/CN=" + cn)
	add("emailAddress", "webmaster@"+d.str("domain"))
	return b.String()
}

// storeCert writes the generated CSR/cert/key back to the row without a
// datalog entry and clears ssl_action.
func (p *Plugin) storeCert(domain, csr, crt, key string) error {
	if p.db == nil {
		return nil
	}
	req, _ := os.ReadFile(csr)
	cert, _ := os.ReadFile(crt)
	k, _ := os.ReadFile(key)
	err := p.db.Table("web_domain").Where("domain = ?", domain).
		Updates(map[string]any{
			"ssl_request": string(req), "ssl_cert": string(cert),
			"ssl_key": string(k), "ssl_action": "",
		}).Error
	if err != nil {
		return fmt.Errorf("nginx: storing generated cert for %s: %w", domain, err)
	}
	return nil
}

// clearSSLAction resets ssl_action so a rejected save is not retried forever.
func (p *Plugin) clearSSLAction(domain string) {
	if p.db == nil {
		return
	}
	_ = p.db.Table("web_domain").Where("domain = ?", domain).
		Update("ssl_action", "").Error
}
