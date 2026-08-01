// Package cmd defines the Cobra CLI commands for go-ispconfig
// (init, serve, daemon, migrate, version).
// Configuration is loaded from config.toml via Viper and can be overridden
// with GOISP_* environment variables.
package cmd

import (
	"embed"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cfgFile string
	// Version is injected at build time via -ldflags "-X go-ispconfig/cmd.Version=x.y.z".
	Version = "dev"
	// BuildDate is injected at build time.
	BuildDate = "unknown"
	// GitCommit is injected at build time.
	GitCommit = "unknown"
)

var rootCmd = &cobra.Command{
	Use:   "go-ispconfig",
	Short: "go-ispconfig — hosting control panel (ISPConfig3 port in Go)",
	Long: `go-ispconfig is a hosting control panel written in Go, a port of ISPConfig3
managing nginx (web) and Bind (DNS) with a database schema identical to ISPConfig3.

Available commands:
  init      Create a default configuration file
  serve     Start the panel web server (API + embedded SPA)
  daemon    Start the config-apply daemon (sys_datalog consumer)
  migrate   Create or validate the database schema
  version   Show version information`,
}

var globalFS embed.FS

// Execute sets the embedded filesystem and runs the root Cobra command.
// It is called from main() with the compiled-in web/dist filesystem.
func Execute(fs embed.FS) {
	globalFS = fs
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "path to config.toml (default: ./config.toml)")
}

// initConfig loads configuration from disk and environment variables via Viper.
// Search order: --config flag → ./config.toml → /etc/go-ispconfig/config.toml.
// Environment variables prefixed with GOISP_ override any file values.
func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		viper.SetConfigName("config")
		viper.SetConfigType("toml")
		viper.AddConfigPath(".")
		viper.AddConfigPath("/etc/go-ispconfig/")
	}

	viper.SetEnvPrefix("GOISP")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			fmt.Fprintf(os.Stderr, "error reading config: %v\n", err)
		}
	}
}
