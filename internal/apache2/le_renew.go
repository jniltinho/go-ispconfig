package apache2

import (
	"context"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/web"
)

// RegisterRenewal adds the daily Let's Encrypt renewal job to the scheduler.
// An Apache-only server has no nginx plugin to run it, and its vhosts serve
// the very certificate files the native client renews, so the job must be
// registered here too — only one of the two web plugins is ever loaded.
func (p *Plugin) RegisterRenewal(s *engine.Scheduler) error {
	return s.Register(web.RenewJobName, web.RenewJobSpec, p.renewCertificates)
}

// renewCertificates renews via the native ACME client and reloads Apache when
// a certificate actually changed.
func (p *Plugin) renewCertificates(ctx context.Context) error {
	mgr := p.acmeManager(p.serverID, "ECDSA")
	if p.acmeMgr != nil {
		mgr = p.acmeMgr
	}
	return web.RenewNative(ctx, mgr, p.services, p.log, ServiceName)
}
