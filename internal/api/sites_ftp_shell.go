package api

// FTP and shell user entities of the sites module (openspec change
// add-ftp-shell-module, tasks 5.1–5.3): the Go port of ftp_user.tform.php /
// shell_user.tform.php and their edit.php prepare hooks, mounted under
// /api/sites/ftp-users and /api/sites/shell-users.

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/labstack/echo/v5"
	"gorm.io/gorm"

	"go-ispconfig/internal/getconf"
	"go-ispconfig/internal/model"
	"go-ispconfig/internal/repository"
	"go-ispconfig/internal/shell"
	"go-ispconfig/internal/system"
	"go-ispconfig/internal/validator"
)

// registerSitesFTPShellEntities mounts the FTP and shell user entities.
func registerSitesFTPShellEntities(g *echo.Group, d *Deps) error {
	if err := RegisterEntity[model.FTPUser](g, d, ftpUserEntity()); err != nil {
		return err
	}
	return RegisterEntity[model.ShellUser](g, d, shellUserEntity())
}

// --- FTP users ---

// ftpUserEntity is the declarative definition of the ftp_user form.
func ftpUserEntity() *Entity {
	adminInt := func(name, label string, def any) Field {
		f := intField(name, label, def)
		f.AdminOnly = true
		return f
	}
	adminText := func(name, label string, rules ...validator.Rule) Field {
		f := text(name, label, rules...)
		f.AdminOnly = true
		return f
	}
	return &Entity{
		Name:        "ftp-users",
		Title:       "ftp_user_edit_title",
		Prepare:     ftpUserPrepare,
		AfterInsert: ftpUserAfterInsert,
		BeforeUpdate: ftpUserBeforeUpdate,
		Decorate:    ftpShellDecorate("ftp_user", "ftp_user_id"),
		ListFilters: map[string]ListFilterFunc{
			"_server_name":   relatedNameFilter("server_id", "server", "server_id", "server_name"),
			"_parent_domain": relatedNameFilter("parent_domain_id", "web_domain", "domain_id", "domain"),
		},
		Tabs: []Tab{
			{
				Name: "ftp", Label: "ftp_user_tab_txt",
				Fields: []Field{
					selectField("parent_domain_id", "parent_domain_id_txt", "INTEGER", nil, nil,
						validator.Rule{Type: "ISPOSITIVE", ErrKey: "parent_domain_id_error_empty"}),
					// server_id is derived from the parent site (Prepare).
					selectField("server_id", "server_id_txt", "INTEGER", nil, nil),
					text("username", "username_txt",
						validator.Rule{Type: "NOTEMPTY", ErrKey: "username_error_empty"},
						validator.Rule{Type: "UNIQUE", ErrKey: "username_error_unique"},
						regex(`^[\w.\-@+]{1,64}$`, "username_error_regex")),
					// username_prefix is set by Prepare; display-only on the form.
					text("username_prefix", "username_prefix_txt"),
					{Name: "password", Label: "password_txt", Datatype: "VARCHAR",
						Formtype: "PASSWORD"},
					intField("quota_size", "quota_size_txt", float64(-1),
						validator.Rule{Type: "NOTEMPTY", ErrKey: "quota_size_error_empty"},
						regex(`^(-1|[0-9]{1,10})$`, "quota_size_error_regex")),
					checkbox("active", "active_txt", "y"),
				},
			},
			{
				// Options tab is visible to clients (dir only) and admins
				// (uid/gid/ratios/bandwidth) — matches ftp_user.tform.php.
				Name: "advanced", Label: "limit_parameters_txt",
				Fields: []Field{
					text("dir", "directory_txt",
						validator.Rule{Type: "NOTEMPTY", ErrKey: "directory_error_empty"},
						regex(`^/[-a-zA-Z0-9_/.~ ]+$`, "directory_error_regex")),
					// uid/gid are admin-editable but derived from the parent
					// site for clients (Prepare + AfterInsert). No NOTEMPTY:
					// AdminOnly fields are stripped from client bodies before
					// validate, and AfterInsert always stamps them.
					adminText("uid", "uid_txt"),
					adminText("gid", "gid_txt"),
					// PHP advanced defaults are 0 (not -1) for ratio/bandwidth.
					adminInt("quota_files", "quota_files_txt", float64(0)),
					adminInt("ul_ratio", "ul_ratio_txt", float64(0)),
					adminInt("dl_ratio", "dl_ratio_txt", float64(0)),
					adminInt("ul_bandwidth", "ul_bandwidth_txt", float64(0)),
					adminInt("dl_bandwidth", "dl_bandwidth_txt", float64(0)),
					// expires is free-form date; GORM maps *time.Time.
					{Name: "expires", Label: "expires_txt", Datatype: "VARCHAR", Formtype: "TEXT"},
				},
			},
		},
	}
}

// ftpUserPrepare ports ftp_user_edit.php: parent-site ownership, prefix
// application, CRYPT password, dir under docroot, admin-only advanced
// fields, and derivation of server_id / uid / gid / sys_groupid.
func ftpUserPrepare(c *echo.Context, d *Deps, id *repository.Identity, body map[string]any) error {
	ctx := c.Request().Context()
	fields := map[string][]string{}
	create := c.Param("id") == ""

	var old *model.FTPUser
	if !create {
		old = &model.FTPUser{}
		if err := d.DB.WithContext(ctx).Take(old, c.Param("id")).Error; err != nil {
			return err
		}
	}

	pid := bodyInt(body, "parent_domain_id")
	if pid == 0 && old != nil {
		pid = int64(old.ParentDomainID)
	}
	var parent *model.WebDomain
	if pid <= 0 {
		fields["parent_domain_id"] = append(fields["parent_domain_id"], "parent_domain_id_error_empty")
	} else {
		var err error
		parent, err = loadOwned[model.WebDomain](c, d, id, pid)
		if err != nil {
			return err
		}
		if parent.Type != "vhost" {
			fields["parent_domain_id"] = append(fields["parent_domain_id"], "parent_domain_id_error_invalid")
		}
	}

	// Prefix application (create keeps the expanded template; update keeps
	// the stored prefix so a later template change does not rename users).
	global := sitesGlobalConfig(d.DB)
	expandedPrefix := expandSitesPrefix(ctx, d.DB, id, global["ftpuser_prefix"], body)
	prefix := expandedPrefix
	if old != nil {
		prefix = keepStoredPrefix(old.UsernamePrefix, expandedPrefix)
	}
	if suffix, ok := body["username"].(string); ok {
		// Clients submit the unprefixed name; re-apply the prefix.
		suffix = strings.TrimSpace(suffix)
		if prefix != "" && strings.HasPrefix(suffix, prefix) {
			suffix = strings.TrimPrefix(suffix, prefix)
		}
		body["username"] = prefix + suffix
		body["username_prefix"] = prefix
	}

	// Password: required on create; empty on update means "unchanged".
	pw, _ := body["password"].(string)
	switch {
	case pw == "" && create:
		fields["password"] = append(fields["password"], "password_error_empty")
	case pw == "":
		delete(body, "password")
	default:
		if key := checkPasswordPolicy(d.DB, pw); key != "" {
			fields["password"] = append(fields["password"], key)
		} else if err := cryptBodyPassword(body, "password"); err != nil {
			return err
		}
	}

	if parent != nil {
		body["server_id"] = float64(parent.ServerID)
		body["parent_domain_id"] = float64(parent.DomainID)
		// Non-admins never set uid/gid; always derive from the site.
		if !id.IsAdmin() || create || body["uid"] == nil || body["uid"] == "" {
			body["uid"] = parent.SystemUser
		}
		if !id.IsAdmin() || create || body["gid"] == nil || body["gid"] == "" {
			body["gid"] = parent.SystemGroup
		}
		// Default dir to the document root (or keep stored on update).
		dir, _ := body["dir"].(string)
		if dir == "" {
			if old != nil && old.Dir != "" {
				dir = old.Dir
			} else {
				dir = parent.DocumentRoot
			}
			body["dir"] = dir
		}
		dir = filepath.Clean(strings.TrimSpace(dir))
		body["dir"] = dir
		if strings.Contains(dir, "..") || strings.Contains(dir, "./") {
			fields["dir"] = append(fields["dir"], "directory_error_regex")
		} else if !system.UnderDocroot(dir, parent.DocumentRoot) {
			// PHP resets non-admin escapes to the docroot; we reject so
			// the operator sees the mistake.
			fields["dir"] = append(fields["dir"], "directory_error_notinweb")
		}
		// Ownership follows the parent site (sys_groupid on insert is
		// stamped by the repository from the identity; AfterInsert will
		// re-assert the site's group when needed).
		if create {
			body["user_type"] = "user"
		}
	}

	// Admin-only advanced fields are stripped for non-admins by the
	// entity framework; still reset non-admin uid/gid on update.
	if !id.IsAdmin() && parent != nil {
		body["uid"] = parent.SystemUser
		body["gid"] = parent.SystemGroup
	}

	if len(fields) > 0 {
		return &ValidationError{Fields: fields}
	}
	return nil
}

// ftpUserAfterInsert re-asserts ownership and defaults from the parent site
// (ftp_user_edit.php onAfterInsert). The insert already wrote the row, so
// this issues an UPDATE for the derived columns before the datalog insert.
func ftpUserAfterInsert(ctx context.Context, tx *gorm.DB, _ *repository.Identity, recAny any, _ map[string]any) error {
	rec, ok := recAny.(*model.FTPUser)
	if !ok {
		return nil
	}
	if err := stampFTPUserParent(ctx, tx, rec); err != nil {
		return err
	}
	return tx.Model(rec).Select("sys_groupid", "server_id", "uid", "gid", "dir").Updates(rec).Error
}

// ftpUserBeforeUpdate re-derives ownership when the parent site changes
// (ftp_user_edit.php onAfterUpdate parent-change branch). Mutates the
// in-memory record; the repository Save persists it.
func ftpUserBeforeUpdate(ctx context.Context, tx *gorm.DB, _ *repository.Identity, _ map[string]any, oldAny, recAny any) error {
	old, _ := oldAny.(*model.FTPUser)
	rec, ok := recAny.(*model.FTPUser)
	if !ok || rec == nil {
		return nil
	}
	if old != nil && old.ParentDomainID == rec.ParentDomainID {
		return nil
	}
	return stampFTPUserParent(ctx, tx, rec)
}

// stampFTPUserParent copies server_id/uid/gid/sys_groupid (and empty dir)
// from the parent web_domain onto an FTP user record.
func stampFTPUserParent(ctx context.Context, tx *gorm.DB, rec *model.FTPUser) error {
	if rec == nil || rec.ParentDomainID == 0 {
		return nil
	}
	var parent model.WebDomain
	if err := tx.WithContext(ctx).Where("domain_id = ?", rec.ParentDomainID).Take(&parent).Error; err != nil {
		return err
	}
	rec.SysGroupID = parent.SysGroupID
	rec.ServerID = parent.ServerID
	if rec.UID == "" {
		rec.UID = parent.SystemUser
	}
	if rec.GID == "" {
		rec.GID = parent.SystemGroup
	}
	if rec.Dir == "" {
		rec.Dir = parent.DocumentRoot
	}
	return nil
}

// --- Shell users ---

// shellUserEntity is the declarative definition of the shell_user form.
func shellUserEntity() *Entity {
	return &Entity{
		Name:        "shell-users",
		Title:       "shell_user_edit_title",
		Prepare:     shellUserPrepare,
		AfterInsert: shellUserAfterInsert,
		Decorate:    ftpShellDecorate("shell_user", "shell_user_id"),
		ListFilters: map[string]ListFilterFunc{
			"_server_name":   relatedNameFilter("server_id", "server", "server_id", "server_name"),
			"_parent_domain": relatedNameFilter("parent_domain_id", "web_domain", "domain_id", "domain"),
		},
		Tabs: []Tab{
			{
				Name: "shell", Label: "shell_user_tab_txt",
				Fields: []Field{
					selectField("parent_domain_id", "parent_domain_id_txt", "INTEGER", nil, nil,
						validator.Rule{Type: "ISPOSITIVE", ErrKey: "parent_domain_id_error_empty"}),
					selectField("server_id", "server_id_txt", "INTEGER", nil, nil),
					text("username", "username_txt",
						validator.Rule{Type: "NOTEMPTY", ErrKey: "username_error_empty"},
						validator.Rule{Type: "UNIQUE", ErrKey: "username_error_unique"},
						regex(`^[\w.\-]{1,32}$`, "username_error_regex")),
					text("username_prefix", "username_prefix_txt"),
					{Name: "password", Label: "password_txt", Datatype: "VARCHAR",
						Formtype: "PASSWORD"},
					selectField("chroot", "chroot_txt", "VARCHAR", "", []Option{
						{Value: "", Label: "no_txt"},
						{Value: "no", Label: "no_txt"},
						{Value: "jailkit", Label: "jailkit_txt"},
					}),
					intField("quota_size", "quota_size_txt", float64(-1),
						validator.Rule{Type: "NOTEMPTY", ErrKey: "quota_size_error_empty"},
						regex(`^(-1|[0-9]{1,10})$`, "quota_size_error_regex")),
					checkbox("active", "active_txt", "y"),
					textarea("ssh_rsa", "ssh_rsa_txt"),
				},
			},
			{
				// Admin-only Options tab (puser/pgroup/shell/dir).
				Name: "advanced", Label: "limit_parameters_txt", AdminOnly: true,
				Fields: []Field{
					// puser/pgroup/shell/dir are admin Options fields; clients
					// never see this tab (AdminOnly). Empty is allowed so a
					// client create passes validate — Prepare/AfterInsert
					// stamp the parent site values after the row exists.
					text("puser", "puser_txt"),
					text("pgroup", "pgroup_txt"),
					text("shell", "shell_txt",
						regex(`^(|/bin/(ba)?sh|/bin/false|/usr/bin/(ba)?sh|/usr/sbin/jk_chrootsh|/bin/rbash)$`,
							"shell_error_regex")),
					text("dir", "directory_txt",
						regex(`^(|/[-a-zA-Z0-9_/.~ ]+)$`, "directory_error_regex")),
				},
			},
		},
	}
}

// shellUserPrepare ports shell_user_edit.php: parent locked after create,
// blacklist, 32-char cap after prefix, chroot allow-list, ssh_authentication
// mode, CRYPT password, dir under docroot, admin-only advanced fields.
func shellUserPrepare(c *echo.Context, d *Deps, id *repository.Identity, body map[string]any) error {
	ctx := c.Request().Context()
	fields := map[string][]string{}
	create := c.Param("id") == ""

	var old *model.ShellUser
	if !create {
		old = &model.ShellUser{}
		if err := d.DB.WithContext(ctx).Take(old, c.Param("id")).Error; err != nil {
			return err
		}
		// Parent domain is immutable after create (PHP edit_disabled).
		if v := bodyInt(body, "parent_domain_id"); v != 0 && uint32(v) != old.ParentDomainID {
			fields["parent_domain_id"] = append(fields["parent_domain_id"], "parent_domain_id_error_immutable")
		}
		body["parent_domain_id"] = float64(old.ParentDomainID)
	}

	pid := bodyInt(body, "parent_domain_id")
	if pid == 0 && old != nil {
		pid = int64(old.ParentDomainID)
	}
	var parent *model.WebDomain
	if pid <= 0 {
		fields["parent_domain_id"] = append(fields["parent_domain_id"], "parent_domain_id_error_empty")
	} else {
		var err error
		parent, err = loadOwned[model.WebDomain](c, d, id, pid)
		if err != nil {
			return err
		}
		if parent.Type != "vhost" {
			fields["parent_domain_id"] = append(fields["parent_domain_id"], "parent_domain_id_error_invalid")
		}
	}

	global := sitesGlobalConfig(d.DB)
	expandedPrefix := expandSitesPrefix(ctx, d.DB, id, global["shelluser_prefix"], body)
	prefix := expandedPrefix
	if old != nil {
		prefix = keepStoredPrefix(old.UsernamePrefix, expandedPrefix)
	}

	if suffix, ok := body["username"].(string); ok {
		suffix = strings.TrimSpace(suffix)
		// Blacklist checks the operator-typed name (before prefix).
		checkName := suffix
		if prefix != "" && strings.HasPrefix(suffix, prefix) {
			checkName = strings.TrimPrefix(suffix, prefix)
		}
		if shell.Blacklisted(checkName) || shell.Blacklisted(prefix+checkName) {
			fields["username"] = append(fields["username"], "username_error_blacklist")
		}
		full := prefix + checkName
		if len(full) > 32 {
			fields["username"] = append(fields["username"], "username_error_len")
		}
		body["username"] = full
		body["username_prefix"] = prefix
	}

	// chroot allow-list from the owning client's ssh_chroot (comma list).
	if chroot, ok := body["chroot"].(string); ok {
		chroot = strings.TrimSpace(chroot)
		if chroot == "no" {
			chroot = ""
		}
		body["chroot"] = chroot
		if chroot != "" && chroot != "jailkit" {
			fields["chroot"] = append(fields["chroot"], "chroot_error_regex")
		}
		if !id.IsAdmin() && chroot == "jailkit" {
			if allowed, err := clientSSHChroot(ctx, d.DB, id); err != nil {
				return err
			} else if !allowed {
				fields["chroot"] = append(fields["chroot"], "chroot_error_notallowed")
			}
		}
	}

	// ssh_authentication mode from system config (sites or misc).
	authMode := sshAuthMode(d.DB)
	pw, _ := body["password"].(string)
	sshKey, _ := body["ssh_rsa"].(string)
	sshKey = strings.TrimSpace(sshKey)
	body["ssh_rsa"] = sshKey

	switch authMode {
	case "password":
		body["ssh_rsa"] = ""
	case "key":
		// Key-only: clear password on save.
		if create || pw != "" || sshKey != "" {
			body["password"] = ""
			pw = ""
		}
		if create && sshKey == "" {
			fields["ssh_rsa"] = append(fields["ssh_rsa"], "ssh_rsa_error_empty")
		}
	}

	switch {
	case pw == "" && create && authMode != "key":
		fields["password"] = append(fields["password"], "password_error_empty")
	case pw == "":
		delete(body, "password")
	default:
		if key := checkPasswordPolicy(d.DB, pw); key != "" {
			fields["password"] = append(fields["password"], key)
		} else if err := cryptBodyPassword(body, "password"); err != nil {
			return err
		}
	}

	if parent != nil {
		body["server_id"] = float64(parent.ServerID)
		body["parent_domain_id"] = float64(parent.DomainID)
		if create || !id.IsAdmin() {
			body["puser"] = parent.SystemUser
			body["pgroup"] = parent.SystemGroup
		}
		if create {
			if body["shell"] == nil || body["shell"] == "" {
				body["shell"] = "/bin/bash"
			}
			if body["dir"] == nil || body["dir"] == "" {
				body["dir"] = parent.DocumentRoot
			}
		}
		dir, _ := body["dir"].(string)
		if dir == "" && old != nil {
			dir = old.Dir
			body["dir"] = dir
		}
		if dir != "" {
			dir = filepath.Clean(strings.TrimSpace(dir))
			body["dir"] = dir
			if strings.Contains(dir, "..") || strings.Contains(dir, "./") {
				fields["dir"] = append(fields["dir"], "directory_error_regex")
			} else if !system.UnderDocroot(dir, parent.DocumentRoot) {
				fields["dir"] = append(fields["dir"], "directory_error_notinweb")
			}
		}
	}

	if len(fields) > 0 {
		return &ValidationError{Fields: fields}
	}
	return nil
}

// shellUserAfterInsert re-asserts ownership from the parent site so the
// row lands under the site's group (PHP onAfterInsert).
func shellUserAfterInsert(ctx context.Context, tx *gorm.DB, _ *repository.Identity, recAny any, _ map[string]any) error {
	rec, ok := recAny.(*model.ShellUser)
	if !ok || rec.ParentDomainID == 0 {
		return nil
	}
	var parent model.WebDomain
	if err := tx.WithContext(ctx).Where("domain_id = ?", rec.ParentDomainID).Take(&parent).Error; err != nil {
		return err
	}
	rec.SysGroupID = parent.SysGroupID
	rec.ServerID = parent.ServerID
	if rec.PUser == "" {
		rec.PUser = parent.SystemUser
	}
	if rec.PGroup == "" {
		rec.PGroup = parent.SystemGroup
	}
	if rec.Dir == "" {
		rec.Dir = parent.DocumentRoot
	}
	if rec.Shell == "" {
		rec.Shell = "/bin/bash"
	}
	return tx.Model(rec).Select("sys_groupid", "server_id", "puser", "pgroup", "dir", "shell").Updates(rec).Error
}

// ftpShellDecorate redacts passwords, attaches datalog state, and adds
// legacy-panel display names (_server_name, _parent_domain) for list UI.
func ftpShellDecorate(table, pk string) func(ctx context.Context, db *gorm.DB, items []map[string]any) error {
	state := datalogStateDecorator(table, pk)
	return func(ctx context.Context, db *gorm.DB, items []map[string]any) error {
		for _, it := range items {
			delete(it, "password")
		}
		if err := state(ctx, db, items); err != nil {
			return err
		}
		servers := nameLookup(ctx, db, "server", "server_id", "server_name",
			collectIDs(items, "server_id"))
		domains := nameLookup(ctx, db, "web_domain", "domain_id", "domain",
			collectIDs(items, "parent_domain_id"))
		for _, item := range items {
			item["_server_name"] = servers[idString(item["server_id"])]
			item["_parent_domain"] = domains[idString(item["parent_domain_id"])]
		}
		return nil
	}
}

// sshAuthMode reads the allowed SSH authentication mode from system config
// (sites.ssh_authentication, falling back to misc). Empty means both.
func sshAuthMode(db *gorm.DB) string {
	if db == nil {
		return ""
	}
	sections, err := getconf.GetGlobalConfig(db)
	if err != nil {
		return ""
	}
	if v := sections["sites"]["ssh_authentication"]; v != "" {
		return v
	}
	return sections["misc"]["ssh_authentication"]
}

// clientSSHChroot reports whether the caller's client is allowed to pick
// jailkit (client.ssh_chroot contains "jailkit", or is empty for admin-like
// unrestricted clients). Schema default for clients is often "no".
func clientSSHChroot(ctx context.Context, db *gorm.DB, id *repository.Identity) (bool, error) {
	if id == nil || id.IsAdmin() {
		return true, nil
	}
	var user model.SysUser
	if err := db.WithContext(ctx).Select("client_id").Where("userid = ?", id.UserID).Take(&user).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return false, nil
		}
		return false, err
	}
	if user.ClientID == 0 {
		return true, nil
	}
	var client model.Client
	if err := db.WithContext(ctx).Select("ssh_chroot").Where("client_id = ?", user.ClientID).Take(&client).Error; err != nil {
		return false, err
	}
	// Empty means "no restriction" for legacy rows; "no" forbids jailkit;
	// a comma list may include "jailkit".
	v := strings.TrimSpace(client.SSHChroot)
	if v == "" || v == "no" {
		return false, nil
	}
	for part := range strings.SplitSeq(v, ",") {
		if strings.TrimSpace(part) == "jailkit" {
			return true, nil
		}
	}
	return false, nil
}
