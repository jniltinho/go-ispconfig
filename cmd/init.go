package cmd

// initCmd writes a default config.toml to the current directory from the
// config.toml.example embedded at build time. It never overwrites an
// existing file.
//
// One value is not copied verbatim: the JWT signing key is generated here.
// The embedded example ships with an empty jwt_secret on purpose — a literal
// key checked into the repository would be the same key on every install in
// the world — so the randomness has to be produced at write time, the same
// way the installer does it.

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

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

		defaultConfig, err := fs.ReadFile(globalFS, "config.toml.example")
		if err != nil {
			return fmt.Errorf("failed to load embedded default configuration: %w", err)
		}
		if defaultConfig, err = withGeneratedJWTSecret(defaultConfig); err != nil {
			return err
		}

		// O_EXCL: fail atomically if the file already exists (no Stat/Write race).
		// 0600: the config holds database credentials.
		f, err := os.OpenFile(targetFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			if os.IsExist(err) {
				return fmt.Errorf("file already exists: %s\nUse -o to specify a different filename", targetFile)
			}
			return fmt.Errorf("failed to create configuration file: %w", err)
		}
		if _, err := f.Write(defaultConfig); err != nil {
			_ = f.Close()
			return fmt.Errorf("failed to write configuration file: %w", err)
		}
		if err := f.Close(); err != nil {
			return fmt.Errorf("failed to write configuration file: %w", err)
		}

		fmt.Printf("Created default configuration file: %s\n", targetFile)
		fmt.Println("Review the [server] and [database] sections, then run: go-ispconfig migrate && go-ispconfig serve")
		fmt.Println("A random [auth] jwt_secret was generated for you; keep it out of version control.")
		return nil
	},
}

// jwtSecretPlaceholder is the empty value the embedded example carries.
const jwtSecretPlaceholder = `jwt_secret = ""`

// withGeneratedJWTSecret substitutes a fresh signing key into the template.
// If the placeholder is ever renamed the substitution is a no-op rather than
// a silent corruption, and the config is still valid — the exchange endpoint
// simply stays disabled until an operator sets a key.
func withGeneratedJWTSecret(template []byte) ([]byte, error) {
	if !strings.Contains(string(template), jwtSecretPlaceholder) {
		return template, nil
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return nil, fmt.Errorf("failed to generate a jwt signing key: %w", err)
	}
	generated := fmt.Sprintf(`jwt_secret = %q`, hex.EncodeToString(buf))
	return []byte(strings.Replace(string(template), jwtSecretPlaceholder, generated, 1)), nil
}

func init() {
	initCmd.Flags().StringVarP(&initOutput, "output", "o", "config.toml", "name of the generated configuration file")
	rootCmd.AddCommand(initCmd)
}
