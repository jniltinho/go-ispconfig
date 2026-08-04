package installer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"go-ispconfig/internal/fail2ban"
)

// Fail2ban package and unit names, identical on all five supported distros.
const (
	// Fail2banPackage brings in fail2ban-server and fail2ban-client.
	Fail2banPackage = "fail2ban"
	// Fail2banService is the unit shipped by that package.
	Fail2banService = "fail2ban"
	// Fail2banSudoersFile is the drop-in inside State.SudoersDir.
	Fail2banSudoersFile = "go-ispconfig-fail2ban"
)

// fail2banSudoers lets the unprivileged panel user reach fail2ban's
// root-only control socket. The whitelist is exactly the three commands
// internal/fail2ban issues; the jail name and the address are validated
// (ValidJailName, net.ParseIP) before any command is built.
const fail2banSudoers = `# Managed by go-ispconfig — do not edit.
go-ispconfig ALL=(root) NOPASSWD: /usr/bin/fail2ban-client status
go-ispconfig ALL=(root) NOPASSWD: /usr/bin/fail2ban-client status *
go-ispconfig ALL=(root) NOPASSWD: /usr/bin/fail2ban-client set * unbanip *
`

// fail2banStep installs fail2ban and writes the panel-owned jail drop-ins.
// ISPConfig3's configure_fail2ban() is a stub, so there is nothing to port:
// the jail set comes from internal/fail2ban.
type fail2banStep struct{}

// Name identifies the step in the pipeline log.
func (fail2banStep) Name() string { return "fail2ban" }

// Run installs the package, renders /etc/fail2ban/jail.d/ispconfig-*.local
// and enables the unit; a re-run with unchanged jails only enables it.
func (fail2banStep) Run(ctx context.Context, st *State) error {
	installed, err := dpkgInstalled(ctx, st, Fail2banPackage)
	if err != nil {
		return err
	}
	if !installed {
		args := append(append([]string{}, aptOptions...), "install", "-y", "-q", Fail2banPackage)
		if _, err := st.Exec.Run(ctx, aptEnv, "apt-get", args...); err != nil {
			return fmt.Errorf("apt-get install %s: %w", Fail2banPackage, err)
		}
	}

	// The HTTP jail follows the web server actually installed; a node
	// without the web module gets no HTTP jail at all.
	webServer := ""
	if st.Answers.EnableWeb {
		webServer = st.Answers.WebServer
	}
	changed, err := fail2ban.Write(st.Fail2banJailDir, fail2ban.Jails(webServer))
	if err != nil {
		return err
	}
	if err := writeFail2banSudoers(ctx, st); err != nil {
		return err
	}
	if _, err := st.Exec.Run(ctx, nil, "systemctl", "enable", "--now", Fail2banService); err != nil {
		return fmt.Errorf("enabling %s: %w", Fail2banService, err)
	}
	if !changed && installed {
		return Skip("fail2ban jails already in place")
	}
	if _, err := st.Exec.Run(ctx, nil, "systemctl", "restart", Fail2banService); err != nil {
		return fmt.Errorf("restarting %s: %w", Fail2banService, err)
	}
	return nil
}

// writeFail2banSudoers installs the panel's sudo grant, validating it with
// visudo before it goes live: a malformed file in /etc/sudoers.d breaks
// sudo for the whole host. The staging name carries a dot, which sudo
// ignores, so a failure between write and rename can never take effect.
func writeFail2banSudoers(ctx context.Context, st *State) error {
	path := filepath.Join(st.SudoersDir, Fail2banSudoersFile)
	staging := path + ".new"
	if err := os.MkdirAll(st.SudoersDir, 0o750); err != nil {
		return fmt.Errorf("creating %s: %w", st.SudoersDir, err)
	}
	if err := os.WriteFile(staging, []byte(fail2banSudoers), 0o440); err != nil {
		return fmt.Errorf("writing %s: %w", staging, err)
	}
	defer func() { _ = os.Remove(staging) }()
	if out, err := st.Exec.Run(ctx, nil, "visudo", "-c", "-f", staging); err != nil {
		return fmt.Errorf("visudo -c %s: %w: %s", staging, err, out)
	}
	if err := os.Rename(staging, path); err != nil {
		return fmt.Errorf("installing %s: %w", path, err)
	}
	return nil
}
