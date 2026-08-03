package powerdns

import (
	"context"
	"log/slog"

	"gorm.io/gorm"

	"go-ispconfig/internal/engine"
)

// Plugin applies dns_* events to the PowerDNS gmysql schema (port of
// powerdns_plugin.inc.php). Panel dns_* rows stay on panelDB; zones/records
// are written to pdnsDB (database "powerdns").
type Plugin struct {
	panelDB  *gorm.DB
	pdnsDB   *gorm.DB
	services *engine.Services
	runner   engine.CommandRunner
	log      *slog.Logger
	serverID uint32

	// tools is the pdns_control / pdnsutil discovery cache (filled lazily).
	tools *controlTools
}

// NewPlugin creates the PowerDNS plugin for one server. panelDB is the
// ISPConfig schema; pdnsDB is the PowerDNS schema (may be the same host).
// log nil means slog.Default.
func NewPlugin(
	panelDB, pdnsDB *gorm.DB,
	services *engine.Services,
	runner engine.CommandRunner,
	serverID uint32,
	log *slog.Logger,
) *Plugin {
	if log == nil {
		log = slog.Default()
	}
	return &Plugin{
		panelDB:  panelDB,
		pdnsDB:   pdnsDB,
		services: services,
		runner:   runner,
		log:      log,
		serverID: serverID,
	}
}

// Name identifies the plugin in logs and the registry.
func (*Plugin) Name() string { return "powerdns" }

// OnLoad subscribes to the nine dns module events (port of
// powerdns_plugin.inc.php onLoad). The dns module must be loaded first.
func (p *Plugin) OnLoad(r *engine.Registry) error {
	subs := []struct {
		event string
		fn    engine.EventFunc
	}{
		{"dns_soa_insert", p.soaInsert},
		{"dns_soa_update", p.soaUpdate},
		{"dns_soa_delete", p.soaDelete},
		{"dns_slave_insert", p.slaveInsert},
		{"dns_slave_update", p.slaveUpdate},
		{"dns_slave_delete", p.slaveDelete},
		{"dns_rr_insert", p.rrInsert},
		{"dns_rr_update", p.rrUpdate},
		{"dns_rr_delete", p.rrDelete},
	}
	for _, s := range subs {
		if err := r.RegisterEvent(s.event, s.fn); err != nil {
			return err
		}
	}
	return nil
}

// forThisServer returns true when the event payload targets this daemon's
// server_id (or server_id is missing — datalog is already filtered by the
// engine, but direct RaiseEvent tests still gate).
func (p *Plugin) forThisServer(data engine.Data) bool {
	sid := payloadServerID(data)
	return sid == 0 || sid == p.serverID
}

// noop handlers are filled by later tasks; 2.1 only wires the registry and
// the server_id gate so tests can prove subscription without SQL.

func (p *Plugin) soaInsert(ctx context.Context, event string, data engine.Data) error {
	if !p.forThisServer(data) {
		return nil
	}
	return p.handleSOAInsert(ctx, data)
}

func (p *Plugin) soaUpdate(ctx context.Context, event string, data engine.Data) error {
	if !p.forThisServer(data) {
		return nil
	}
	return p.handleSOAUpdate(ctx, data)
}

func (p *Plugin) soaDelete(ctx context.Context, event string, data engine.Data) error {
	if !p.forThisServer(data) {
		return nil
	}
	return p.handleSOADelete(ctx, data)
}

func (p *Plugin) slaveInsert(ctx context.Context, event string, data engine.Data) error {
	if !p.forThisServer(data) {
		return nil
	}
	return p.handleSlaveInsert(ctx, data)
}

func (p *Plugin) slaveUpdate(ctx context.Context, event string, data engine.Data) error {
	if !p.forThisServer(data) {
		return nil
	}
	return p.handleSlaveUpdate(ctx, data)
}

func (p *Plugin) slaveDelete(ctx context.Context, event string, data engine.Data) error {
	if !p.forThisServer(data) {
		return nil
	}
	return p.handleSlaveDelete(ctx, data)
}

func (p *Plugin) rrInsert(ctx context.Context, event string, data engine.Data) error {
	if !p.forThisServer(data) {
		return nil
	}
	return p.handleRRInsert(ctx, data)
}

func (p *Plugin) rrUpdate(ctx context.Context, event string, data engine.Data) error {
	if !p.forThisServer(data) {
		return nil
	}
	return p.handleRRUpdate(ctx, data)
}

func (p *Plugin) rrDelete(ctx context.Context, event string, data engine.Data) error {
	if !p.forThisServer(data) {
		return nil
	}
	return p.handleRRDelete(ctx, data)
}
