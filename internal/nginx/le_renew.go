package nginx

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/web"
)

// renewJobName is the scheduler job identifier for the daily renewal.
const renewJobName = "letsencrypt_renew"

// renewJobSpec runs the renewal daily at 02:00 (the client owns the real
// renewal-due logic; this just triggers it).
const renewJobSpec = "0 2 * * *"

// RegisterRenewal adds the daily Let's Encrypt renewal job to the scheduler
// (design D6: acme.sh/certbot own their renewal state; no system cron).
func (p *Plugin) RegisterRenewal(s *engine.Scheduler) error {
	return s.Register(renewJobName, renewJobSpec, p.renewCertificates)
}

// renewCertificates runs the detected client's renew command and schedules an
// nginx reload only when a certificate was actually renewed.
func (p *Plugin) renewCertificates(ctx context.Context) error {
	c := p.newLEClient(ctx)
	if c.kind == leNone {
		p.log.Info("nginx: LE renewal skipped, no client found")
		return nil
	}
	var out []byte
	var err error
	switch c.kind {
	case leAcme:
		out, err = p.runner.Run(ctx, c.script, "--cron")
	case leCertbot:
		out, err = p.runner.Run(ctx, c.script, "renew", "-n")
	case leNone:
		return nil
	}
	if err != nil {
		return fmt.Errorf("nginx: LE renewal failed: %w: %s", err, out)
	}
	if renewalHappened(c.kind, string(out)) {
		p.log.Info("nginx: LE certificates renewed, scheduling nginx reload")
		if p.services != nil {
			p.services.Register(web.HttpdService)
			p.services.RestartServiceDelayed(web.HttpdService, engine.ActionReload)
		}
	}
	return nil
}

// acmeRenewedRe matches acme.sh output indicating a cert was renewed/installed.
var acmeRenewedRe = regexp.MustCompile(`(?i)(Cert success|Reload success|Renew.*success|Installing cert)`)

// renewalHappened reports whether the client's renew output indicates a real
// renewal (so nginx should reload).
func renewalHappened(kind leClientKind, out string) bool {
	switch kind {
	case leAcme:
		return acmeRenewedRe.MatchString(out)
	case leCertbot:
		// certbot prints "No renewals were attempted" when nothing changed.
		return !strings.Contains(out, "No renewals were attempted") &&
			strings.Contains(strings.ToLower(out), "renew")
	case leNone:
	}
	return false
}
