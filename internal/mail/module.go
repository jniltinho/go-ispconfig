// Package mail implements the mail module of the daemon (openspec change
// add-mail-module): it translates sys_datalog changes of the MAIL table
// group into named events for the mail plugins and registers the
// postfix / dovecot / rspamd services. Go port of ISPConfig3's
// server/mods-available/mail_module.inc.php, trimmed to the in-scope
// tables (design D2: mail_get, mail_content_filter and mail_mailinglist
// are non-goals and simply not registered).
package mail

import (
	"context"

	"go-ispconfig/internal/engine"
)

// hookedTables are the datalog tables the mail module translates into
// events (design D2). spamfilter_policy has no daemon hook in PHP either.
var hookedTables = []string{
	"mail_domain", "mail_user", "mail_forwarding",
	"mail_transport", "mail_access",
	"spamfilter_users", "spamfilter_wblist",
}

// serverTables are additionally hooked so the rspamd plugin can react
// to server / server_ip changes (PHP raises these from the server core;
// no other Go module announces them yet).
var serverTables = []string{"server", "server_ip"}

// Module is the mail module. Wire it into the daemon via
// engine.Registry.Load on servers with mail_server = 1.
type Module struct {
	reg *engine.Registry
}

// NewModule creates the mail module.
func NewModule() *Module { return &Module{} }

// Name identifies the module in logs.
func (*Module) Name() string { return "mail" }

// OnLoad announces the mail events and registers the table hooks (port
// of mail_module.inc.php onLoad/actions_available for the in-scope
// tables).
func (m *Module) OnLoad(r *engine.Registry) error {
	m.reg = r
	var events []string
	for _, table := range append(append([]string{}, hookedTables...), serverTables...) {
		events = append(events, table+"_insert", table+"_update", table+"_delete")
		r.RegisterTableHook(table, m.process)
	}
	r.AnnounceEvents(m.Name(), events...)
	return nil
}

// process raises the named event for one table change (port of
// mail_module.inc.php process): mail_user + action "u" becomes
// mail_user_update, and so on.
func (m *Module) process(ctx context.Context, table, action string, data engine.Data) error {
	return m.reg.RaiseEvent(ctx, engine.EventName(table, action), data)
}

// Service keys for delayed restart/reload requests. On Debian/Ubuntu the
// keys are the systemd unit names; amavis is never registered (design
// D3, rspamd supersedes it).
const (
	// PostfixService is the Postfix MTA service key.
	PostfixService = "postfix"
	// DovecotService is the Dovecot IMAP/POP3/LMTP service key (PHP's
	// mail_module does not register it; this port is Dovecot-centric).
	DovecotService = "dovecot"
	// RspamdService is the Rspamd content filter service key.
	RspamdService = "rspamd"
)

// RegisterServices declares the mail services in the delayed-restart
// registry so the mail plugins can queue restart/reload requests.
func RegisterServices(s *engine.Services) {
	s.Register(PostfixService)
	s.Register(DovecotService)
	s.Register(RspamdService)
}
