package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"go-ispconfig/internal/config"
	"go-ispconfig/internal/database"
)

// migrateCmd creates the ISPConfig3-identical schema on an empty database
// (embedded ispconfig3.sql, design D9) or validates an existing ISPConfig
// 3.3.x database for adoption without touching it.
var migrateCmd = &cobra.Command{
	Use:          "migrate",
	Short:        "Create or validate the database schema",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		db, err := database.Open(cfg.Database.DSN)
		if err != nil {
			return err
		}
		needSeed, err := database.Migrate(db)
		if err != nil {
			return err
		}
		if !needSeed {
			// The panel defaults still have to be filled when sys_ini is
			// empty: installs created before the panel seeded that row never
			// get another chance, and an empty dbname_prefix silently drops
			// the prefix from every client database. No-op when the row
			// already carries a configuration.
			filled, err := database.EnsureSystemConfig(db)
			if err != nil {
				return err
			}
			fmt.Println("Existing ISPConfig schema detected and validated; no DDL executed, no seed data written.")
			if filled {
				fmt.Println("sys_ini was empty and has been filled with the panel defaults " +
					"(database/FTP/shell prefixes, password policy).")
			}
			return nil
		}
		hostname, err := os.Hostname()
		if err != nil || hostname == "" {
			hostname = "server1"
		}
		if !strings.Contains(hostname, ".") {
			fmt.Fprintf(os.Stderr, "warning: hostname %q is not fully qualified (no domain part); the server row will be named %q\n", hostname, hostname)
		}
		// GOISP_ADMIN_PASSWORD allows unattended installs; when unset a
		// random password is generated.
		password, err := database.Seed(db, hostname, os.Getenv("GOISP_ADMIN_PASSWORD"))
		if err != nil {
			return err
		}
		fmt.Println("Schema created or completed from embedded ispconfig3.sql.")
		// The password goes to stderr so it does not end up in stdout
		// redirections or pipelines.
		fmt.Fprintf(os.Stderr, "Admin login: admin / %s\n", password)
		fmt.Fprintln(os.Stderr, "This password is shown only once; store it now or change it after first login.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(migrateCmd)
}
