// Package cron implements the client cron module of the daemon (openspec
// change add-cron-module): it translates sys_datalog changes of the cron
// table into named events for the client-job plugin. It is the Go port of
// ISPConfig3's server/mods-available/cron_module.inc.php. Enablement
// (server.web_server = 1 and config.toml) is applied at daemon bootstrap
// (task 2.2 / design D1).
package cron

import (
	"context"

	"go-ispconfig/internal/engine"
)

// hookedTable is the single datalog table the cron module owns.
const hookedTable = "cron"

// Module is the cron module. Wire it into the daemon via
// engine.Registry.Load only when the local server row has web_server = 1
// and the module is enabled in config.toml (cron-module-events / design D1).
type Module struct {
	reg *engine.Registry
}

// NewModule creates the cron module.
func NewModule() *Module { return &Module{} }

// Name identifies the module in logs.
func (*Module) Name() string { return "cron" }

// OnLoad announces cron_insert/update/delete and registers the table hook
// for cron (port of cron_module.inc.php onLoad/process).
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

// process raises the named event for one cron table change:
// action "i" → cron_insert, "u" → cron_update, "d" → cron_delete.
func (m *Module) process(ctx context.Context, table, action string, data engine.Data) error {
	return m.reg.RaiseEvent(ctx, engine.EventName(table, action), data)
}
