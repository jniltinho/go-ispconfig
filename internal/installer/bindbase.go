package installer

import (
	"context"
	"fmt"
	"os"
)

// bindBaseStep renders the bind base config (port of configure_bind, dns
// scope): named.conf.options from the embedded template and an existing
// named.conf.local for the dns module to manage (the distro named.conf
// already includes both). Writes are gated by named-checkconf with
// restore-on-failure; bind reloads only on change.
type bindBaseStep struct{}

// Name identifies the step in the pipeline log.
func (bindBaseStep) Name() string { return "bind-base" }

// Run converges the bind base files, validating before reload.
func (bindBaseStep) Run(ctx context.Context, st *State) error {
	if !st.Answers.EnableDNS {
		return Skip("dns server disabled by answer")
	}
	p := st.Profile

	changed, restore, err := writeFileBackup(p.NamedConfOptionsPath, []byte(namedConfOptions), 0o644)
	if err != nil {
		return err
	}
	// The dns module appends zones to named.conf.local; make sure it exists
	// (the bind9 package normally ships it).
	if _, err := os.Stat(p.NamedConfLocalPath); os.IsNotExist(err) {
		if err := os.WriteFile(p.NamedConfLocalPath, nil, 0o644); err != nil {
			return fmt.Errorf("creating %s: %w", p.NamedConfLocalPath, err)
		}
	}
	if !changed {
		return Skip("bind base config unchanged")
	}

	if _, err := st.Exec.Run(ctx, nil, "named-checkconf"); err != nil {
		if rerr := restore(); rerr != nil {
			return fmt.Errorf("named-checkconf failed (%v) and restoring %s failed too: %w", err, p.NamedConfOptionsPath, rerr)
		}
		return fmt.Errorf("named-checkconf rejected %s (previous config restored): %w", p.NamedConfOptionsPath, err)
	}
	if _, err := st.Exec.Run(ctx, nil, "systemctl", "reload", p.BindService); err != nil {
		return fmt.Errorf("reloading bind: %w", err)
	}
	return nil
}
