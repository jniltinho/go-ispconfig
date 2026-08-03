package installer

import (
	"context"
	"fmt"

	"go-ispconfig/internal/fail2ban"
)

// Fail2ban package and unit names, identical on all five supported distros.
const (
	// Fail2banPackage brings in fail2ban-server and fail2ban-client.
	Fail2banPackage = "fail2ban"
	// Fail2banService is the unit shipped by that package.
	Fail2banService = "fail2ban"
)

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
