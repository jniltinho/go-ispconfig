package powerdns

import (
	"context"

	"go-ispconfig/internal/engine"
)

// ServiceName is the delayed services registry key (section 3).
const ServiceName = "powerdns"

// controlTools holds discovered binary paths (filled in section 3).
type controlTools struct {
	pdnsControl string
	pdnsUtil    string
	version     string
}

// Stubs until task 3.1 implements discovery and wrappers. Missing binaries
// are non-fatal (PHP parity).

func (p *Plugin) doRediscover(ctx context.Context) {
	_ = ctx
}

func (p *Plugin) doNotify(ctx context.Context, data engine.Data) {
	_ = ctx
	_ = data
}

func (p *Plugin) doRetrieve(ctx context.Context, data engine.Data) {
	_ = ctx
	_ = data
}

func (p *Plugin) doRectify(ctx context.Context, data engine.Data) {
	_ = ctx
	_ = data
}

func (p *Plugin) doHandleDNSSEC(ctx context.Context, data engine.Data) {
	_ = ctx
	_ = data
}
