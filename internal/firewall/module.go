package firewall

import (
	"context"

	"go-ispconfig/internal/engine"
)

// hookedTable is the single datalog table the firewall module owns.
const hookedTable = "firewall"

// Module is the firewall module. Wire it into the daemon via
// engine.Registry.Load only when the local server row has
// firewall_server = 1 and the module is enabled in config.toml
// (firewall-module-events / design D3).
type Module struct {
	reg *engine.Registry
}

// NewModule creates the firewall module.
func NewModule() *Module { return &Module{} }

// Name identifies the module in logs.
func (*Module) Name() string { return "firewall" }

// OnLoad announces firewall_insert/update/delete and registers the
// table hook for firewall (port of server_module.inc.php onLoad/process
// case "firewall").
func (m *Module) OnLoad(r *engine.Registry) error {
	m.reg = r
	events := []string{
		hookedTable + "_insert",
		hookedTable + "_update",
		hookedTable + "_delete",
	}
	r.RegisterTableHook(hookedTable, m.process)
	r.AnnounceEvents(m.Name(), events...)
	return nil
}

// process raises the named event for one firewall table change:
// action "i" → firewall_insert, "u" → firewall_update, "d" → firewall_delete.
func (m *Module) process(ctx context.Context, table, action string, data engine.Data) error {
	return m.reg.RaiseEvent(ctx, engine.EventName(table, action), data)
}
