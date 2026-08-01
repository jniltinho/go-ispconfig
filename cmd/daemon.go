package cmd

// daemonCmd will run the persistent config-apply daemon: sys_datalog consumer
// plus the internal job scheduler. Implemented by the sys_datalog engine tasks.

import (
	"errors"

	"github.com/spf13/cobra"
)

var daemonCmd = &cobra.Command{
	Use:          "daemon",
	Short:        "Start the config-apply daemon (sys_datalog consumer)",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return errors.New("daemon: not implemented yet (openspec change port-ispconfig3-to-go, group 4)")
	},
}

func init() {
	rootCmd.AddCommand(daemonCmd)
}
