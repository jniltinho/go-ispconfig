package apache2

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

// sslPaths returns the certificate, key and chain-bundle paths of a site.
// Same layout as the nginx plugin (<document_root>/ssl/<ssl_domain>.*) so a
// server switched between web servers keeps serving the same files, and so
// the shared Let's Encrypt code finds the "-le" variants where it left them.
func sslPaths(d row) (crt, key, bundle string) {
	sslDomain := d.str("ssl_domain")
	if sslDomain == "" {
		sslDomain = d.str("domain")
	}
	dir := filepath.Join(strings.TrimSuffix(d.str("document_root"), "/"), "ssl")
	crt = filepath.Join(dir, sslDomain+".crt")
	key = filepath.Join(dir, sslDomain+".key")
	bundle = filepath.Join(dir, sslDomain+".bundle")
	// Let's Encrypt certificates land next to them with a "-le" infix and
	// take precedence when present (port of get_website_certificate_paths).
	leCrt := filepath.Join(dir, sslDomain+"-le.crt")
	if fileNonEmpty(leCrt) {
		crt = leCrt
		key = filepath.Join(dir, sslDomain+"-le.key")
		bundle = filepath.Join(dir, sslDomain+"-le.bundle")
	}
	return crt, key, bundle
}

// fileNonEmpty reports whether path is a regular file with content. An empty
// certificate file is what a failed issuance leaves behind, and Apache
// refuses to start on one, so it must never be referenced.
func fileNonEmpty(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().IsRegular() && fi.Size() > 0
}

// sslUsable reports whether both the certificate and the key exist non-empty.
// The vhost may only enable SSLEngine when they do.
func sslUsable(d row) bool {
	crt, key, _ := sslPaths(d)
	return fileNonEmpty(crt) && fileNonEmpty(key)
}

// probeOCSP reports whether the certificate carries an OCSP responder URI.
// SSLUseStapling against a certificate without one makes Apache log an error
// on every handshake, so the template branch is gated on this probe.
func (p *Plugin) probeOCSP(ctx context.Context, crt string) bool {
	if !fileNonEmpty(crt) {
		return false
	}
	out, err := p.runner.Run(ctx, "openssl", "x509", "-noout", "-ocsp_uri", "-in", crt)
	return err == nil && strings.Contains(string(out), "http")
}
