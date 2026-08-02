package clientdb

import (
	"context"

	"go-ispconfig/internal/engine"
)

// eventPrefix maps the hooked datalog tables to their event name prefixes
// (port of database_module.inc.php process): web_database raises
// database_insert/update/delete, web_database_user raises
// database_user_insert/update/delete.
var eventPrefix = map[string]string{
	"web_database":      "database",
	"web_database_user": "database_user",
}

// Module is the database module. Wire it into the daemon via
// engine.Registry.Load only when the local server row has db_server = 1
// and daemon.disable_database_module is not set in config.toml
// (database-module-events / design D15).
type Module struct {
	reg *engine.Registry
}

// NewModule creates the database module.
func NewModule() *Module { return &Module{} }

// Name identifies the module in logs.
func (*Module) Name() string { return "database" }

// OnLoad announces the six database events and registers the table hooks
// for web_database and web_database_user (port of
// database_module.inc.php onLoad). No service is registered: MySQL
// privilege changes apply via FLUSH PRIVILEGES inside the plugin, never a
// service bounce (design D2).
func (m *Module) OnLoad(r *engine.Registry) error {
	m.reg = r
	var events []string
	for table, prefix := range eventPrefix {
		events = append(events, prefix+"_insert", prefix+"_update", prefix+"_delete")
		r.RegisterTableHook(table, m.process)
	}
	r.AnnounceEvents(m.Name(), events...)
	return nil
}

// process raises the named event for one table change: web_database +
// action "u" becomes database_update, and so on.
func (m *Module) process(ctx context.Context, table, action string, data engine.Data) error {
	return m.reg.RaiseEvent(ctx, engine.EventName(eventPrefix[table], action), data)
}
