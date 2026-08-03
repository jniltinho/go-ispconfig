package web

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"

	"go-ispconfig/internal/engine"
)

// RenewJobName is the scheduler job identifier for the daily renewal.
const RenewJobName = "letsencrypt_renew"

// RenewJobSpec runs the renewal daily at 02:00 (the client owns the real
// renewal-due logic; this just triggers it).
const RenewJobSpec = "0 2 * * *"

// ACMEKind identifies the detected ACME client.
type ACMEKind int

// ACMENone means no client is installed; ACMEAcme is acme.sh and ACMECertbot
// is certbot / the legacy letsencrypt client.
const (
	ACMENone ACMEKind = iota
	ACMEAcme
	ACMECertbot
)

// AcmeScriptCandidates lists the acme.sh paths probed with `which`.
var AcmeScriptCandidates = []string{"acme.sh", "/root/.acme.sh/acme.sh"}

// CertbotCandidates lists the certbot paths probed with `which`.
var CertbotCandidates = []string{"certbot", "/opt/eff.org/certbot/venv/bin/certbot", "letsencrypt"}

// WhichExecutable runs `which` over the candidates and returns the first
// executable path, "" when none is installed.
func WhichExecutable(ctx context.Context, runner engine.CommandRunner, candidates []string) string {
	out, err := runner.Run(ctx, "which", candidates...)
	if err != nil {
		return ""
	}
	for line := range strings.SplitSeq(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if info, err := os.Stat(line); err == nil && info.Mode()&0o111 != 0 {
			return line
		}
	}
	return ""
}

// DetectACME returns the installed client (acme.sh preferred, certbot
// fallback) and the path of its script.
func DetectACME(ctx context.Context, runner engine.CommandRunner) (ACMEKind, string) {
	if script := WhichExecutable(ctx, runner, AcmeScriptCandidates); script != "" {
		return ACMEAcme, script
	}
	if script := WhichExecutable(ctx, runner, CertbotCandidates); script != "" {
		return ACMECertbot, script
	}
	return ACMENone, ""
}

// RenewCertificates runs the detected client's renew command and schedules a
// reload of serviceKey only when a certificate was actually renewed.
//
// The job is web-server agnostic: acme.sh/certbot renew every certificate
// they know about regardless of which server serves it, so nginx and Apache
// share this body and only differ in the service key they reload.
func RenewCertificates(ctx context.Context, runner engine.CommandRunner, services *engine.Services, log *slog.Logger, serviceKey string) error {
	if log == nil {
		log = slog.Default()
	}
	kind, script := DetectACME(ctx, runner)
	var out []byte
	var err error
	switch kind {
	case ACMEAcme:
		out, err = runner.Run(ctx, script, "--cron")
	case ACMECertbot:
		out, err = runner.Run(ctx, script, "renew", "-n")
	case ACMENone:
		log.Info("web: LE renewal skipped, no client found")
		return nil
	}
	if err != nil {
		return fmt.Errorf("web: LE renewal failed: %w: %s", err, out)
	}
	if RenewalHappened(kind, string(out)) {
		log.Info("web: LE certificates renewed, scheduling a reload", "service", serviceKey)
		if services != nil {
			services.Register(serviceKey)
			services.RestartServiceDelayed(serviceKey, engine.ActionReload)
		}
	}
	return nil
}

// acmeRenewedRe matches acme.sh output indicating a cert was renewed/installed.
var acmeRenewedRe = regexp.MustCompile(`(?i)(Cert success|Reload success|Renew.*success|Installing cert)`)

// RenewalHappened reports whether the client's renew output indicates a real
// renewal (so the web server should reload).
func RenewalHappened(kind ACMEKind, out string) bool {
	switch kind {
	case ACMEAcme:
		return acmeRenewedRe.MatchString(out)
	case ACMECertbot:
		// certbot prints "No renewals were attempted" when nothing changed.
		return !strings.Contains(out, "No renewals were attempted") &&
			strings.Contains(strings.ToLower(out), "renew")
	case ACMENone:
	}
	return false
}
