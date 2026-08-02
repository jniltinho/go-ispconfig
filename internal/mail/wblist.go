package mail

import (
	"context"
	"net"
	"os"
	"strconv"
	"strings"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/mastertpl"
)

// wblistFilter is the normalized wblist vector (rspamd_plugin
// spamfilter_wblist_update $filter array).
type wblistFilter struct {
	wb       string
	from     string
	rcpt     string
	ip       string
	hostname string
}

// wblistFile is the conf path of one wblist row.
func (r *RspamdPlugin) wblistFile(global bool, recordID int64) string {
	prefix := "spamfilter_wblist_"
	if global {
		prefix = "global_wblist_"
	}
	return r.usersDir() + prefix + strconv.FormatInt(recordID, 10) + ".conf"
}

// normalizeWblistAddr applies the PHP address massaging: bare domains
// gain '@', '*@' collapses to '@', invalid results become ”.
func normalizeWblistAddr(v string) string {
	if v == "" {
		return ""
	}
	switch {
	case !strings.Contains(v, "@"):
		v = "@" + v
	case strings.HasPrefix(v, "*@"):
		v = v[1:]
	}
	if !validEmail(v) {
		return ""
	}
	return v
}

// wblistUpdate renders or removes one wblist conf (port of
// spamfilter_wblist_update, also handling mail_access rows).
func (r *RspamdPlugin) wblistUpdate(ctx context.Context, event string, data engine.Data) error {
	if !isDir(r.RspamdDir) {
		return nil
	}
	cfg, err := r.base.config(ctx)
	if err != nil {
		r.base.log.Warn("mail: using default [mail] config", "error", err)
	}
	newRow := row(data.New)

	global := strings.HasPrefix(event, "mail_access_")
	var recordID int64
	var filter *wblistFilter
	if global {
		recordID = newRow.num("access_id")
		source := newRow.str("source")
		wb := "B"
		if newRow.str("access") == "OK" {
			wb = "W"
		}
		f := wblistFilter{wb: wb}
		switch newRow.str("type") {
		case "sender":
			f.from = idnEncode(source)
		case "recipient":
			f.rcpt = idnEncode(source)
		case "client":
			if net.ParseIP(source) != nil {
				f.ip = source
			} else {
				f.hostname = source
			}
		}
		filter = &f
	} else {
		recordID = newRow.num("wblist_id")
		if r.base.db != nil {
			var email string
			err := r.base.db.WithContext(ctx).Table("spamfilter_users").
				Select("email").Where("id = ?", newRow.num("rid")).Scan(&email).Error
			if err == nil && email != "" {
				filter = &wblistFilter{
					wb:   newRow.str("wb"),
					from: idnEncode(newRow.str("email")),
					rcpt: idnEncode(email),
				}
			}
		}
	}
	file := r.wblistFile(global, recordID)

	remove := func() {
		if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
			r.base.log.Warn("mail: could not remove wblist conf", "file", file, "error", err)
		}
	}

	if newRow.str("active") != "y" || filter == nil {
		remove()
		r.reloadRspamd(cfg)
		return nil
	}

	from := normalizeWblistAddr(filter.from)
	rcpt := normalizeWblistAddr(filter.rcpt)
	invalid := (global && from == "" && rcpt == "" && filter.ip == "" && filter.hostname == "") ||
		(!global && (from == "" || rcpt == ""))
	if invalid {
		remove()
		r.reloadRspamd(cfg)
		return nil
	}

	scope := "spamfilter"
	prioOffset := int64(40)
	if global {
		scope = "global"
		prioOffset = 30
	}
	src, _, err := mastertpl.Load("rspamd_wblist.inc.conf.master", r.customTplDir)
	if err != nil {
		return err
	}
	tpl := mastertpl.New(src)
	tpl.SetVar("list_scope", scope)
	tpl.SetVar("record_id", strconv.FormatInt(recordID, 10))
	tpl.SetVar("priority", strconv.FormatInt(newRow.num("priority")+prioOffset, 10))
	tpl.SetVar("from", from)
	tpl.SetVar("recipient", rcpt)
	tpl.SetVar("hostname", filter.hostname)
	tpl.SetVar("ip", filter.ip)
	tpl.SetVar("wblist", filter.wb)
	out, err := tpl.Render()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(r.usersDir(), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(file, []byte(out), 0o644); err != nil {
		r.base.log.Error("mail: writing wblist conf failed", "file", file, "error", err)
	}
	r.reloadRspamd(cfg)
	return nil
}

// wblistDelete removes the conf of a deleted row.
func (r *RspamdPlugin) wblistDelete(ctx context.Context, event string, data engine.Data) error {
	if !isDir(r.RspamdDir) {
		return nil
	}
	cfg, err := r.base.config(ctx)
	if err != nil {
		r.base.log.Warn("mail: using default [mail] config", "error", err)
	}
	oldRow := row(data.Old)
	global := event == "mail_access_delete"
	id := oldRow.num("wblist_id")
	if global {
		id = oldRow.num("access_id")
	}
	file := r.wblistFile(global, id)
	if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
		r.base.log.Warn("mail: could not remove wblist conf", "file", file, "error", err)
	}
	r.reloadRspamd(cfg)
	return nil
}
