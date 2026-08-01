package cmd

import (
	"fmt"
	"os"

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
		created, err := database.Migrate(db)
		if err != nil {
			return err
		}
		if !created {
			fmt.Println("Existing ISPConfig schema detected and validated; no DDL executed, no seed data written.")
			return nil
		}
		hostname, err := os.Hostname()
		if err != nil || hostname == "" {
			hostname = "server1"
		}
		password, err := database.Seed(db, hostname)
		if err != nil {
			return err
		}
		fmt.Println("Schema created from embedded ispconfig3.sql.")
		fmt.Printf("Admin login: admin / %s\n", password)
		fmt.Println("This password is shown only once; store it now or change it after first login.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(migrateCmd)
}
