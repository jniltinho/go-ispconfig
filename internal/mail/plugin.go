package mail

import (
	"context"
	"log/slog"
	"os/user"
	"strconv"

	"gorm.io/gorm"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/getconf"
)

// Plugin is the mail plugin: maildir lifecycle, domain delete cascade
// and transport-triggered Postfix reload (port of mail_plugin.inc.php).
type Plugin struct {
	db       *gorm.DB
	services *engine.Services
	runner   engine.CommandRunner
	serverID uint32
	log      *slog.Logger

	// LookupUserUID resolves a system username to its uid (virtual
	// uid/gid maps); nil means os/user lookup. Tests inject a fake.
	LookupUserUID func(name string) (int64, bool)
	// LoadConfig loads the [mail] getconf section; nil means the DB
	// getconf of this plugin's server. Tests inject a fixed config.
	LoadConfig func(ctx context.Context) (getconf.MailConfig, error)
}

// NewPlugin creates the mail plugin for one server.
func NewPlugin(db *gorm.DB, services *engine.Services, runner engine.CommandRunner, serverID uint32, log *slog.Logger) *Plugin {
	if log == nil {
		log = slog.Default()
	}
	return &Plugin{db: db, services: services, runner: runner, serverID: serverID, log: log}
}

// Name identifies the plugin in logs and the registry.
func (*Plugin) Name() string { return "mail" }

// OnLoad subscribes the plugin to its events (spec plugin subscription
// matrix; PHP registers all three transport events onto one handler).
// Handlers are registered by the tasks that implement them.
func (p *Plugin) OnLoad(r *engine.Registry) error {
	for event, handler := range p.handlers() {
		h := handler
		if err := r.RegisterEvent(event, func(ctx context.Context, _ string, data engine.Data) error {
			return h(ctx, data)
		}); err != nil {
			return err
		}
	}
	return nil
}

// handlers maps event names to their implementations.
func (p *Plugin) handlers() map[string]func(context.Context, engine.Data) error {
	return map[string]func(context.Context, engine.Data) error{
		"mail_user_insert": p.userInsert,
	}
}

// config loads the [mail] section (typed, with defaults).
func (p *Plugin) config(ctx context.Context) (getconf.MailConfig, error) {
	if p.LoadConfig != nil {
		return p.LoadConfig(ctx)
	}
	cfg, err := getconf.GetServerConfig(p.db, p.serverID)
	if err != nil {
		return getconf.DefaultMailConfig(), err
	}
	return cfg.Mail, nil
}

// resolveUIDGID ports the uid/gid resolution of user_insert/user_update:
// -1 values map to the web_domain system user when virtual uid/gid maps
// are on and web+mail share the server, else fall back to the getconf
// mailuser uid/gid. Changed values are written back onto mail_user
// directly (no datalog row — PHP parity).
func (p *Plugin) resolveUIDGID(ctx context.Context, cfg getconf.MailConfig, newRow row) (uid, gid int64) {
	uid, gid = newRow.num("uid"), newRow.num("gid")
	if uid != -1 && gid != -1 {
		return uid, gid
	}
	if cfg.MailboxVirtualUidgidMaps == "y" {
		if mapped, ok := p.webDomainUID(ctx, newRow.str("email"), newRow.num("server_id")); ok {
			uid = mapped
		}
	}
	if uid == -1 {
		uid, _ = strconv.ParseInt(cfg.MailuserUID, 10, 64)
	}
	if gid == -1 {
		gid, _ = strconv.ParseInt(cfg.MailuserGID, 10, 64)
	}

	if uid != newRow.num("uid") || gid != newRow.num("gid") {
		// Direct UPDATE, never through the datalog writer: the daemon
		// must not journal its own bookkeeping (PHP parity).
		err := p.db.WithContext(ctx).Exec(
			"UPDATE mail_user SET uid = ?, gid = ? WHERE mailuser_id = ?",
			uid, gid, newRow.num("mailuser_id")).Error
		if err != nil {
			p.log.Error("mail: could not write back uid/gid", "mailuser_id", newRow.num("mailuser_id"), "error", err)
		}
		p.log.Debug("mail: resolved mailbox uid/gid", "uid", uid, "gid", gid)
	}
	return uid, gid
}

// webDomainUID resolves the system uid of the web_domain matching the
// email's domain part, walking parent_domain_id up while the row has no
// system user (mail_plugin parity). Only applies when the web domain
// lives on the same server as the mailbox.
func (p *Plugin) webDomainUID(ctx context.Context, email string, serverID int64) (int64, bool) {
	at := -1
	for i, c := range email {
		if c == '@' {
			at = i
		}
	}
	if at < 0 {
		return 0, false
	}
	type webRow struct {
		DomainID       uint32
		ServerID       int64
		SystemUser     string
		ParentDomainID uint32
	}
	var w webRow
	err := p.db.WithContext(ctx).Table("web_domain").
		Select("domain_id, server_id, system_user, parent_domain_id").
		Where("domain = ?", email[at+1:]).Take(&w).Error
	if err != nil {
		return 0, false
	}
	for w.SystemUser == "" && w.ParentDomainID != 0 {
		err = p.db.WithContext(ctx).Table("web_domain").
			Select("domain_id, server_id, system_user, parent_domain_id").
			Where("domain_id = ?", w.ParentDomainID).Take(&w).Error
		if err != nil {
			return 0, false
		}
	}
	if w.ServerID != serverID || w.SystemUser == "" {
		return 0, false
	}
	lookup := p.LookupUserUID
	if lookup == nil {
		lookup = systemUID
	}
	return lookup(w.SystemUser)
}

// systemUID resolves a username via the OS user database.
func systemUID(name string) (int64, bool) {
	u, err := user.Lookup(name)
	if err != nil {
		return 0, false
	}
	uid, err := strconv.ParseInt(u.Uid, 10, 64)
	return uid, err == nil
}
