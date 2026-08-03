package engine

import (
	"bytes"
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

// DirRunner is the optional working-directory extension of CommandRunner,
// for tools whose output location depends on the process CWD (e.g.
// dnssec-signzone writes its dsset file into the working directory).
type DirRunner interface {
	// RunInDir executes name with args in the working directory dir.
	RunInDir(ctx context.Context, dir, name string, args ...string) ([]byte, error)
}

// StdinRunner is the optional standard-input extension of CommandRunner,
// for tools that read a secret from a pipe instead of argv (chpasswd -e,
// whose hash must not end up visible in the process list).
type StdinRunner interface {
	// RunWithStdin executes name with args, feeding stdin to the process,
	// and returns the combined stdout/stderr.
	RunWithStdin(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error)
}

// commandTimeout caps one command invocation so a hanging binary can never
// stall the daemon cycle indefinitely.
const commandTimeout = 90 * time.Second

// ExecRunner is the production CommandRunner backed by os/exec.
type ExecRunner struct{}

// Run executes the command with a hard timeout and returns its combined
// output.
func (ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return ExecRunner{}.RunInDir(ctx, "", name, args...)
}

// RunInDir executes the command in dir ("" means the daemon CWD) with a
// hard timeout and returns its combined output.
func (ExecRunner) RunInDir(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

// RunWithStdin executes the command with a hard timeout, writing stdin to
// its standard input, and returns its combined output.
func (ExecRunner) RunWithStdin(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = bytes.NewReader(stdin)
	return cmd.CombinedOutput()
}
