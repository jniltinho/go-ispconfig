package installer

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// summaryStep prints the final install summary. The generated admin
// password (set only when the seed ran, i.e. first install) is printed
// exactly once; with --write-credentials it is additionally persisted
// root-only to CredentialsFile. Re-runs never regenerate or reprint it.
type summaryStep struct{}

// Name identifies the step in the pipeline log.
func (summaryStep) Name() string { return "summary" }

// Run assembles and prints the summary, optionally persisting it.
func (summaryStep) Run(_ context.Context, st *State) error {
	lines := []string{
		"",
		"go-ispconfig installation complete.",
		fmt.Sprintf("  Panel URL: https://%s:%d/ (self-signed certificate)", st.Answers.Hostname, st.Answers.PanelPort),
	}
	if st.AdminPassword != "" {
		lines = append(lines,
			"  Admin login: admin / "+st.AdminPassword,
			"  This password is shown only once; store it now or change it after first login.")
	} else {
		lines = append(lines, "  Admin credentials: unchanged (already provisioned on a previous run).")
	}
	summary := strings.Join(lines, "\n")

	if st.WriteCredentials && st.AdminPassword != "" {
		if err := os.WriteFile(st.CredentialsFile, []byte(summary+"\n"), 0o600); err != nil {
			return fmt.Errorf("writing %s: %w", st.CredentialsFile, err)
		}
		// WriteFile does not chmod an existing file; re-assert 0600.
		if err := os.Chmod(st.CredentialsFile, 0o600); err != nil {
			return fmt.Errorf("chmod %s: %w", st.CredentialsFile, err)
		}
		summary += "\n  Credentials written to " + st.CredentialsFile + " — delete this file after storing them."
	}
	st.logf("%s", summary)
	return nil
}
