package api

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v5"
	"golang.org/x/net/idna"
	"gorm.io/gorm"

	"go-ispconfig/internal/auth"
	"go-ispconfig/internal/getconf"
	"go-ispconfig/internal/model"
	"go-ispconfig/internal/nginx"
	"go-ispconfig/internal/repository"
	"go-ispconfig/internal/validator"
)

// This file defines the sites module entities (design D7): the Go port of
// web_vhost_domain.tform.php, web_folder.tform.php and
// web_folder_user.tform.php on the declarative entity framework, mounted
// under /api/sites. The API only persists records + sys_datalog; the daemon
// (internal/web + internal/nginx) applies them to the system.

// registerSitesEntities mounts the sites CRUD entities on the /sites group.
func registerSitesEntities(g *echo.Group, d *Deps) error {
	if err := RegisterEntity[model.WebDomain](g, d, webDomainEntity()); err != nil {
		return err
	}
	if err := RegisterEntity[model.WebFolder](g, d, webFolderEntity()); err != nil {
		return err
	}
	if err := RegisterEntity[model.WebFolderUser](g, d, webFolderUserEntity()); err != nil {
		return err
	}
	if err := registerSitesDatabaseEntities(g, d); err != nil {
		return err
	}
	if err := registerSitesCronEntity(g, d); err != nil {
		return err
	}
	return registerSitesFTPShellEntities(g, d)
}

// --- field helpers (trim the tform port boilerplate) ---

// ynOptions is the tform CHECKBOX value array(0 => 'n', 1 => 'y').
func ynOptions() []Option {
	return []Option{{Value: "n", Label: "no_txt"}, {Value: "y", Label: "yes_txt"}}
}

// checkbox is a y/n CHECKBOX field.
func checkbox(name, label, def string) Field {
	return Field{Name: name, Label: label, Datatype: "VARCHAR", Formtype: "CHECKBOX",
		Default: def, Options: ynOptions()}
}

// text is a VARCHAR TEXT field.
func text(name, label string, rules ...validator.Rule) Field {
	return Field{Name: name, Label: label, Datatype: "VARCHAR", Formtype: "TEXT", Validators: rules}
}

// textarea is a TEXT TEXTAREA field.
func textarea(name, label string, rules ...validator.Rule) Field {
	return Field{Name: name, Label: label, Datatype: "TEXT", Formtype: "TEXTAREA", Validators: rules}
}

// intField is an INTEGER TEXT field with a default.
func intField(name, label string, def any, rules ...validator.Rule) Field {
	return Field{Name: name, Label: label, Datatype: "INTEGER", Formtype: "TEXT",
		Default: def, Validators: rules}
}

// clientGroupField is the virtual owner selector: applyClientGroup maps it
// onto sys_groupid on create. An entity that omits it silently files every
// record under the admin group, where the owning client can never see it.
func clientGroupField() Field {
	return Field{Name: "client_group_id", Label: "client_txt", Datatype: "INTEGER",
		Formtype: "SELECT", Default: "0", AdminOnly: true, Virtual: true}
}

// selectField is a SELECT field.
func selectField(name, label, datatype string, def any, opts []Option, rules ...validator.Rule) Field {
	return Field{Name: name, Label: label, Datatype: datatype, Formtype: "SELECT",
		Default: def, Options: opts, Validators: rules}
}

// regex builds a REGEX rule.
func regex(pattern, errKey string) validator.Rule {
	return validator.Rule{Type: "REGEX", Regex: pattern, ErrKey: errKey}
}

// alwaysHidden drops a field from form metadata for every request while
// leaving defaults and API body writes intact (legacy template conditionals
// that never render the control on the Website Domain tab).
func alwaysHidden(*echo.Context) bool { return true }

// --- web-domain entity (port of web_vhost_domain.tform.php) ---

// webDomainEntity is the declarative definition of the web_domain vhost
// form: tabs Domain, Redirect, SSL, Statistics, Backup and Options (Options
// is admin-only, as in ISPConfig).
func webDomainEntity() *Entity {
	// Website Domain tab order mirrors web_vhost_domain_edit.htm for
	// vhostdomain_type=domain. Child-only / never-rendered controls stay
	// Hidden from SPA metadata. Apache-only Domain fields (suexec, perl,
	// ruby, python) and Options directives stay in metadata; the SPA
	// hides them from nginx servers via adjustForm parity (server_type
	// from /meta/lookups/servers). type/parent/web_folder stay visible in
	// metadata so the SPA can reveal them when editing a vhostsubdomain.
	vhostTypeField := selectField("vhost_type", "vhost_type_txt", "VARCHAR", "name", []Option{
		{Value: "name", Label: "Namebased"},
		{Value: "ip", Label: "IP-Based"},
	})
	vhostTypeField.Hidden = alwaysHidden
	leExcludeField := checkbox("ssl_letsencrypt_exclude", "ssl_letsencrypt_exclude_txt", "n")
	leExcludeField.Hidden = alwaysHidden // child-domain form only
	pagespeedField := checkbox("enable_pagespeed", "enable_pagespeed_txt", "n")
	pagespeedField.Hidden = alwaysHidden // needs nginx_enable_pagespeed; not wired yet

	return &Entity{
		Name:        "web-domains",
		Title:       "web_vhost_domain_edit_title",
		Prepare:     webDomainPrepare,
		AfterInsert: webDomainAfterInsert,
		// Server column shows the hostname (legacy list parity); the filter
		// box searches server.server_name via ListFilters.
		Decorate: webDomainDecorate(),
		ListFilters: map[string]ListFilterFunc{
			"_server_name": relatedNameFilter("server_id", "server", "server_id", "server_name"),
		},
		Tabs: []Tab{
			{
				Name: "domain", Label: "domain_tab_txt",
				Fields: []Field{
					selectField("server_id", "server_id_txt", "INTEGER", nil, nil,
						validator.Rule{Type: "ISPOSITIVE", ErrKey: "no_server_error"}),
					clientGroupField(),
					selectField("ip_address", "ip_address_txt", "VARCHAR", "*", nil),
					selectField("ipv6_address", "ipv6_address_txt", "VARCHAR", "", nil),
					text("domain", "domain_txt",
						validator.Rule{Type: "NOTEMPTY", ErrKey: "domain_error_empty"},
						regex(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`, "domain_error_regex"),
						validator.Rule{Type: "UNIQUE", ErrKey: "domain_error_unique"}),
					selectField("type", "type_txt", "VARCHAR", "vhost", []Option{
						{Value: "vhost", Label: "type_vhost_txt"},
						{Value: "vhostsubdomain", Label: "type_vhostsubdomain_txt"},
						{Value: "vhostalias", Label: "type_vhostalias_txt"},
					}),
					selectField("parent_domain_id", "parent_domain_id_txt", "INTEGER", nil, nil),
					vhostTypeField,
					text("web_folder", "web_folder_txt",
						validator.Rule{Type: "CUSTOM", ErrKey: "web_folder_error_regex", Fn: checkWebFolder}),
					intField("hd_quota", "hd_quota_txt", "-1",
						validator.Rule{Type: "NOTEMPTY", ErrKey: "hd_quota_error_empty"},
						regex(`^(\-1|[0-9]{1,10})$`, "hd_quota_error_regex")),
					intField("traffic_quota", "traffic_quota_txt", "-1",
						validator.Rule{Type: "NOTEMPTY", ErrKey: "traffic_quota_error_empty"},
						regex(`^(\-1|[0-9]{1,10})$`, "traffic_quota_error_regex")),
					checkbox("cgi", "cgi_txt", "n"),
					checkbox("ssi", "ssi_txt", "n"),
					// Apache-only in the legacy template (.apache); SPA hides
					// on nginx. Order matches web_vhost_domain_edit.htm.
					checkbox("perl", "perl_txt", "n"),
					checkbox("ruby", "ruby_txt", "n"),
					checkbox("python", "python_txt", "n"),
					checkbox("suexec", "suexec_txt", "y"),
					{Name: "errordocs", Label: "errordocs_txt", Datatype: "INTEGER",
						Formtype: "CHECKBOX", Default: "1",
						Options: []Option{{Value: "0", Label: "no_txt"}, {Value: "1", Label: "yes_txt"}}},
					selectField("subdomain", "subdomain_txt", "VARCHAR", "www", []Option{
						{Value: "none", Label: "none_txt"},
						{Value: "www", Label: "www."},
						{Value: "*", Label: "*."},
					}),
					checkbox("ssl", "ssl_txt", "n"),
					checkbox("ssl_letsencrypt", "ssl_letsencrypt_txt", "n"),
					leExcludeField,
					// Full tform PHP list; SPA narrows to nginx (no/php-fpm)
					// or apache modes from the selected server's server_type
					// (legacy adjustForm / add-apache2-module proposal).
					selectField("php", "php_txt", "VARCHAR", "php-fpm", []Option{
						{Value: "no", Label: "disabled_txt"},
						{Value: "fast-cgi", Label: "Fast-CGI"},
						{Value: "cgi", Label: "CGI"},
						{Value: "mod", Label: "Mod-PHP"},
						{Value: "suphp", Label: "SuPHP"},
						{Value: "php-fpm", Label: "PHP-FPM"},
					}),
					// ponytail: single "Default" option; per-server PHP
					// versions get a lookup when multi-PHP support lands.
					selectField("server_php_id", "server_php_id_txt", "INTEGER", "0",
						[]Option{{Value: "0", Label: "Default"}}),
					// Legacy "Web server config" (directive_snippets plugin).
					selectField("directive_snippets_id", "directive_snippets_id_txt", "INTEGER", "0",
						[]Option{{Value: "0", Label: "-"}}),
					pagespeedField,
					checkbox("active", "active_txt", "y"),
				},
			},
			{
				Name: "redirect", Label: "redirect_tab_txt",
				Fields: []Field{
					selectField("redirect_type", "redirect_type_txt", "VARCHAR", "", []Option{
						{Value: "", Label: "no_redirect_txt"},
						{Value: "no", Label: "no_flag_txt"},
						{Value: "last", Label: "last"},
						{Value: "break", Label: "break"},
						{Value: "redirect", Label: "redirect"},
						{Value: "permanent", Label: "permanent"},
						{Value: "proxy", Label: "proxy"},
					}),
					text("redirect_path", "redirect_path_txt",
						validator.Rule{Type: "CUSTOM", ErrKey: "redirect_error_regex", Fn: checkRedirectPath}),
					selectField("seo_redirect", "seo_redirect_txt", "VARCHAR", "", []Option{
						{Value: "", Label: "no_redirect_txt"},
						{Value: "non_www_to_www", Label: "domain.tld => www.domain.tld"},
						{Value: "www_to_non_www", Label: "www.domain.tld => domain.tld"},
						{Value: "*_domain_tld_to_domain_tld", Label: "*.domain.tld => domain.tld"},
						{Value: "*_domain_tld_to_www_domain_tld", Label: "*.domain.tld => www.domain.tld"},
						{Value: "*_to_domain_tld", Label: "* => domain.tld"},
						{Value: "*_to_www_domain_tld", Label: "* => www.domain.tld"},
					}),
					textarea("rewrite_rules", "rewrite_rules_txt"),
					checkbox("rewrite_to_https", "rewrite_to_https_txt", "n"),
				},
			},
			{
				Name: "ssl", Label: "ssl_tab_txt",
				Fields: []Field{
					text("ssl_state", "ssl_state_txt",
						regex(`^[-a-zA-Z0-9._,&äöüÄÖÜ ]{0,255}$`, "ssl_state_error_regex")),
					text("ssl_locality", "ssl_locality_txt",
						regex(`^[-a-zA-Z0-9._,&äöüÄÖÜ ]{0,255}$`, "ssl_locality_error_regex")),
					text("ssl_organisation", "ssl_organisation_txt",
						regex(`^[-a-zA-Z0-9._,&äöüÄÖÜ ]{0,255}$`, "ssl_organisation_error_regex")),
					text("ssl_organisation_unit", "ssl_organisation_unit_txt",
						regex(`^[-a-zA-Z0-9._,&äöüÄÖÜ ]{0,255}$`, "ssl_organisation_unit_error_regex")),
					text("ssl_country", "ssl_country_txt",
						regex(`^([A-Z]{2})?$`, "ssl_country_error_regex")),
					text("ssl_domain", "ssl_domain_txt"),
					textarea("ssl_key", "ssl_key_txt"),
					textarea("ssl_request", "ssl_request_txt"),
					textarea("ssl_cert", "ssl_cert_txt"),
					textarea("ssl_bundle", "ssl_bundle_txt"),
					selectField("ssl_action", "ssl_action_txt", "VARCHAR", "", []Option{
						{Value: "", Label: "none_txt"},
						{Value: "save", Label: "save_certificate_txt"},
						{Value: "create", Label: "create_certificate_txt"},
						{Value: "del", Label: "delete_certificate_txt"},
					}),
				},
			},
			{
				Name: "stats", Label: "stats_tab_txt",
				Fields: []Field{
					{Name: "stats_password", Label: "stats_password_txt",
						Datatype: "VARCHAR", Formtype: "PASSWORD"},
					selectField("stats_type", "stats_type_txt", "VARCHAR", "awstats", []Option{
						{Value: "awstats", Label: "AWStats"},
						{Value: "goaccess", Label: "GoAccess"},
						{Value: "webalizer", Label: "Webalizer"},
						{Value: "", Label: "none_txt"},
					}),
				},
			},
			{
				Name: "backup", Label: "backup_tab_txt",
				Fields: []Field{
					selectField("backup_interval", "backup_interval_txt", "VARCHAR", "none", []Option{
						{Value: "none", Label: "no_backup_txt"},
						{Value: "daily", Label: "daily_backup_txt"},
						{Value: "weekly", Label: "weekly_backup_txt"},
						{Value: "monthly", Label: "monthly_backup_txt"},
					}),
					selectField("backup_copies", "backup_copies_txt", "INTEGER", "1", []Option{
						{Value: "1", Label: "1"}, {Value: "2", Label: "2"}, {Value: "3", Label: "3"},
						{Value: "4", Label: "4"}, {Value: "5", Label: "5"}, {Value: "6", Label: "6"},
						{Value: "7", Label: "7"}, {Value: "8", Label: "8"}, {Value: "9", Label: "9"},
						{Value: "10", Label: "10"}, {Value: "15", Label: "15"},
						{Value: "20", Label: "20"}, {Value: "30", Label: "30"},
					}),
					text("backup_excludes", "backup_excludes_txt",
						validator.Rule{Type: "CUSTOM", ErrKey: "backup_excludes_error_regex", Fn: checkBackupExcludes}),
					selectField("backup_format_web", "backup_format_web_txt", "VARCHAR", "default", []Option{
						{Value: "default", Label: "backup_format_default_txt"},
						{Value: "zip", Label: "backup_format_zip_txt"},
						{Value: "zip_bzip2", Label: "backup_format_zip_bzip2_txt"},
						{Value: "tar_gzip", Label: "backup_format_tar_gzip_txt"},
						{Value: "tar_bzip2", Label: "backup_format_tar_bzip2_txt"},
						{Value: "tar_xz", Label: "backup_format_tar_xz_txt"},
					}),
					selectField("backup_format_db", "backup_format_db_txt", "VARCHAR", "gzip", []Option{
						{Value: "zip", Label: "backup_format_zip_txt"},
						{Value: "gzip", Label: "backup_format_gzip_txt"},
						{Value: "bzip2", Label: "backup_format_bzip2_txt"},
						{Value: "xz", Label: "backup_format_xz_txt"},
					}),
					checkbox("backup_encrypt", "backup_encrypt_txt", "n"),
					text("backup_password", "backup_password_txt"),
				},
			},
			{
				Name: "advanced", Label: "options_tab_txt", AdminOnly: true,
				Fields: []Field{
					// document_root, system_user and system_group are derived
					// server-side on insert (webDomainAfterInsert); no
					// NOTEMPTY here because they are legitimately empty in
					// the create payload.
					text("document_root", "document_root_txt"),
					text("system_user", "system_user_txt"),
					text("system_group", "system_group_txt"),
					{Name: "allow_override", Label: "allow_override_txt", Datatype: "VARCHAR",
						Formtype: "TEXT", Default: "All",
						Validators: []validator.Rule{
							{Type: "NOTEMPTY", ErrKey: "allow_override_error_empty"},
						}},
					checkbox("proxy_protocol", "proxy_protocol_txt", "n"),
					checkbox("php_fpm_use_socket", "php_fpm_use_socket_txt", "y"),
					checkbox("php_fpm_chroot", "php_fpm_chroot_txt", "n"),
					selectField("pm", "pm_txt", "VARCHAR", "ondemand", []Option{
						{Value: "static", Label: "static"},
						{Value: "dynamic", Label: "dynamic"},
						{Value: "ondemand", Label: "ondemand"},
					}),
					intField("pm_max_children", "pm_max_children_txt", "10",
						regex(`^([1-9][0-9]{0,10})$`, "pm_max_children_error_regex")),
					intField("pm_start_servers", "pm_start_servers_txt", "2",
						regex(`^([1-9][0-9]{0,10})$`, "pm_start_servers_error_regex")),
					intField("pm_min_spare_servers", "pm_min_spare_servers_txt", "1",
						regex(`^([1-9][0-9]{0,10})$`, "pm_min_spare_servers_error_regex")),
					intField("pm_max_spare_servers", "pm_max_spare_servers_txt", "5",
						regex(`^([1-9][0-9]{0,10})$`, "pm_max_spare_servers_error_regex")),
					intField("pm_process_idle_timeout", "pm_process_idle_timeout_txt", "10",
						regex(`^([1-9][0-9]{0,10})$`, "pm_process_idle_timeout_error_regex")),
					intField("pm_max_requests", "pm_max_requests_txt", "0",
						regex(`^([0-9]{1,11})$`, "pm_max_requests_error_regex")),
					checkbox("disable_symlinknotowner", "disable_symlinknotowner_txt", "n"),
					text("php_open_basedir", "php_open_basedir_txt"),
					textarea("custom_php_ini", "custom_php_ini_txt"),
					textarea("apache_directives", "apache_directives_txt"),
					textarea("nginx_directives", "nginx_directives_txt",
						validator.Rule{Type: "CUSTOM", ErrKey: "nginx_directive_blocked_error", Fn: checkNginxDirectives}),
					textarea("proxy_directives", "proxy_directives_txt"),
					intField("http_port", "http_port_txt", "80",
						regex(`^([0-9]{1,5})$`, "http_port_error_regex")),
					intField("https_port", "https_port_txt", "443",
						regex(`^([0-9]{1,5})$`, "https_port_error_regex")),
					intField("log_retention", "log_retention_txt", "30",
						regex(`^([0-9]{1,4})$`, "log_retention_error_regex")),
					textarea("jailkit_chroot_app_sections", "jailkit_chroot_app_sections_txt",
						regex(`^[a-zA-Z0-9_ -]*$`, "jailkit_chroot_app_sections_error_regex")),
					textarea("jailkit_chroot_app_programs", "jailkit_chroot_app_programs_txt",
						regex(`^[a-zA-Z0-9._/ -]*$`, "jailkit_chroot_app_programs_error_regex")),
					checkbox("delete_unused_jailkit", "delete_unused_jailkit_txt", "n"),
				},
			},
		},
	}
}

// --- custom validators (tform CUSTOM classes ported; PHP lookahead
// regexes rewritten without lookahead for RE2) ---

// webFolderRe is the allowed character set of a web_folder value.
var webFolderRe = regexp.MustCompile(`^[\w/._-]{1,100}$`)

// checkWebFolder ports the web_folder REGEX (no traversal, no leading
// slash, no double slash); empty is allowed.
func checkWebFolder(_ *validator.Context, value string) string {
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "/") || strings.Contains(value, "..") ||
		strings.Contains(value, "./") || strings.Contains(value, "//") ||
		!webFolderRe.MatchString(value) {
		return "web_folder_error_regex"
	}
	return ""
}

// redirectURLRe accepts scheme://host[:port][/path] redirect targets.
var redirectURLRe = regexp.MustCompile(`^(ftp|https?|\[scheme\])://[-\w.]+(:\d+)?(/[\w/_.,\-+?~!:%=&;#]*)?$`)

// redirectPathRe accepts absolute /path/ redirect targets.
var redirectPathRe = regexp.MustCompile(`^/[\w/_.\-]{1,255}/$`)

// checkRedirectPath ports the redirect_path validator: empty, a URL or an
// absolute path ending in '/', never containing '..'.
func checkRedirectPath(_ *validator.Context, value string) string {
	if value == "" {
		return ""
	}
	if strings.Contains(value, "..") ||
		(!redirectURLRe.MatchString(value) && !redirectPathRe.MatchString(value)) {
		return "redirect_error_regex"
	}
	return ""
}

// backupExcludesRe is the allowed character set of backup_excludes.
var backupExcludesRe = regexp.MustCompile(`^[-a-zA-Z0-9_/.~,* ]*$`)

// checkBackupExcludes ports the backup_excludes validator (no '..').
func checkBackupExcludes(_ *validator.Context, value string) string {
	if strings.Contains(value, "..") || !backupExcludesRe.MatchString(value) {
		return "backup_excludes_error_regex"
	}
	return ""
}

// folderPathRe is the allowed character set of a web_folder path.
var folderPathRe = regexp.MustCompile(`^[\w./_-]{0,255}$`)

// checkFolderPath validates a protected-folder path with the daemon's
// traversal guards applied at save time: no '..', './' or backslashes
// (the daemon's folderAuthPath remains as defense in depth).
func checkFolderPath(_ *validator.Context, value string) string {
	if value == "" {
		return ""
	}
	if strings.Contains(value, "..") || strings.Contains(value, "./") ||
		strings.Contains(value, `\`) || !folderPathRe.MatchString(value) {
		return "path_error_regex"
	}
	return ""
}

// checkNginxDirectives rejects custom nginx directives matching the
// embedded security blacklist at save time (spec sites-api). The returned
// key carries the offending lines; the render-time strip in the daemon
// remains as defense in depth.
func checkNginxDirectives(_ *validator.Context, value string) string {
	if blocked := nginx.BlockedDirectives(value); len(blocked) > 0 {
		return "nginx_directive_blocked_error: " + strings.Join(blocked, ", ")
	}
	return ""
}

// --- hooks ---

// bodyInt reads a numeric JSON body value (float64 or numeric string).
func bodyInt(body map[string]any, key string) int64 {
	switch v := body[key].(type) {
	case float64:
		return int64(v)
	case string:
		n, _ := strconv.ParseInt(v, 10, 64)
		return n
	default:
		return 0
	}
}

// loadOwned loads a record of another entity under the caller's read
// permission scope; a cross-client reference fails with permission denied.
func loadOwned[T any](c *echo.Context, d *Deps, id *repository.Identity, pk any) (*T, error) {
	repo, err := repository.New[T](d.DB)
	if err != nil {
		return nil, err
	}
	var rec T
	if err := repo.Get(c.Request().Context(), id, pk, &rec); err != nil {
		return nil, err
	}
	return &rec, nil
}

// cryptBodyPassword crypts a plaintext password field in the request body
// (tform 'encryption' => 'CRYPT'); values already in crypt form are kept.
func cryptBodyPassword(body map[string]any, field string) error {
	v, ok := body[field].(string)
	if !ok || v == "" || auth.IsLegacyHash(v) {
		return nil
	}
	h, err := auth.CryptPassword(v)
	if err != nil {
		return err
	}
	body[field] = h
	return nil
}

// webDomainPrepare normalizes and derives web-domain body fields before
// validation: domain lowercased and IDN-encoded (tform TOLOWER/IDNTOASCII
// filters), stats_password crypted, and any referenced parent domain
// checked against the caller's read scope (cross-client protection), with
// vhostsubdomain/vhostalias inheriting the parent's server.
func webDomainPrepare(c *echo.Context, d *Deps, id *repository.Identity, body map[string]any) error {
	if v, ok := body["domain"].(string); ok {
		v = strings.ToLower(strings.TrimSpace(v))
		if ascii, err := idna.Lookup.ToASCII(v); err == nil {
			v = ascii
		}
		body["domain"] = v
	}
	if err := cryptBodyPassword(body, "stats_password"); err != nil {
		return err
	}
	if pid := bodyInt(body, "parent_domain_id"); pid > 0 {
		parent, err := loadOwned[model.WebDomain](c, d, id, pid)
		if err != nil {
			return err
		}
		if t, _ := body["type"].(string); t == "vhostsubdomain" || t == "vhostalias" {
			// The parent of a vhost subdomain/alias must be a real vhost
			// (PHP restricts the datasource to type = 'vhost').
			if parent.Type != "vhost" {
				return &ValidationError{Fields: map[string][]string{
					"parent_domain_id": {"parent_domain_id_error_invalid"},
				}}
			}
			body["server_id"] = float64(parent.ServerID)
		}
	}
	// Last: a vhostsubdomain inherits server_id from its parent above, and
	// an unowned parent must fail with the permission error, not with a
	// target-server complaint.
	return requireTargetServer("web_server")(c, d, body)
}

// defaultWebsitePath is the ISPConfig Debian/Ubuntu website_path used when
// the server web config does not define one.
const defaultWebsitePath = "/var/www/clients/client[client_id]/web[website_id]"

// webDomainAfterInsert ports the web_vhost_domain onAfterInsert derivation:
// once the domain_id exists, document_root ([client_id]/[website_id]
// placeholders of the server's website_path), system_user (web<id>) and
// system_group (client<id>) are computed and persisted inside the create
// transaction, so the datalog insert row already carries them.
// vhostsubdomain/vhostalias records inherit these values from their parent.
func webDomainAfterInsert(ctx context.Context, tx *gorm.DB, id *repository.Identity, recAny any, _ map[string]any) error {
	rec := recAny.(*model.WebDomain)
	now := time.Now()
	rec.AddedDate = &now
	if id != nil {
		rec.AddedBy = id.Username
	}
	cols := []string{"added_date", "added_by"}

	path, basedirTpl := defaultWebsitePath, ""
	if cfg, err := getconf.GetServerConfig(tx, rec.ServerID); err == nil {
		if cfg.Web.WebsitePath != "" {
			path = cfg.Web.WebsitePath
		}
		basedirTpl = cfg.Web.PHPOpenBasedir
	}

	switch rec.Type {
	case "vhost":
		var group model.SysGroup
		err := tx.Where("groupid = ?", rec.SysGroupID).Take(&group).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("api: loading sys_group %d: %w", rec.SysGroupID, err)
		}
		clientID := group.ClientID

		rec.DocumentRoot = strings.NewReplacer(
			"[client_id]", strconv.FormatUint(uint64(clientID), 10),
			"[website_id]", strconv.FormatUint(uint64(rec.DomainID), 10),
		).Replace(path)
		rec.SystemUser = fmt.Sprintf("web%d", rec.DomainID)
		rec.SystemGroup = fmt.Sprintf("client%d", clientID)
		rec.PHPOpenBasedir = expandOpenBasedir(basedirTpl, rec.DocumentRoot, rec.Domain, "")
		cols = append(cols, "document_root", "system_user", "system_group", "php_open_basedir")
	case "vhostsubdomain", "vhostalias":
		var parent model.WebDomain
		if err := tx.Where("domain_id = ?", rec.ParentDomainID).Take(&parent).Error; err != nil {
			return fmt.Errorf("api: loading parent domain %d: %w", rec.ParentDomainID, err)
		}
		rec.SysGroupID = parent.SysGroupID
		rec.DocumentRoot = parent.DocumentRoot
		rec.SystemUser = parent.SystemUser
		rec.SystemGroup = parent.SystemGroup
		rec.AllowOverride = parent.AllowOverride
		rec.PHPOpenBasedir = expandOpenBasedir(basedirTpl, parent.DocumentRoot, rec.Domain, rec.WebFolder)
		cols = append(cols, "sys_groupid", "document_root", "system_user", "system_group",
			"allow_override", "php_open_basedir")
	}
	return tx.Model(rec).Select(cols).Updates(rec).Error
}

// expandOpenBasedir ports the php_open_basedir derivation of
// web_vhost_domain_edit onAfterInsert: the server's php_open_basedir template
// with its [website_path] / [website_domain] placeholders expanded. Without
// it the FPM pool falls back to the bare document_root and loses /tmp,
// /usr/share/php and the /var/www/<domain> symlink the vhost root points at.
// webFolder is empty for a vhost; for vhostsubdomain/vhostalias the "/web"
// suffix of the two placeholders becomes the record's own web folder.
func expandOpenBasedir(tmpl, docroot, domain, webFolder string) string {
	if tmpl == "" {
		return ""
	}
	pairs := []string{}
	if webFolder != "" {
		// Longer patterns first: Replacer matches them in the given order.
		pairs = append(pairs,
			"[website_path]/web", docroot+"/"+webFolder,
			"[website_domain]/web", domain+"/"+webFolder,
		)
	}
	pairs = append(pairs, "[website_path]", docroot, "[website_domain]", domain)
	return strings.NewReplacer(pairs...).Replace(tmpl)
}

// --- web-folder and web-folder-user entities ---

// webFolderEntity is the declarative definition of the web_folder form
// (port of web_folder.tform.php).
func webFolderEntity() *Entity {
	return &Entity{
		Name:     "web-folders",
		Title:    "web_folder_edit_title",
		Prepare:  webFolderPrepare,
		Decorate: siteChildDecorate("web_folder", "web_folder_id"),
		ListFilters: map[string]ListFilterFunc{
			"_server_name":   relatedNameFilter("server_id", "server", "server_id", "server_name"),
			"_parent_domain": relatedNameFilter("parent_domain_id", "web_domain", "domain_id", "domain"),
		},
		Tabs: []Tab{{
			Name: "folder", Label: "folder_tab_txt",
			Fields: []Field{
				selectField("server_id", "server_id_txt", "INTEGER", nil, nil,
					validator.Rule{Type: "ISPOSITIVE", ErrKey: "no_server_error"}),
				selectField("parent_domain_id", "parent_domain_id_txt", "INTEGER", nil, nil,
					validator.Rule{Type: "ISPOSITIVE", ErrKey: "folder_error_domain_empty"}),
				{Name: "path", Label: "path_txt", Datatype: "VARCHAR", Formtype: "TEXT",
					Default: "/",
					Validators: []validator.Rule{
						{Type: "NOTEMPTY", ErrKey: "folder_error_empty"},
						{Type: "CUSTOM", ErrKey: "path_error_regex", Fn: checkFolderPath},
					}},
				checkbox("active", "active_txt", "y"),
			},
		}},
	}
}

// webFolderPrepare derives server_id from the referenced parent domain and
// enforces the caller's read permission on it (cross-client protection).
func webFolderPrepare(c *echo.Context, d *Deps, id *repository.Identity, body map[string]any) error {
	if pid := bodyInt(body, "parent_domain_id"); pid > 0 {
		parent, err := loadOwned[model.WebDomain](c, d, id, pid)
		if err != nil {
			return err
		}
		body["server_id"] = float64(parent.ServerID)
	}
	return nil
}

// webFolderUserEntity is the declarative definition of the web_folder_user
// form (port of web_folder_user.tform.php). Passwords are stored SHA-512
// crypted (never plain) so the daemon can copy them into .htpasswd files.
func webFolderUserEntity() *Entity {
	return &Entity{
		Name:     "web-folder-users",
		Title:    "web_folder_user_edit_title",
		Prepare:  webFolderUserPrepare,
		Decorate: datalogStateDecorator("web_folder_user", "web_folder_user_id"),
		Tabs: []Tab{{
			Name: "user", Label: "folder_user_tab_txt",
			Fields: []Field{
				// server_id is not part of the PHP form; declared so the
				// value derived from the folder (Prepare) reaches the row.
				selectField("server_id", "server_id_txt", "INTEGER", nil, nil),
				selectField("web_folder_id", "web_folder_id_txt", "INTEGER", nil, nil,
					validator.Rule{Type: "ISPOSITIVE", ErrKey: "folder_error_empty"}),
				text("username", "username_txt",
					validator.Rule{Type: "NOTEMPTY", ErrKey: "username_error_empty"},
					regex(`^[\w.-]{0,64}$`, "username_error_regex")),
				{Name: "password", Label: "password_txt", Datatype: "VARCHAR",
					Formtype: "PASSWORD",
					Validators: []validator.Rule{
						{Type: "NOTEMPTY", ErrKey: "password_error_empty"},
					}},
				checkbox("active", "active_txt", "y"),
			},
		}},
	}
}

// webFolderUserPrepare crypts the submitted password and derives server_id
// from the referenced folder, enforcing the caller's read permission on it.
func webFolderUserPrepare(c *echo.Context, d *Deps, id *repository.Identity, body map[string]any) error {
	if err := cryptBodyPassword(body, "password"); err != nil {
		return err
	}
	if fid := bodyInt(body, "web_folder_id"); fid > 0 {
		folder, err := loadOwned[model.WebFolder](c, d, id, fid)
		if err != nil {
			return err
		}
		body["server_id"] = float64(folder.ServerID)
	}
	return nil
}

// --- datalog pending/error state (spec sites-api "Datalog error
// visibility") ---

// webDomainDecorate adds datalog state plus the server hostname so the
// websites list can filter/display names instead of raw server_id, and
// mirrors sys_groupid as client_group_id for the virtual form field.
func webDomainDecorate() func(ctx context.Context, db *gorm.DB, items []map[string]any) error {
	inner := serverNameDecorate(datalogStateDecorator("web_domain", "domain_id"))
	return func(ctx context.Context, db *gorm.DB, items []map[string]any) error {
		for _, it := range items {
			it["client_group_id"] = it["sys_groupid"]
		}
		return inner(ctx, db, items)
	}
}

// datalogStateDecorator returns a Decorate hook adding the per-record
// sys_datalog state to list/get responses: `_datalog_state` is "pending"
// while a journaled change has not been processed by the record's daemon
// (datalog_id > server.updated) and "error" when the newest processed row
// was quarantined, with the recorded message in `_datalog_error`.
func datalogStateDecorator(table, pkCol string) func(ctx context.Context, db *gorm.DB, items []map[string]any) error {
	return func(ctx context.Context, db *gorm.DB, items []map[string]any) error {
		keys := make([]string, 0, len(items))
		byIdx := make(map[string]map[string]any, len(items))
		for _, it := range items {
			key := fmt.Sprintf("%s:%v", pkCol, it[pkCol])
			keys = append(keys, key)
			byIdx[key] = it
		}

		var servers []model.Server
		if err := db.WithContext(ctx).Select("server_id", "updated").Find(&servers).Error; err != nil {
			return err
		}
		updated := make(map[uint32]uint64, len(servers))
		for _, s := range servers {
			updated[s.ServerID] = s.Updated
		}

		var rows []model.SysDatalog
		err := db.WithContext(ctx).
			Select("dbidx", "server_id", "datalog_id", "status", "error").
			Where("dbtable = ? AND dbidx IN ?", table, keys).
			Order("datalog_id").Find(&rows).Error
		if err != nil {
			return err
		}

		type state struct {
			pending bool
			errMsg  string
			hasErr  bool
		}
		states := map[string]*state{}
		for _, r := range rows {
			st := states[r.DBIdx]
			if st == nil {
				st = &state{}
				states[r.DBIdx] = st
			}
			if uint64(r.DatalogID) > updated[r.ServerID] {
				st.pending = true
				continue
			}
			// Processed rows: the newest one wins (an ok run clears an
			// older error).
			st.hasErr = r.Status == "error"
			st.errMsg = r.Error
		}
		for key, it := range byIdx {
			st := states[key]
			switch {
			case st == nil:
			case st.pending:
				it["_datalog_state"] = "pending"
			case st.hasErr:
				it["_datalog_state"] = "error"
				it["_datalog_error"] = st.errMsg
			}
		}
		return nil
	}
}
