// Package dns implements the DNS stack of the daemon (openspec change
// add-dns-bind-module): the dns module translating sys_datalog changes of
// the dns tables into named events (port of ISPConfig3's
// server/mods-available/dns_module.inc.php) and the bind plugin applying
// them to Bind zone files and named.conf.local (port of
// server/plugins-available/bind_plugin.inc.php). One Go package, two
// registrations (design D1).
package dns

import (
	"context"

	"go-ispconfig/internal/engine"
)

// hookedTables are the datalog tables the dns module translates into events.
var hookedTables = []string{"dns_soa", "dns_slave", "dns_rr"}

// Module is the dns module. Wire it into the daemon via
// engine.Registry.Load only when the server row has dns_server = 1
// (dns-module-events spec: non-DNS servers register no dns hooks).
type Module struct {
	reg *engine.Registry
}

// NewModule creates the dns module.
func NewModule() *Module { return &Module{} }

// Name identifies the module in logs.
func (*Module) Name() string { return "dns" }

// OnLoad announces the nine dns events and registers the table hooks
// (port of dns_module.inc.php onLoad). The powerdns service of the PHP
// original is not ported (proposal non-goal).
func (m *Module) OnLoad(r *engine.Registry) error {
	m.reg = r
	var events []string
	for _, table := range hookedTables {
		events = append(events, table+"_insert", table+"_update", table+"_delete")
		r.RegisterTableHook(table, m.process)
	}
	r.AnnounceEvents(m.Name(), events...)
	return nil
}

// process raises the named event for one table change (port of
// dns_module.inc.php process): dns_soa + action "u" becomes
// dns_soa_update, and so on.
func (m *Module) process(ctx context.Context, table, action string, data engine.Data) error {
	return m.reg.RaiseEvent(ctx, engine.EventName(table, action), data)
}
