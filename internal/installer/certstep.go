package installer

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"go-ispconfig/internal/tlscert"
)

// tlsCertStep pre-seeds the panel's self-signed certificate at the exact
// path serve manages itself (<ConfigDir>/ssl/panel.crt|key, foundation
// D13): CN/SAN = answered FQDN, 10-year validity, key 0600. A still-valid
// existing pair is kept; serve terminates TLS with these files directly —
// no nginx vhost fronts the panel (design D8).
type tlsCertStep struct{}

// Name identifies the step in the pipeline log.
func (tlsCertStep) Name() string { return "tls-cert" }

// Run generates the pair when missing/expired and hands the ssl dir to the
// panel user (serve rotates the pair on expiry, so it needs ownership).
func (tlsCertStep) Run(ctx context.Context, st *State) error {
	sslDir := filepath.Join(st.ConfigDir, "ssl")
	certFile := filepath.Join(sslDir, tlscert.CertFile)
	keyFile := filepath.Join(sslDir, tlscert.KeyFile)

	generated := false
	if err := tlscert.ValidateCertKey(certFile, keyFile); err != nil {
		if err := tlscert.WriteSelfSigned(certFile, keyFile, st.Answers.Hostname,
			time.Now().Add(-time.Hour), time.Now().AddDate(10, 0, 0)); err != nil {
			return err
		}
		generated = true
	}
	if _, err := st.Exec.Run(ctx, nil, "chown", "-R", PanelUser+":"+PanelUser, sslDir); err != nil {
		return fmt.Errorf("chown %s: %w", sslDir, err)
	}
	if !generated {
		return Skip("valid certificate already present")
	}
	return nil
}
