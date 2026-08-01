package cmd

// migrateCmd will create the ISPConfig3-identical schema on an empty database
// (embedded ispconfig3.sql) or validate an existing one. Implemented by the
// database layer tasks.

import (
	"errors"

	"github.com/spf13/cobra"
)

var migrateCmd = &cobra.Command{
	Use:          "migrate",
	Short:        "Create or validate the database schema",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return errors.New("migrate: not implemented yet (openspec change port-ispconfig3-to-go, group 2)")
	},
}

func init() {
	rootCmd.AddCommand(migrateCmd)
}
