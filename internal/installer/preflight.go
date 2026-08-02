package installer

import (
	"context"
	"fmt"
	"os"
)

// preflightStep validates the host before anything is changed: root,
// systemd, apt, and no existing PHP ISPConfig3 install (which belongs to
// the add-legacy-migration change, not this installer).
type preflightStep struct{}

// Name identifies the step in the pipeline log.
func (preflightStep) Name() string { return "preflight" }

// Run performs the host checks; it never modifies the system.
func (preflightStep) Run(_ context.Context, st *State) error {
	if st.Euid != 0 {
		return fmt.Errorf("root is required to install (running as uid %d); use --dry-run to preview", st.Euid)
	}
	if _, err := os.Stat(st.SystemdMarker); err != nil {
		return fmt.Errorf("systemd not detected (%s missing): go-ispconfig requires a systemd host", st.SystemdMarker)
	}
	if _, err := st.Exec.LookPath("apt-get"); err != nil {
		return fmt.Errorf("apt-get not found: only Debian/Ubuntu hosts are supported")
	}
	if _, err := os.Stat(st.LegacyMarker); err == nil {
		return fmt.Errorf("existing PHP ISPConfig3 installation detected (%s): "+
			"this installer targets clean hosts — use the add-legacy-migration path to migrate it", st.LegacyMarker)
	}
	return nil
}
