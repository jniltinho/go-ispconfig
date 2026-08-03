package apache2

import (
	"context"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/web"
)

// RegisterRenewal adds the daily Let's Encrypt renewal job to the scheduler.
// An Apache-only server has no nginx plugin to run it, and its vhosts serve
// the very certificate files acme.sh renews, so the job must be registered
// here too — only one of the two web plugins is ever loaded.
func (p *Plugin) RegisterRenewal(s *engine.Scheduler) error {
	return s.Register(web.RenewJobName, web.RenewJobSpec, p.renewCertificates)
}

// renewCertificates renews via the shared web helper and reloads Apache when
// a certificate actually changed.
func (p *Plugin) renewCertificates(ctx context.Context) error {
	return web.RenewCertificates(ctx, p.runner, p.services, p.log, ServiceName)
}
