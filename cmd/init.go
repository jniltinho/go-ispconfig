package cmd

// initCmd writes a default config.toml to the current directory from the
// config.toml.example embedded at build time. It never overwrites an
// existing file.

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var initOutput string

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Create a default configuration file in the current directory",
	Long: `Create a configuration file with default values in the current working directory.

If the target file already exists, the command fails; use -o to pick another name.

Examples:
  go-ispconfig init
  go-ispconfig init -o my-server-config.toml`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to determine current directory: %w", err)
		}

		targetFile := filepath.Join(cwd, initOutput)
		if _, err := os.Stat(targetFile); err == nil {
			return fmt.Errorf("file already exists: %s\nUse -o to specify a different filename", targetFile)
		}

		defaultConfig, err := fs.ReadFile(globalFS, "config.toml.example")
		if err != nil {
			return fmt.Errorf("failed to load embedded default configuration: %w", err)
		}

		if err := os.WriteFile(targetFile, defaultConfig, 0644); err != nil {
			return fmt.Errorf("failed to write configuration file: %w", err)
		}

		fmt.Printf("Created default configuration file: %s\n", targetFile)
		fmt.Println("Review the [server] and [database] sections, then run: go-ispconfig migrate && go-ispconfig serve")
		return nil
	},
}

func init() {
	initCmd.Flags().StringVarP(&initOutput, "output", "o", "config.toml", "name of the generated configuration file")
	rootCmd.AddCommand(initCmd)
}
