package engine

import (
	"context"
	"os/exec"
	"time"
)

// CommandRunner executes one operating-system command and returns its
// combined output. Modules and plugins use it for every shell interaction
// (nginx -t, useradd, chown, ...) so tests can inject a fake and never touch
// the real system. Commands are argv slices only — no shell interpolation.
type CommandRunner interface {
	// Run executes name with args and returns the combined stdout/stderr.
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// commandTimeout caps one command invocation so a hanging binary can never
// stall the daemon cycle indefinitely.
const commandTimeout = 90 * time.Second

// ExecRunner is the production CommandRunner backed by os/exec.
type ExecRunner struct{}

// Run executes the command with a hard timeout and returns its combined
// output.
func (ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}
