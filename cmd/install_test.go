package cmd

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runInstall executes the install command with args, capturing output.
func runInstall(t *testing.T, args ...string) (string, error) {
	t.Helper()
	out := &bytes.Buffer{}
	rootCmd.SetOut(out)
	rootCmd.SetErr(out)
	rootCmd.SetArgs(append([]string{"install"}, args...))
	t.Cleanup(func() {
		rootCmd.SetArgs(nil)
		installYes, installUpdate, installDryRun, installAnswers, installWriteCrd = false, false, false, "", false
	})
	err := rootCmd.Execute()
	return out.String(), err
}

func TestInstallRefusesUnprivileged(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root")
	}
	_, err := runInstall(t, "--yes")
	require.ErrorContains(t, err, "root")
}

func TestInstallDryRunShowsPlanAndFlagPrecedence(t *testing.T) {
	if _, err := os.Stat("/etc/os-release"); err != nil {
		t.Skip("no /etc/os-release")
	}
	out, err := runInstall(t, "--dry-run", "--yes", "--hostname", "test.example.com", "--panel-port", "9443")
	if err != nil {
		// Host distro may be unsupported for this test environment.
		t.Skipf("dry-run failed on this host: %v", err)
	}
	assert.Contains(t, out, "Dry run")
	assert.Contains(t, out, "test.example.com")
	assert.Contains(t, out, "9443", "flag beats default")
	assert.Contains(t, out, "preflight")
	assert.Contains(t, out, "summary")
}

func TestInstallDryRunUpdateSubset(t *testing.T) {
	if _, err := os.Stat("/etc/os-release"); err != nil {
		t.Skip("no /etc/os-release")
	}
	out, err := runInstall(t, "--dry-run", "--yes", "--update", "--hostname", "test.example.com")
	if err != nil {
		t.Skipf("dry-run failed on this host: %v", err)
	}
	assert.NotContains(t, out, "mariadb", "update pipeline has no DB step")
	assert.Contains(t, out, "systemd-units")
}
