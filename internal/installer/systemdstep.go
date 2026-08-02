package installer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"go-ispconfig/init/systemd"
)

// Unit file names installed to /etc/systemd/system.
const (
	ServeUnitName  = "go-ispconfig-serve.service"
	DaemonUnitName = "go-ispconfig-daemon.service"
)

// systemdStep installs the go-ispconfig binary to BinPath (the units'
// ExecStart), writes the embedded unit files and enables both units.
// No crontab is ever created or touched (foundation D1b): the daemon's
// internal scheduler owns all periodic work. In --update mode units are
// re-rendered and restarted instead of enable --now.
type systemdStep struct{}

// Name identifies the step in the pipeline log.
func (systemdStep) Name() string { return "systemd-units" }

// Run converges binary, unit files and unit state.
func (systemdStep) Run(ctx context.Context, st *State) error {
	if err := installBinary(st); err != nil {
		return err
	}

	changed := false
	for name, content := range map[string]string{
		ServeUnitName:  systemd.ServeUnit,
		DaemonUnitName: systemd.DaemonUnit,
	} {
		c, _, err := writeFileBackup(filepath.Join(st.SystemdDir, name), []byte(content), 0o644)
		if err != nil {
			return err
		}
		changed = changed || c
	}
	if changed {
		if _, err := st.Exec.Run(ctx, nil, "systemctl", "daemon-reload"); err != nil {
			return fmt.Errorf("systemctl daemon-reload: %w", err)
		}
	}

	if st.Update {
		if _, err := st.Exec.Run(ctx, nil, "systemctl", "restart", ServeUnitName, DaemonUnitName); err != nil {
			return fmt.Errorf("restarting units: %w", err)
		}
		return nil
	}
	if _, err := st.Exec.Run(ctx, nil, "systemctl", "enable", "--now", ServeUnitName, DaemonUnitName); err != nil {
		return fmt.Errorf("enabling units: %w", err)
	}
	return nil
}

// installBinary copies the running executable to BinPath when it is not
// already running from there (the units ExecStart from BinPath).
func installBinary(st *State) error {
	if st.SelfExe == "" || st.SelfExe == st.BinPath {
		return nil
	}
	content, err := os.ReadFile(st.SelfExe)
	if err != nil {
		return fmt.Errorf("reading own binary: %w", err)
	}
	existing, err := os.ReadFile(st.BinPath)
	if err == nil && string(existing) == string(content) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(st.BinPath), 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(st.BinPath), err)
	}
	// Write to a temp name then rename: the running daemon's binary must
	// never be truncated in place.
	tmp := st.BinPath + ".new"
	if err := os.WriteFile(tmp, content, 0o755); err != nil {
		return fmt.Errorf("writing %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, st.BinPath); err != nil {
		return fmt.Errorf("installing %s: %w", st.BinPath, err)
	}
	return nil
}
