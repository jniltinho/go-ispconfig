package installer

import (
	"context"
	"fmt"
	"os"

	"go-ispconfig/internal/getconf"
)

// GetmailPackage is the current getmail (Python 3) package; Debian 11 and
// older only ship the pre-fork "getmail" name.
const (
	// GetmailPackage is the preferred package name.
	GetmailPackage = "getmail6"
	// GetmailLegacyPackage is the fallback on distros without getmail6.
	GetmailLegacyPackage = "getmail"
	// GetmailUser is the unprivileged owner of the rc files and the
	// identity the daemon's getmail_fetch job drops to.
	GetmailUser = "getmail"
)

// getmailStep is the Go port of installer_base.lib.php::configure_getmail:
// package, system user and the 0700 config directory owned by it. The PHP
// run-getmail.sh script and the "*/5 * * * *" crontab are deliberately not
// ported — the daemon scheduler owns all periodic work (foundation D1b).
type getmailStep struct{}

// Name identifies the step in the pipeline log.
func (getmailStep) Name() string { return "getmail" }

// Run provisions getmail on mail servers only; it is skipped before the
// database step ran or when this host has no mail role.
func (getmailStep) Run(ctx context.Context, st *State) error {
	serverID, _, err := mailRole(st)
	if err != nil {
		return err
	}
	dir := getconf.DefaultGetmailConfig().ConfigDir
	if cfg, err := getconf.GetServerConfig(st.DB, serverID); err == nil && cfg.Getmail.ConfigDir != "" {
		dir = cfg.Getmail.ConfigDir
	}

	if err := installGetmailPackage(ctx, st); err != nil {
		return err
	}
	// Home = the rc directory, exactly as configure_getmail does.
	if _, err := st.Exec.Run(ctx, nil, "id", "-u", GetmailUser); err != nil {
		if _, err := st.Exec.Run(ctx, nil, "useradd", "--system", "--user-group",
			"--home-dir", dir, "--no-create-home",
			"--shell", "/usr/sbin/nologin", GetmailUser); err != nil {
			return fmt.Errorf("creating user %s: %w", GetmailUser, err)
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	// The rc files hold cleartext mailbox passwords: the directory must be
	// private to the getmail user even if a file mode is ever wrong.
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("chmod %s: %w", dir, err)
	}
	if _, err := st.Exec.Run(ctx, nil, "chown", GetmailUser+":"+GetmailUser, dir); err != nil {
		return fmt.Errorf("chown %s: %w", dir, err)
	}
	return nil
}

// installGetmailPackage installs getmail6, falling back to the legacy
// package name on distros that do not carry it.
func installGetmailPackage(ctx context.Context, st *State) error {
	pkg := GetmailPackage
	installed, err := dpkgInstalled(ctx, st, pkg)
	if err != nil {
		return err
	}
	if installed {
		return nil
	}
	if _, err := st.Exec.Run(ctx, aptEnv, "apt-cache", "show", pkg); err != nil {
		pkg = GetmailLegacyPackage
		if installed, err := dpkgInstalled(ctx, st, pkg); err != nil || installed {
			return err
		}
	}
	args := append(append([]string{}, aptOptions...), "install", "-y", "-q", pkg)
	if _, err := st.Exec.Run(ctx, aptEnv, "apt-get", args...); err != nil {
		return fmt.Errorf("installing %s: %w", pkg, err)
	}
	return nil
}
