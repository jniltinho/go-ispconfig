package cron

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultURLTimeout is the PHP wget -T 7200 parity for URL jobs (design D4).
const DefaultURLTimeout = 7200 * time.Second

// DefaultURLUserAgent identifies URL cron fetches (ISPConfig-compatible string).
const DefaultURLUserAgent = "Mozilla/5.0 (compatible; ISPConfig/3.0; +https://www.ispconfig.org/)"

// maxURLBody keeps a bounded response tail for sys_log (design D6: 4 KiB).
const maxURLBody = 4 * 1024

// Run status values used in sys_log cron_run messages (design D6).
const (
	StatusOK      = "ok"
	StatusExit    = "exit"
	StatusTimeout = "timeout"
	StatusError   = "error"
)

// RunResult is the outcome of one client job execution.
type RunResult struct {
	Status   string
	ExitCode int
	Output   string
	Start    time.Time
	End      time.Time
	Err      error
}

// URLExecutor performs HTTP GET of URL-type cron commands (design D4).
// TLS certificate verification is on by default (hardening vs PHP
// --no-check-certificate). Hostname shape is enforced at API validation
// time; at execution we re-check insecure characters and http(s) scheme.
type URLExecutor struct {
	// Client is the HTTP client; nil builds one with Timeout.
	Client *http.Client
	// Timeout caps the request; zero means DefaultURLTimeout.
	Timeout time.Duration
	// UserAgent overrides the request User-Agent; empty uses DefaultURLUserAgent.
	UserAgent string
}

// Execute expands {DOMAIN}, refuses insecure command characters, and GETs
// the resulting URL. Commands with CR/LF/NUL or backslash are refused
// without a request (PHP WARN skip parity).
func (e *URLExecutor) Execute(ctx context.Context, command, domain string) RunResult {
	start := time.Now()
	res := RunResult{Start: start, Status: StatusError, ExitCode: -1}

	if strings.ContainsAny(command, "\r\n\x00") || strings.Contains(command, `\`) {
		res.Err = fmt.Errorf("insecure cron command skipped")
		res.End = time.Now()
		res.Output = res.Err.Error()
		return res
	}

	urlStr := strings.ReplaceAll(command, "{DOMAIN}", domain)
	parsed, err := url.Parse(urlStr)
	if err != nil {
		res.Err = fmt.Errorf("invalid cron URL: %w", err)
		res.End = time.Now()
		res.Output = res.Err.Error()
		return res
	}
	if !strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https") {
		res.Err = fmt.Errorf("invalid cron URL scheme %q", parsed.Scheme)
		res.End = time.Now()
		res.Output = res.Err.Error()
		return res
	}
	if parsed.Host == "" {
		res.Err = fmt.Errorf("invalid cron URL host")
		res.End = time.Now()
		res.Output = res.Err.Error()
		return res
	}

	timeout := e.Timeout
	if timeout <= 0 {
		timeout = DefaultURLTimeout
	}
	client := e.Client
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}

	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, urlStr, nil)
	if err != nil {
		res.Err = err
		res.End = time.Now()
		res.Output = err.Error()
		return res
	}
	ua := e.UserAgent
	if ua == "" {
		ua = DefaultURLUserAgent
	}
	req.Header.Set("User-Agent", ua)

	resp, err := client.Do(req)
	if err != nil {
		res.End = time.Now()
		if reqCtx.Err() != nil {
			res.Status = StatusTimeout
			res.Err = fmt.Errorf("url job timed out: %w", err)
		} else {
			res.Err = err
		}
		res.Output = res.Err.Error()
		return res
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxURLBody+1))
	if len(body) > maxURLBody {
		body = body[len(body)-maxURLBody:]
	}
	res.Output = string(body)
	res.End = time.Now()
	res.ExitCode = resp.StatusCode
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		res.Status = StatusOK
	} else {
		res.Status = StatusExit
		res.Err = fmt.Errorf("http status %d", resp.StatusCode)
	}
	return res
}
