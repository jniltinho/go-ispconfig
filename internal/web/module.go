// Package web implements the web module of the daemon (openspec change
// add-web-nginx-module): it translates sys_datalog changes of the web tables
// into named events for plugins and registers the httpd / php-fpm services.
// It is the Go port of ISPConfig3's server/mods-available/web_module.inc.php,
// trimmed to the tables this port hooks (design D1: unhooked tables like
// ftp_user or aps_* are simply not registered — adding them later is
// additive).
package web

import (
	"context"

	"go-ispconfig/internal/engine"
)

// hookedTables are the datalog tables the web module translates into events.
// server_php stays a plain data table in this port: no plugin consumes
// server_php_* events yet, so they are not announced (design D1).
var hookedTables = []string{"web_domain", "web_folder", "web_folder_user"}

// Module is the web module. Wire it into the daemon via
// engine.Registry.Load.
type Module struct {
	reg *engine.Registry
}

// NewModule creates the web module.
func NewModule() *Module { return &Module{} }

// Name identifies the module in logs.
func (*Module) Name() string { return "web" }

// OnLoad announces the web events and registers the table hooks (port of
// web_module.inc.php onLoad). client_delete is announced here too: the
// nginx plugin subscribes to it for the client teardown cascade
// (web-module-events spec); the event itself will be raised by the future
// client module.
func (m *Module) OnLoad(r *engine.Registry) error {
	m.reg = r
	events := []string{"client_delete"}
	for _, table := range hookedTables {
		events = append(events, table+"_insert", table+"_update", table+"_delete")
		r.RegisterTableHook(table, m.process)
	}
	r.AnnounceEvents(m.Name(), events...)
	return nil
}

// process raises the named event for one table change (port of
// web_module.inc.php process): web_domain + action "u" becomes
// web_domain_update, and so on.
func (m *Module) process(ctx context.Context, table, action string, data engine.Data) error {
	return m.reg.RaiseEvent(ctx, engine.EventName(table, action), data)
}
