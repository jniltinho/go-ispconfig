package installer

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

// Executor runs external commands (apt-get, systemctl, useradd, nginx -t,
// named-checkconf). Production shells out; tests inject a mock so the test
// suite never touches the OS (repo pattern, see internal/engine.Executor).
type Executor interface {
	// Run executes name with args and returns the combined output.
	Run(ctx context.Context, env []string, name string, args ...string) ([]byte, error)
	// LookPath reports whether an executable is on PATH.
	LookPath(name string) (string, error)
}

// commandTimeout caps a single external command; apt installs on a slow
// mirror are the worst case.
const commandTimeout = 30 * time.Minute

// execRunner is the production Executor.
type execRunner struct{}

// Run executes the command with the extra env appended, capped at
// commandTimeout, returning combined output (included in the error too).
func (execRunner) Run(ctx context.Context, env []string, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	if len(env) > 0 {
		cmd.Env = append(cmd.Environ(), env...)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%s %v: %w: %s", name, args, err, out)
	}
	return out, nil
}

// LookPath resolves an executable through the real PATH.
func (execRunner) LookPath(name string) (string, error) { return exec.LookPath(name) }
