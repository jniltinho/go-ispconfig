package nginx

import (
	"context"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/web"
)

// RegisterRenewal adds the daily Let's Encrypt renewal job to the scheduler
// (design D6: acme.sh/certbot own their renewal state; no system cron).
func (p *Plugin) RegisterRenewal(s *engine.Scheduler) error {
	return s.Register(web.RenewJobName, web.RenewJobSpec, p.renewCertificates)
}

// renewCertificates renews via the shared web helper and reloads the httpd
// service key (mapped to the nginx unit by the GuardedExecutor).
func (p *Plugin) renewCertificates(ctx context.Context) error {
	return web.RenewCertificates(ctx, p.runner, p.services, p.log, web.HttpdService)
}
