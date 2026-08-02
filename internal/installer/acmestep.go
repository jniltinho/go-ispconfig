package installer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// acmeStep optionally installs an ACME client for the web module's site
// Let's Encrypt certificates (design D8b, default off). The panel cert
// stays self-signed regardless. An already-present client (acme.sh or
// certbot — the same candidates internal/nginx detects) makes this a no-op.
type acmeStep struct{}

// Name identifies the step in the pipeline log.
func (acmeStep) Name() string { return "install-acme" }

// Run installs the chosen client when requested and absent.
func (acmeStep) Run(ctx context.Context, st *State) error {
	if !st.Answers.InstallAcme {
		return Skip("acme client install not requested")
	}
	if _, err := st.Exec.LookPath("acme.sh"); err == nil {
		return Skip("acme.sh already installed")
	}
	if _, err := os.Stat(filepath.Join(st.AcmeShHome, "acme.sh")); err == nil {
		return Skip("acme.sh already installed in " + st.AcmeShHome)
	}
	if _, err := st.Exec.LookPath("certbot"); err == nil {
		return Skip("certbot already installed")
	}

	if st.Answers.AcmeClient == "certbot" {
		args := append(append([]string{}, aptOptions...), "install", "-y", "-q", "certbot")
		if _, err := st.Exec.Run(ctx, aptEnv, "apt-get", args...); err != nil {
			return fmt.Errorf("installing certbot: %w", err)
		}
		return nil
	}

	// A minimal host may lack curl (the acme.sh installer needs it).
	if _, err := st.Exec.LookPath("curl"); err != nil {
		args := append(append([]string{}, aptOptions...), "install", "-y", "-q", "curl")
		if _, err := st.Exec.Run(ctx, aptEnv, "apt-get", args...); err != nil {
			return fmt.Errorf("installing curl for acme.sh: %w", err)
		}
	}
	install := "curl -fsSL https://get.acme.sh | sh -s"
	if st.Answers.AdminEmail != "" {
		// AdminEmail is validated by ResolveAnswers against a shell-inert
		// pattern; nothing else may be interpolated here.
		install += " email=" + st.Answers.AdminEmail
	}
	if _, err := st.Exec.Run(ctx, nil, "sh", "-c", install); err != nil {
		return fmt.Errorf("installing acme.sh: %w", err)
	}
	return nil
}
