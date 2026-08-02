package installer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// UninstallOptions control what `go-ispconfig uninstall` removes.
type UninstallOptions struct {
	// KeepConfig preserves /etc/go-ispconfig (config.toml, ssl/).
	KeepConfig bool
	// PurgeDB drops the ISPConfig database and DB user.
	PurgeDB bool
}

// Uninstall stops/disables/removes the systemd units, removes the
// go-ispconfig-rendered nginx include and (unless KeepConfig) the config
// directory; with PurgeDB it drops the database and user. OS packages are
// never removed (apt state belongs to the admin). The bind
// named.conf.options we rendered stays in place: it is a valid bind config
// and removing it would break the service.
func Uninstall(ctx context.Context, st *State, opts UninstallOptions) error {
	// Stop and disable units; a missing unit is not an error on re-run.
	if _, err := st.Exec.Run(ctx, nil, "systemctl", "disable", "--now", ServeUnitName, DaemonUnitName); err != nil {
		st.logf("warning: disabling units: %v", err)
	}
	for _, name := range []string{ServeUnitName, DaemonUnitName} {
		if err := removeIfExists(filepath.Join(st.SystemdDir, name)); err != nil {
			return err
		}
	}
	if err := removeIfExists(st.BinPath); err != nil {
		return err
	}
	if _, err := st.Exec.Run(ctx, nil, "systemctl", "daemon-reload"); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %w", err)
	}

	// Rendered nginx include (only written on hosts whose nginx.conf lost
	// the sites-enabled include).
	include := filepath.Join(st.Profile.NginxConfigDir, "conf.d", "go-ispconfig-sites.conf")
	if _, err := os.Stat(include); err == nil {
		if err := os.Remove(include); err != nil {
			return fmt.Errorf("removing %s: %w", include, err)
		}
		if _, err := st.Exec.Run(ctx, nil, "systemctl", "reload", st.Profile.NginxService); err != nil {
			st.logf("warning: reloading nginx: %v", err)
		}
	}

	if !opts.KeepConfig {
		if err := os.RemoveAll(st.ConfigDir); err != nil {
			return fmt.Errorf("removing %s: %w", st.ConfigDir, err)
		}
	}

	if opts.PurgeDB {
		// The uninstall command builds Answers directly (no ResolveAnswers
		// validation), so re-check the identifiers used in DROP statements.
		if !identRe.MatchString(st.Answers.DBName) || !identRe.MatchString(st.Answers.DBUser) {
			return fmt.Errorf("invalid database name/user %q/%q: only letters, digits and _ are allowed",
				st.Answers.DBName, st.Answers.DBUser)
		}
		root, err := connectRoot(st)
		if err != nil {
			return err
		}
		defer closeDB(root)
		stmts := []string{fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", st.Answers.DBName)}
		for _, host := range st.DBUserHosts {
			stmts = append(stmts, fmt.Sprintf("DROP USER IF EXISTS '%s'@'%s'", st.Answers.DBUser, host))
		}
		for _, s := range stmts {
			if err := root.Exec(s).Error; err != nil {
				return fmt.Errorf("purging database: %w", err)
			}
		}
	}

	st.logf("go-ispconfig uninstalled (packages were not removed; use apt for that).")
	return nil
}

// removeIfExists removes path, tolerating its absence.
func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing %s: %w", path, err)
	}
	return nil
}
