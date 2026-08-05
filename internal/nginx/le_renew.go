package nginx

import (
	"context"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/web"
)

// RegisterRenewal adds the daily Let's Encrypt renewal job to the scheduler.
func (p *Plugin) RegisterRenewal(s *engine.Scheduler) error {
	return s.Register(web.RenewJobName, web.RenewJobSpec, p.renewCertificates)
}

// renewCertificates renews via the native ACME client and reloads nginx when
// a certificate actually changed.
func (p *Plugin) renewCertificates(ctx context.Context) error {
	mgr := p.acmeManager(p.serverID, "ECDSA")
	if p.acmeMgr != nil {
		mgr = p.acmeMgr
	}
	return web.RenewNative(ctx, mgr, p.services, p.log, web.HttpdService)
}
