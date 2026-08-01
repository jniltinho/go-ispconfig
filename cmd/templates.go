package cmd

// templatesCmd manages the ".master" template override directory (design
// D6b, conf-custom parity): list shows every embedded template and marks
// custom overrides, export copies embedded originals into the custom dir
// for the operator to edit.

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"go-ispconfig/internal/config"
	"go-ispconfig/internal/mastertpl"
)

var (
	templatesDir   string
	templatesAll   bool
	templatesForce bool
)

var templatesCmd = &cobra.Command{
	Use:   "templates",
	Short: "Manage .master configuration template overrides",
	Long: `Manage the ".master" configuration templates rendered by the daemon.

A file in the custom directory ([templates] custom_dir in config.toml,
default /etc/go-ispconfig/templates-custom) with the same name as an
embedded template overrides it — the equivalent of ISPConfig3's
server/conf-custom/ directory.`,
}

var templatesListCmd = &cobra.Command{
	Use:          "list",
	Short:        "List embedded templates, marking custom overrides",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, err := templatesCustomDir()
		if err != nil {
			return err
		}
		return listTemplates(cmd.OutOrStdout(), dir)
	},
}

var templatesExportCmd = &cobra.Command{
	Use:          "export <name>... | --all",
	Short:        "Export embedded template originals into the custom directory",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 && !templatesAll {
			return errors.New("specify template names or --all")
		}
		if templatesAll {
			args = mastertpl.Names()
		}
		dir, err := templatesCustomDir()
		if err != nil {
			return err
		}
		return exportTemplates(cmd.OutOrStdout(), dir, args, templatesForce)
	},
}

// templatesCustomDir resolves the custom directory: the --dir flag wins,
// otherwise the [templates] custom_dir config value is used.
func templatesCustomDir() (string, error) {
	if templatesDir != "" {
		return templatesDir, nil
	}
	cfg, err := config.Load()
	if err != nil {
		return "", err
	}
	return cfg.Templates.CustomDir, nil
}

// listTemplates writes one line per embedded template, appending
// "(overridden)" when a custom file of the same name exists in dir.
func listTemplates(w io.Writer, dir string) error {
	for _, name := range mastertpl.Names() {
		marker := ""
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			marker = " (overridden)"
		}
		if _, err := fmt.Fprintf(w, "%s%s\n", name, marker); err != nil {
			return err
		}
	}
	return nil
}

// exportTemplates writes the embedded originals of the given templates into
// dir. An existing file is never overwritten unless force is set; the
// existence check runs before anything is written so a refusal leaves the
// directory untouched.
func exportTemplates(w io.Writer, dir string, names []string, force bool) error {
	embedded := mastertpl.Names()
	for _, name := range names {
		found := false
		for _, e := range embedded {
			if e == name {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("unknown template %q (see: go-ispconfig templates list)", name)
		}
		if !force {
			if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
				return fmt.Errorf("refusing to overwrite existing %s (use --force)", filepath.Join(dir, name))
			}
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating custom template directory: %w", err)
	}
	for _, name := range names {
		data, err := mastertpl.Templates.ReadFile("templates/" + name)
		if err != nil {
			return fmt.Errorf("reading embedded template %q: %w", name, err)
		}
		target := filepath.Join(dir, name)
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", target, err)
		}
		if _, err := fmt.Fprintf(w, "exported %s\n", target); err != nil {
			return err
		}
	}
	return nil
}

func init() {
	templatesCmd.PersistentFlags().StringVar(&templatesDir, "dir", "",
		"custom template directory (overrides [templates] custom_dir)")
	templatesExportCmd.Flags().BoolVar(&templatesAll, "all", false, "export every embedded template")
	templatesExportCmd.Flags().BoolVar(&templatesForce, "force", false, "overwrite existing customized files")
	templatesCmd.AddCommand(templatesListCmd, templatesExportCmd)
	rootCmd.AddCommand(templatesCmd)
}
