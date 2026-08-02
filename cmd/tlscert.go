package cmd

// TLS certificate resolution for the standalone HTTPS panel (design D13):
// serve terminates TLS itself. An explicitly configured cert/key pair is
// used as-is (and never touched on disk); without one a 10-year self-signed
// certificate is generated into <config dir>/ssl/ and reused across starts,
// regenerated only when missing, unreadable or expired. Generation and
// validation live in internal/tlscert, shared with the installer's
// tls-cert step (which pre-seeds the same files).

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/viper"

	"go-ispconfig/internal/config"
	"go-ispconfig/internal/tlscert"
)

// configDir returns the directory holding the loaded config.toml, or "."
// when serve runs on defaults without any config file. The auto-generated
// ssl/ directory lives next to the config file (design D13).
func configDir() string {
	if f := viper.ConfigFileUsed(); f != "" {
		return filepath.Dir(f)
	}
	return "."
}

// resolveTLS decides how serve terminates TLS. It returns the cert/key file
// pair to serve HTTPS with, or empty strings for plain HTTP (server.https =
// false). A configured pair is validated but never modified: an invalid or
// expired explicit certificate is a startup error. Without a configured
// pair, a 10-year self-signed certificate under dir/ssl/ is reused when
// still valid and (re)generated otherwise.
func resolveTLS(srv config.ServerConfig, dir string) (certFile, keyFile string, err error) {
	if !srv.HTTPS {
		return "", "", nil
	}
	if (srv.TLSCert == "") != (srv.TLSKey == "") {
		return "", "", fmt.Errorf("config: server.tls_cert and server.tls_key must both be set (got cert=%q key=%q)",
			srv.TLSCert, srv.TLSKey)
	}
	if srv.TLSCert != "" {
		if err := tlscert.ValidateCertKey(srv.TLSCert, srv.TLSKey); err != nil {
			return "", "", fmt.Errorf("configured server.tls_cert/tls_key are invalid: %w; "+
				"fix or remove them from the config — go-ispconfig never overwrites an explicitly configured certificate", err)
		}
		return srv.TLSCert, srv.TLSKey, nil
	}

	certFile = filepath.Join(dir, "ssl", tlscert.CertFile)
	keyFile = filepath.Join(dir, "ssl", tlscert.KeyFile)
	if err := tlscert.ValidateCertKey(certFile, keyFile); err == nil {
		// Re-assert the private key mode on reuse: a key loosened by an
		// operator (or created by an older build) must not stay readable.
		if err := os.Chmod(keyFile, 0o600); err != nil {
			return "", "", fmt.Errorf("restricting permissions of %s: %w", keyFile, err)
		}
		slog.Info("reusing self-signed certificate", "cert", certFile)
		return certFile, keyFile, nil
	} else if !os.IsNotExist(err) {
		slog.Warn("self-signed certificate invalid, regenerating", "cert", certFile, "error", err)
	}
	if err := tlscert.WriteSelfSigned(certFile, keyFile, "", time.Now().Add(-time.Hour), time.Now().AddDate(10, 0, 0)); err != nil {
		return "", "", err
	}
	slog.Info("generated 10-year self-signed certificate", "cert", certFile)
	return certFile, keyFile, nil
}
