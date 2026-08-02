package mail

import (
	"context"
	"os"
	"strconv"
	"strings"

	"golang.org/x/net/idna"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/getconf"
	"go-ispconfig/internal/mastertpl"
)

// RspamdPlugin renders per-identity Rspamd settings, white/blacklist
// maps and server-level snippets (port of rspamd_plugin.inc.php).
type RspamdPlugin struct {
	base *Plugin
	// RspamdDir is the Rspamd config root (default /etc/rspamd); every
	// handler is a no-op when it does not exist (PHP parity).
	RspamdDir string
	// customTplDir is the .master override directory.
	customTplDir string
}

// NewRspamdPlugin creates the rspamd plugin on top of the shared
// plumbing.
func NewRspamdPlugin(base *Plugin, customTplDir string) *RspamdPlugin {
	return &RspamdPlugin{base: base, RspamdDir: "/etc/rspamd", customTplDir: customTplDir}
}

// Name identifies the plugin in logs and the registry.
func (*RspamdPlugin) Name() string { return "rspamd" }

// usersDir is where per-identity settings and wblist confs live.
func (r *RspamdPlugin) usersDir() string { return r.RspamdDir + "/local.d/users/" }

// localD is the local.d directory for server-level snippets.
func (r *RspamdPlugin) localD() string { return r.RspamdDir + "/local.d" }

// OnLoad subscribes the plugin (PHP registration table).
func (r *RspamdPlugin) OnLoad(reg *engine.Registry) error {
	subs := map[string]func(context.Context, string, engine.Data) error{}
	for _, table := range []string{"spamfilter_users", "mail_user", "mail_forwarding"} {
		for _, action := range []string{"insert", "update", "delete"} {
			subs[table+"_"+action] = r.userSettingsUpdate
		}
	}
	for _, ev := range []string{"spamfilter_wblist_insert", "spamfilter_wblist_update",
		"mail_access_insert", "mail_access_update"} {
		subs[ev] = r.wblistUpdate
	}
	for _, ev := range []string{"spamfilter_wblist_delete", "mail_access_delete"} {
		subs[ev] = r.wblistDelete
	}
	for _, ev := range []string{"server_insert", "server_update",
		"server_ip_insert", "server_ip_update", "server_ip_delete"} {
		subs[ev] = r.serverUpdate
	}
	for event, handler := range subs {
		h := handler
		if err := reg.RegisterEvent(event, func(ctx context.Context, ev string, data engine.Data) error {
			return h(ctx, ev, data)
		}); err != nil {
			return err
		}
	}
	return nil
}

// reloadRspamd queues the delayed reload when rspamd is the filter.
func (r *RspamdPlugin) reloadRspamd(cfg getconf.MailConfig) {
	if cfg.ContentFilter == "rspamd" {
		r.base.services.RestartServiceDelayed(RspamdService, engine.ActionReload)
	}
}

// idnEncode converts a (possibly @-prefixed) address or domain to its
// ASCII form; failures keep the input (PHP idn_encode behavior).
func idnEncode(v string) string {
	local, domain, found := strings.Cut(v, "@")
	if !found {
		if ascii, err := idna.Lookup.ToASCII(v); err == nil {
			return ascii
		}
		return v
	}
	if ascii, err := idna.Lookup.ToASCII(domain); err == nil {
		return local + "@" + ascii
	}
	return v
}

// settingsIdentity describes one per-identity settings target.
type settingsIdentity struct {
	typ      string // spamfilter_user | mail_user | mail_forwarding
	entryID  int64
	email    string // full address or @domain form
	isDomain bool
	name     string // settings file base name (pre-IDN)
}

// classifyIdentity resolves the event payload into a settings identity
// (rspamd_plugin::user_settings_update preamble). ok=false means skip.
func classifyIdentity(event string, useRow row) (settingsIdentity, bool) {
	id := settingsIdentity{}
	switch {
	case strings.HasPrefix(event, "spamfilter_users_"):
		id.typ, id.email, id.entryID = "spamfilter_user", useRow.str("email"), useRow.num("id")
	case strings.HasPrefix(event, "mail_forwarding_"):
		id.typ, id.email, id.entryID = "mail_forwarding", useRow.str("source"), useRow.num("forwarding_id")
	case strings.HasPrefix(event, "mail_user_"):
		id.typ, id.email, id.entryID = "mail_user", useRow.str("email"), useRow.num("mailuser_id")
	default:
		return id, false
	}
	if id.email == "@" || id.email == "*@" || id.email == "" {
		return id, false
	}
	id.name = id.email
	switch {
	case strings.HasPrefix(id.email, "@"):
		id.name = id.email[1:]
		id.isDomain = true
	case !strings.Contains(id.email, "@"):
		id.email = "@" + id.email
		id.isDomain = true
	}
	return id, id.name != ""
}

// settingsFile is the conf path of an identity (IDN-encoded, @ → _).
func (r *RspamdPlugin) settingsFile(name string) string {
	return r.usersDir() + strings.ReplaceAll(idnEncode(name), "@", "_") + ".conf"
}

// userSettingsUpdate writes or removes one identity settings file and
// fans domain identities out to children without their own
// spamfilter_users row.
func (r *RspamdPlugin) userSettingsUpdate(ctx context.Context, event string, data engine.Data) error {
	if !isDir(r.RspamdDir) {
		return nil
	}
	return r.userSettings(ctx, event, data, false)
}

func (r *RspamdPlugin) userSettings(ctx context.Context, event string, data engine.Data, internal bool) error {
	cfg, err := r.base.config(ctx)
	if err != nil {
		r.base.log.Warn("mail: using default [mail] config", "error", err)
	}
	mode := "update"
	useRow := row(data.New)
	switch {
	case strings.HasSuffix(event, "_delete"):
		mode = "delete"
		useRow = row(data.Old)
	case strings.HasSuffix(event, "_insert"):
		mode = "insert"
	}
	id, ok := classifyIdentity(event, useRow)
	if !ok {
		return nil
	}

	// IDN rename cleanup (PHP deletes the pre-IDN filename).
	if encoded := idnEncode(id.name); encoded != id.name {
		old := r.usersDir() + strings.ReplaceAll(id.name, "@", "_") + ".conf"
		_ = os.Remove(old)
	}
	file := r.settingsFile(id.name)

	if mode == "delete" {
		if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
			r.base.log.Warn("mail: could not remove rspamd settings", "file", file, "error", err)
		}
	} else {
		r.writeUserSettings(ctx, id, useRow, file)
	}

	// Domain identities fan out to children lacking their own
	// spamfilter_users row.
	if id.isDomain && r.base.db != nil {
		var users []map[string]any
		err := r.base.db.WithContext(ctx).
			Raw("SELECT mu.* FROM mail_user mu LEFT JOIN spamfilter_users su ON su.email = mu.email "+
				"WHERE mu.email LIKE ? AND su.id IS NULL", "%"+id.email).Scan(&users).Error
		if err != nil {
			r.base.log.Error("mail: rspamd domain fan-out query failed", "error", err)
		}
		for _, u := range users {
			if err := r.userSettings(ctx, "mail_user_"+mode, engine.Data{Old: u, New: u}, true); err != nil {
				return err
			}
		}
		var fwds []map[string]any
		err = r.base.db.WithContext(ctx).
			Raw("SELECT mf.* FROM mail_forwarding mf LEFT JOIN spamfilter_users su ON su.email = mf.source "+
				"WHERE mf.source LIKE ? AND su.id IS NULL", "%_"+id.email).Scan(&fwds).Error
		if err != nil {
			r.base.log.Error("mail: rspamd domain fan-out query failed", "error", err)
		}
		for _, f := range fwds {
			if err := r.userSettings(ctx, "mail_forwarding_"+mode, engine.Data{Old: f, New: f}, true); err != nil {
				return err
			}
		}
	}

	if !internal {
		r.reloadRspamd(cfg)
	}
	return nil
}

// policyRow is the subset of spamfilter_policy the settings need.
type policyRow struct {
	RspamdGreylisting          string
	RspamdSpamGreylistingLevel *float64
	RspamdSpamTagLevel         *float64
	RspamdSpamTagMethod        string
	RspamdSpamKillLevel        *float64
	SpamLover                  string
	VirusLover                 string
}

// resolvePolicy finds the effective policy for an identity (PHP lookup
// chains). nil means no policy applies.
func (r *RspamdPlugin) resolvePolicy(ctx context.Context, id settingsIdentity, useRow row) *policyRow {
	if r.base.db == nil {
		return nil
	}
	var pol policyRow
	if id.typ == "spamfilter_user" {
		if pid := useRow.num("policy_id"); pid > 0 {
			err := r.base.db.WithContext(ctx).Table("spamfilter_policy").
				Where("id = ?", pid).Take(&pol).Error
			if err != nil {
				return nil
			}
			return &pol
		}
		domain := id.email[strings.Index(id.email, "@"):]
		err := r.base.db.WithContext(ctx).Table("spamfilter_users u").
			Select("p.*").
			Joins("INNER JOIN spamfilter_policy p ON p.id = u.policy_id").
			Where("u.server_id = ? AND u.email = ?", r.base.serverID, domain).
			Take(&pol).Error
		if err != nil {
			return nil
		}
		return &pol
	}
	domain := id.email[strings.Index(id.email, "@"):]
	err := r.base.db.WithContext(ctx).Table("spamfilter_users u").
		Select("p.*").
		Joins("INNER JOIN spamfilter_policy p ON p.id = u.policy_id").
		Where("u.server_id = ? AND u.email IN ?", r.base.serverID, []string{id.email, domain}).
		Order("u.priority DESC").Limit(1).
		Take(&pol).Error
	if err != nil {
		return nil
	}
	return &pol
}

// writeUserSettings renders one settings conf (PHP template block). For
// spamfilter_user rows without a valid email or policy the file is
// removed; for mailbox/forwarding identities without any applicable
// policy the file is removed too (the PHP outer type-check made those
// writes dead code — the spec's child re-render supersedes it).
func (r *RspamdPlugin) writeUserSettings(ctx context.Context, id settingsIdentity, useRow row, file string) {
	pol := r.resolvePolicy(ctx, id, useRow)
	invalidSpamfilter := id.typ == "spamfilter_user" &&
		(!validEmail(idnEncode(id.email)) || useRow.num("policy_id") == 0)
	if invalidSpamfilter || pol == nil {
		if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
			r.base.log.Warn("mail: could not remove rspamd settings", "file", file, "error", err)
		}
		return
	}

	greylisting := pol.RspamdGreylisting
	if greylisting == "" {
		greylisting = "n"
	}
	if id.typ != "spamfilter_user" && useRow.str("greylisting") == "y" {
		greylisting = "y"
	} else if id.typ == "spamfilter_user" && !id.isDomain && r.base.db != nil {
		// Address-level spamfilter user: an explicitly greylisted
		// mailbox/forwarding wins over the policy.
		var g string
		err := r.base.db.WithContext(ctx).
			Raw("SELECT greylisting FROM mail_user WHERE server_id = ? AND email = ? "+
				"UNION SELECT greylisting FROM mail_forwarding WHERE server_id = ? AND source = ? "+
				"ORDER BY (greylisting = 'y') DESC LIMIT 1",
				r.base.serverID, id.email, r.base.serverID, id.email).Scan(&g).Error
		if err == nil && g == "y" {
			greylisting = "y"
		}
	}

	priority := int64(20)
	if id.isDomain {
		priority = 10
	}
	if _, ok := useRow["priority"]; ok {
		priority += useRow.num("priority")
	} else {
		priority += 5
	}

	src, _, err := mastertpl.Load("rspamd_users.inc.conf.master", r.customTplDir)
	if err != nil {
		r.base.log.Error("mail: loading rspamd users template failed", "error", err)
		return
	}
	tpl := mastertpl.New(src)
	tpl.SetVar("record_identifier", "ispc_"+id.typ+"_"+strconv.FormatInt(id.entryID, 10))
	tpl.SetVar("priority", strconv.FormatInt(priority, 10))
	if id.typ == "spamfilter_user" && useRow.str("local") != "Y" {
		tpl.SetVar("from_email", idnEncode(id.email))
	} else {
		tpl.SetVar("to_email", idnEncode(id.email))
	}

	tagLevel, killLevel := 6.0, 15.0
	tagMethod := "add_header"
	if pol.RspamdSpamTagLevel != nil {
		tagLevel = *pol.RspamdSpamTagLevel
	}
	if pol.RspamdSpamKillLevel != nil {
		killLevel = *pol.RspamdSpamKillLevel
	}
	if pol.RspamdSpamTagMethod != "" {
		tagMethod = pol.RspamdSpamTagMethod
	}
	tpl.SetVar("rspamd_spam_tag_level", formatFloat(tagLevel))
	tpl.SetVar("rspamd_spam_tag_method", tagMethod)
	tpl.SetVar("rspamd_spam_kill_level", formatFloat(killLevel))
	tpl.SetVar("rspamd_virus_kill_level", formatFloat(killLevel+1000))
	if pol.SpamLover == "Y" {
		tpl.SetVar("spam_lover", "1")
	}
	if pol.VirusLover == "Y" {
		tpl.SetVar("virus_lover", "1")
	}
	tpl.SetVar("greylisting", greylisting)
	greylistLevel := 0.1
	if pol.RspamdSpamGreylistingLevel != nil {
		greylistLevel = *pol.RspamdSpamGreylistingLevel
	}
	tpl.SetVar("greylisting_level", formatFloat(greylistLevel))

	out, err := tpl.Render()
	if err != nil {
		r.base.log.Error("mail: rendering rspamd settings failed", "error", err)
		return
	}
	if err := os.MkdirAll(r.usersDir(), 0o755); err != nil {
		r.base.log.Error("mail: creating rspamd users dir failed", "error", err)
		return
	}
	if err := os.WriteFile(file, []byte(out), 0o644); err != nil {
		r.base.log.Error("mail: writing rspamd settings failed", "file", file, "error", err)
	}
}

// formatFloat renders a float like PHP floatval interpolation (6 → "6",
// 6.5 → "6.5").
func formatFloat(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// validEmail ports rspamd_plugin::isValidEmail closely enough for the
// filter decision: an @ with sane local and domain parts.
func validEmail(email string) bool {
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return false
	}
	local, domain := email[:at], email[at+1:]
	if len(local) > 64 || domain == "" || len(domain) > 255 {
		return false
	}
	if strings.HasPrefix(local, ".") || strings.HasSuffix(local, ".") ||
		strings.Contains(local, "..") || strings.Contains(domain, "..") {
		return false
	}
	for _, part := range strings.Split(domain, ".") {
		if part == "" {
			return false
		}
		for _, c := range part {
			ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-'
			if !ok {
				return false
			}
		}
	}
	if !strings.Contains(domain, ".") {
		return false
	}
	// Empty local means an @domain form: valid for wblist filters.
	return true
}

// serverLocalD are the local.d files regenerated from mail getconf and
// server IPs on server / server_ip events (rspamd_plugin::server_update).
var serverLocalD = []string{
	"dkim_signing.conf", "options.inc", "redis.conf", "classifier-bayes.conf",
}

// serverUpdate regenerates the server-level Rspamd snippets and queues
// a reload.
func (r *RspamdPlugin) serverUpdate(ctx context.Context, _ string, _ engine.Data) error {
	if !isDir(r.RspamdDir) {
		return nil
	}
	cfg, err := r.base.config(ctx)
	if err != nil {
		r.base.log.Warn("mail: using default [mail] config", "error", err)
	}

	var addrs []map[string]any
	if r.base.db != nil {
		var ips []string
		err := r.base.db.WithContext(ctx).Table("server_ip").
			Where("server_id = ?", r.base.serverID).Pluck("ip_address", &ips).Error
		if err != nil {
			r.base.log.Error("mail: loading server IPs failed", "error", err)
		}
		for _, ip := range ips {
			addrs = append(addrs, map[string]any{
				"ip":        ip,
				"quoted_ip": "\"" + ip + "\",\n",
			})
		}
	}

	for _, f := range serverLocalD {
		src, _, err := mastertpl.Load("rspamd_"+f+".master", r.customTplDir)
		if err != nil {
			r.base.log.Error("mail: loading rspamd template failed", "template", f, "error", err)
			continue
		}
		tpl := mastertpl.New(src)
		tpl.SetVar("dkim_path", cfg.DKIMPath)
		tpl.SetVar("rspamd_redis_servers", cfg.RspamdRedisServers)
		tpl.SetVar("rspamd_redis_password", cfg.RspamdRedisPasswd)
		tpl.SetVar("rspamd_redis_bayes_servers", cfg.RspamdRedisBayesServers)
		tpl.SetVar("rspamd_redis_bayes_password", cfg.RspamdRedisBayesPasswd)
		if len(addrs) > 0 {
			tpl.SetLoop("local_addrs", addrs)
		}
		out, err := tpl.Render()
		if err != nil {
			r.base.log.Error("mail: rendering rspamd template failed", "template", f, "error", err)
			continue
		}
		if err := os.WriteFile(r.localD()+"/"+f, []byte(out), 0o644); err != nil {
			r.base.log.Error("mail: writing rspamd config failed", "file", f, "error", err)
		}
	}

	// Protect password-bearing files (PHP chgrp _rspamd + chmod 640).
	protect := []string{r.localD() + "/redis.conf", r.localD() + "/classifier-bayes.conf"}
	for _, f := range protect {
		if _, err := r.base.runner.Run(ctx, "chgrp", "_rspamd", f); err != nil {
			r.base.log.Warn("mail: chgrp rspamd config failed", "file", f, "error", err)
		}
		if err := os.Chmod(f, 0o640); err != nil {
			r.base.log.Warn("mail: chmod rspamd config failed", "file", f, "error", err)
		}
	}
	r.reloadRspamd(cfg)
	return nil
}
