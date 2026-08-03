package cron

import (
	"context"
	"fmt"
	"os/exec"
	"path"
	"strings"
	"time"
	"unicode"

	"go-ispconfig/internal/model"
)

// DefaultPHPCLI is the fallback for {SITE_PHP} when server_php has no
// php_cli_binary (PHP cron_plugin parity).
const DefaultPHPCLI = "/usr/bin/php"

// DefaultExecTimeout caps full/chrooted job runtime (same 7200s as URL jobs).
const DefaultExecTimeout = 7200 * time.Second

// maxExecOutput is the bounded stdout+stderr tail retained for sys_log.
const maxExecOutput = 4 * 1024

// SiteContext holds parent web_domain fields needed to expand and run a job.
type SiteContext struct {
	Domain       string
	DocumentRoot string
	SystemUser   string
	SystemGroup  string
	// PHPCLIBinary comes from server_php.php_cli_binary; empty → DefaultPHPCLI.
	PHPCLIBinary string
}

// ExecSpec is a prepared full/chrooted invocation (no shell).
type ExecSpec struct {
	Argv []string
	Cwd  string
	// Type is model.CronTypeFull or model.CronTypeChrooted.
	Type string
}

// PHPCLI returns the CLI binary path for {SITE_PHP}.
func (s SiteContext) PHPCLI() string {
	if strings.TrimSpace(s.PHPCLIBinary) == "" {
		return DefaultPHPCLI
	}
	return s.PHPCLIBinary
}

// DocrootClient is document_root + "/web" (PHP $web_docroot_client for full jobs).
func (s SiteContext) DocrootClient() string {
	return path.Join(s.DocumentRoot, "web")
}

// ExpandCommand applies placeholders and chrooted document_root stripping
// (port of cron_plugin::_write_crontab full/chrooted branch).
func ExpandCommand(command, jobType string, site SiteContext) (string, error) {
	if strings.ContainsAny(command, "\r\n\x00") {
		return "", fmt.Errorf("insecure cron command skipped")
	}
	cmd := command
	if jobType == model.CronTypeChrooted && site.DocumentRoot != "" {
		if strings.HasPrefix(cmd, site.DocumentRoot) {
			cmd = strings.TrimPrefix(cmd, site.DocumentRoot)
			if cmd == "" {
				cmd = "/"
			}
		}
	}

	docrootClient := site.DocrootClient()
	// PHP builds $web_docroot_client differently for chrooted: starts empty
	// then only appends '/web', so [web_root]/{DOCROOT_CLIENT} become "/web".
	if jobType == model.CronTypeChrooted {
		docrootClient = "/web"
	}

	replacer := strings.NewReplacer(
		"[web_root]", docrootClient,
		"{DOCROOT_CLIENT}", docrootClient,
		"{DOMAIN}", site.Domain,
		"{SITE_PHP}", site.PHPCLI(),
	)
	return replacer.Replace(cmd), nil
}

// SplitArgv splits a command string into argv without invoking a shell.
// Supports double and single quotes and backslash escapes inside double
// quotes; does not expand variables, globs, or pipes.
func SplitArgv(command string) ([]string, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil, fmt.Errorf("empty command")
	}
	var (
		args                        []string
		cur                         strings.Builder
		inSingle, inDouble, escaped bool
	)
	for _, r := range command {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped = false
		case r == '\\' && inDouble:
			escaped = true
		case r == '\'' && !inDouble:
			inSingle = !inSingle
		case r == '"' && !inSingle:
			inDouble = !inDouble
		case unicode.IsSpace(r) && !inSingle && !inDouble:
			if cur.Len() > 0 {
				args = append(args, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if escaped || inSingle || inDouble {
		return nil, fmt.Errorf("unclosed quote in command")
	}
	if cur.Len() > 0 {
		args = append(args, cur.String())
	}
	if len(args) == 0 {
		return nil, fmt.Errorf("empty command")
	}
	return args, nil
}

// BuildExecSpec expands placeholders, splits argv, and sets cwd to
// {document_root}/web for full (and "/web" relative semantics for chrooted
// until jailkit lands — cwd stays document_root/web on the host).
func BuildExecSpec(command, jobType string, site SiteContext) (ExecSpec, error) {
	expanded, err := ExpandCommand(command, jobType, site)
	if err != nil {
		return ExecSpec{}, err
	}
	argv, err := SplitArgv(expanded)
	if err != nil {
		return ExecSpec{}, err
	}
	cwd := site.DocrootClient()
	return ExecSpec{Argv: argv, Cwd: cwd, Type: jobType}, nil
}

// ProcessRunner runs a prepared ExecSpec. Credential/NoNewPrivileges are
// applied by SysProcAttr when set (task 3.5).
type ProcessRunner struct {
	// Timeout caps the process; zero means DefaultExecTimeout.
	Timeout time.Duration
	// LookPath resolves the executable; nil uses exec.LookPath.
	LookPath func(file string) (string, error)
	// Configure optionally mutates the Cmd before Start (tests inject
	// Credential stubs; production uses ApplySiteCredentials in 3.5).
	Configure func(cmd *exec.Cmd, site SiteContext) error
}

// Run executes spec as the prepared argv with cwd, capturing a bounded
// combined output tail.
func (p *ProcessRunner) Run(ctx context.Context, spec ExecSpec, site SiteContext) RunResult {
	start := time.Now()
	res := RunResult{Start: start, Status: StatusError, ExitCode: -1}
	if len(spec.Argv) == 0 {
		res.Err = fmt.Errorf("empty argv")
		res.End = time.Now()
		res.Output = res.Err.Error()
		return res
	}

	timeout := p.Timeout
	if timeout <= 0 {
		timeout = DefaultExecTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	look := p.LookPath
	if look == nil {
		look = exec.LookPath
	}
	bin := spec.Argv[0]
	if path.IsAbs(bin) {
		// Absolute path: use as-is (no PATH search).
	} else {
		resolved, err := look(bin)
		if err != nil {
			res.Err = fmt.Errorf("looking up %q: %w", bin, err)
			res.End = time.Now()
			res.Output = res.Err.Error()
			return res
		}
		bin = resolved
	}

	cmd := exec.CommandContext(runCtx, bin, spec.Argv[1:]...)
	cmd.Dir = spec.Cwd
	if p.Configure != nil {
		if err := p.Configure(cmd, site); err != nil {
			res.Err = err
			res.End = time.Now()
			res.Output = err.Error()
			return res
		}
	}

	out, err := cmd.CombinedOutput()
	res.End = time.Now()
	res.Output = trimTail(string(out), maxExecOutput)
	if err != nil {
		if runCtx.Err() != nil {
			res.Status = StatusTimeout
			res.Err = fmt.Errorf("exec timed out: %w", err)
			return res
		}
		var ee *exec.ExitError
		if ok := errorAsExit(err, &ee); ok {
			res.Status = StatusExit
			res.ExitCode = ee.ExitCode()
			res.Err = err
			return res
		}
		res.Err = err
		res.Output = joinOutput(res.Output, err.Error())
		return res
	}
	res.Status = StatusOK
	res.ExitCode = 0
	return res
}

func errorAsExit(err error, target **exec.ExitError) bool {
	if err == nil {
		return false
	}
	ee, ok := err.(*exec.ExitError)
	if !ok {
		return false
	}
	*target = ee
	return true
}

func trimTail(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[len(s)-max:]
}

func joinOutput(out, extra string) string {
	if out == "" {
		return extra
	}
	return out + "\n" + extra
}
