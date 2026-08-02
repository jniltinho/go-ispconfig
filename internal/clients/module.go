package clients

import (
	"context"

	"go-ispconfig/internal/engine"
)

// Module is the daemon client module (port of
// server/mods-available/client_module.inc.php): it translates
// sys_datalog changes of the client table into the named events
// client_insert / client_update / client_delete for plugins (e.g. the
// nginx plugin's client_delete teardown). It performs no OS mutation
// itself and loads on every daemon regardless of server role flags —
// client rows are broadcast with server_id = 0.
type Module struct {
	// DisableHook keeps the events announced (so plugin subscriptions
	// still Load) but stops translating datalog rows into events —
	// the daemon.disable_client_events emergency switch.
	DisableHook bool

	reg *engine.Registry
}

// NewModule creates the client module.
func NewModule() *Module { return &Module{} }

// Name identifies the module in logs.
func (*Module) Name() string { return "client" }

// OnLoad announces the client events and registers the table hook
// (client_module.inc.php onLoad).
func (m *Module) OnLoad(r *engine.Registry) error {
	m.reg = r
	if !m.DisableHook {
		r.RegisterTableHook("client", m.process)
	}
	r.AnnounceEvents(m.Name(), "client_insert", "client_update", "client_delete")
	return nil
}

// process raises the named event for one client table change
// (client_module.inc.php process): action i/u/d becomes
// client_insert/client_update/client_delete with the {old,new} payload.
func (m *Module) process(ctx context.Context, table, action string, data engine.Data) error {
	return m.reg.RaiseEvent(ctx, engine.EventName(table, action), data)
}
