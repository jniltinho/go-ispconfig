package cmd

// uninstall: removes the go-ispconfig units, rendered configs and (opt-in)
// the database — port of the install/uninstall.php scope (design D10).
// OS packages are never removed.

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"go-ispconfig/internal/installer"
)

var (
	uninstallYes        bool
	uninstallKeepConfig bool
	uninstallPurgeDB    bool
	uninstallDBRootPw   string
	uninstallDBName     string
	uninstallDBUser     string
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove go-ispconfig units and configs from this host",
	Long: `Stop, disable and remove the go-ispconfig systemd units, remove the
rendered nginx include and /etc/go-ispconfig (kept with --keep-config).
The database and DB user are dropped only with --purge-db. OS packages
(nginx, bind9, mariadb-server, ...) are never removed.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if os.Geteuid() != 0 {
			return fmt.Errorf("uninstall must run as root")
		}
		if !uninstallYes {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(),
				"This removes the go-ispconfig services and configuration from this host%s.\nType 'uninstall' to confirm: ",
				map[bool]string{true: " AND DROPS THE DATABASE", false: ""}[uninstallPurgeDB])
			line, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
			if strings.TrimSpace(line) != "uninstall" {
				return fmt.Errorf("aborted: confirmation not given")
			}
		}

		profile, err := installer.DetectDistro("/etc/os-release")
		if err != nil {
			return err
		}
		st := installer.NewState(profile, &installer.Answers{
			DBName:         uninstallDBName,
			DBUser:         uninstallDBUser,
			DBRootPassword: uninstallDBRootPw,
		})
		return installer.Uninstall(cmd.Context(), st, installer.UninstallOptions{
			KeepConfig: uninstallKeepConfig,
			PurgeDB:    uninstallPurgeDB,
		})
	},
}

func init() {
	f := uninstallCmd.Flags()
	f.BoolVarP(&uninstallYes, "yes", "y", false, "skip the typed confirmation")
	f.BoolVar(&uninstallKeepConfig, "keep-config", false, "preserve /etc/go-ispconfig (config.toml, ssl certs)")
	f.BoolVar(&uninstallPurgeDB, "purge-db", false, "also drop the ISPConfig database and DB user")
	f.StringVar(&uninstallDBRootPw, "db-root-password", "", "MariaDB root password (only when unix socket auth is unavailable)")
	f.StringVar(&uninstallDBName, "db-name", "dbispconfig", "ISPConfig database name")
	f.StringVar(&uninstallDBUser, "db-user", "ispconfig", "ISPConfig database user")
	rootCmd.AddCommand(uninstallCmd)
}
