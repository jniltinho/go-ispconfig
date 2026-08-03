package cmd

// install: native Go port of the ISPConfig3 install/ layer for the
// nginx+bind scope (openspec change add-installer-cli). The command wires
// CLI flags into the answers resolution (flags > answers file > prompt >
// default) and runs the idempotent step pipeline; --update re-renders only
// base configs and units (design D9).

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"go-ispconfig/internal/installer"
)

var (
	installYes      bool
	installUpdate   bool
	installDryRun   bool
	installAnswers  string
	installWriteCrd bool
)

// answerFlagNames are the install flags that feed the answers resolution
// (flag name == answers key).
var answerFlagNames = map[string]bool{
	"hostname": true, "panel-port": true, "db-name": true, "db-user": true,
	"db-root-password": true, "web": true, "web-server": true, "dns": true, "dns-backend": true, "php-fpm": true,
	"acme": true, "acme-client": true, "admin-email": true,
}

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install and configure go-ispconfig on this host (nginx + bind)",
	Long: `Install go-ispconfig on a clean Debian 11-13 / Ubuntu 22.04-24.04 host:
OS packages (nginx, bind9, mariadb-server, redis-server, optional php-fpm),
database and schema (identical to ISPConfig3), base service configs, systemd
units and a self-signed panel certificate.

Answers are resolved as: CLI flags > --answers file.toml > interactive
prompt > default. With --yes nothing is prompted. Every step is idempotent:
re-running after a failure converges to a complete installation.

--update re-renders base configs and systemd units only, never touching the
database, credentials, certificates or the admin user.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !installDryRun && os.Geteuid() != 0 {
			return fmt.Errorf("install must run as root (use --dry-run to preview as any user)")
		}

		flags := map[string]string{}
		cmd.Flags().Visit(func(f *pflag.Flag) {
			if answerFlagNames[f.Name] {
				flags[f.Name] = f.Value.String()
			}
		})
		answers, err := installer.ResolveAnswers(installer.ResolveOptions{
			Flags:       flags,
			AnswersFile: installAnswers,
			Interactive: !installYes && !installDryRun,
			In:          cmd.InOrStdin(),
			Out:         cmd.OutOrStdout(),
		})
		if err != nil {
			return err
		}
		profile, err := installer.DetectDistro("/etc/os-release")
		if err != nil {
			return err
		}

		steps := installer.InstallSteps()
		if installUpdate {
			steps = installer.UpdateSteps()
		}
		if installDryRun {
			names := make([]string, len(steps))
			for i, s := range steps {
				names[i] = s.Name()
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(),
				"Dry run — no changes made.\nDistro: %s (%s)\nHostname: %s  Panel port: %d  web=%v dns=%v (%s) php-fpm=%v acme=%v\nSteps: %s\n",
				profile.Name, profile.ID, answers.Hostname, answers.PanelPort,
				answers.EnableWeb, answers.EnableDNS, answers.DNSBackend,
				answers.InstallPHPFPM, answers.InstallAcme,
				strings.Join(names, " → "))
			return nil
		}

		st := installer.NewState(profile, answers)
		st.Update = installUpdate
		st.WriteCredentials = installWriteCrd
		return installer.Run(cmd.Context(), st, steps)
	},
}

func init() {
	f := installCmd.Flags()
	f.BoolVarP(&installYes, "yes", "y", false, "non-interactive: never prompt, abort on missing required answers")
	f.BoolVar(&installUpdate, "update", false, "re-render base configs and units only (keeps DB, credentials, certs)")
	f.BoolVar(&installDryRun, "dry-run", false, "resolve answers and print the plan without changing anything")
	f.StringVar(&installAnswers, "answers", "", "TOML answers file (flags override it)")
	f.BoolVar(&installWriteCrd, "write-credentials", false, "also write the credentials summary to /root/.go-ispconfig-credentials (0600)")

	f.String("hostname", "", "server FQDN (default: system hostname)")
	f.Int("panel-port", 8080, "panel HTTPS port")
	f.String("db-name", "dbispconfig", "ISPConfig database name")
	f.String("db-user", "ispconfig", "ISPConfig database user")
	f.String("db-root-password", "", "MariaDB root password (only when unix socket auth is unavailable)")
	f.String("web", "y", "configure the web server [y/n]")
	f.String("web-server", "nginx", "web server to configure: nginx or apache2")
	f.String("dns", "y", "configure the DNS server [y/n]")
	f.String("dns-backend", "bind", "DNS backend to configure: bind or powerdns")
	f.String("php-fpm", "y", "install the distro php-fpm package for hosted sites [y/n]")
	f.String("acme", "n", "install an ACME client for site Let's Encrypt certificates [y/n]")
	f.String("acme-client", "acme.sh", "ACME client to install: acme.sh or certbot")
	f.String("admin-email", "", "administrator email address")

	rootCmd.AddCommand(installCmd)
}
