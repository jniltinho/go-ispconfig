package api

// Main Config form metadata (spec interface-config): the tab and field shape
// of the panel-wide INI editor over sys_ini, port of
// interface/web/admin/form/system_config.tform.php.
//
// The legacy screen renders 6 tabs and 79 fields. Rendered here are only the
// keys this port actually reads — the rest configure a PHP interface that
// does not exist in this panel (dashboard atom feeds, use_combobox, custom
// login text, maintenance mode), so an adopted database has nothing
// meaningful to show for them. TestSysIniFormCoversEveryRead is what keeps
// that list honest: a key read anywhere in the Go code with no field here
// fails the build.

// Defaults are copied from install/tpl/system.ini.master, NOT from
// system_config.tform.php. The tform ships an empty default for every prefix
// and min_password_length=5, but the values a real ISPConfig install carries
// are the ones its installer writes from the template — c[CLIENTID],
// [CLIENTNAME], min_password_length=8, min_password_strength=3. Taking the
// tform value would show an operator a default their PHP panel never had.
// internal/database/system_config.ini seeds the same values, so a fresh
// install behaves like ISPConfig rather than merely displaying that it would.
//
// systemConfigForm is the FormMeta served by GET /api/meta/forms/system_config.
func systemConfigForm() FormMeta {
	return FormMeta{
		Name:  "system_config",
		Title: "sysini.title",
		Tabs: []FormTabMeta{
			{
				Name:  "sites",
				Label: "sysini.tab.sites",
				Fields: []FormFieldMeta{
					{Name: "dbname_prefix", Label: "sysini.sites.dbname_prefix", Type: "text", Datatype: "VARCHAR", Formtype: "TEXT", Default: "c[CLIENTID]"},
					{Name: "dbuser_prefix", Label: "sysini.sites.dbuser_prefix", Type: "text", Datatype: "VARCHAR", Formtype: "TEXT", Default: "c[CLIENTID]"},
					{Name: "ftpuser_prefix", Label: "sysini.sites.ftpuser_prefix", Type: "text", Datatype: "VARCHAR", Formtype: "TEXT", Default: "[CLIENTNAME]"},
					{Name: "shelluser_prefix", Label: "sysini.sites.shelluser_prefix", Type: "text", Datatype: "VARCHAR", Formtype: "TEXT", Default: "[CLIENTNAME]"},
					{Name: "phpmyadmin_url", Label: "sysini.sites.phpmyadmin_url", Type: "text", Datatype: "VARCHAR", Formtype: "TEXT", Default: "https://[SERVERNAME]:8081/phpmyadmin"},
					// Server ids. The legacy renders these as selects over a
					// db-server datasource; here they are the raw id, which is
					// what the code reads, with the hint in the label. The "1"
					// is the tform default — system.ini.master does not write
					// these keys at all, so the form default is the only source.
					{Name: "default_dbserver", Label: "sysini.sites.default_dbserver", Type: "text", Datatype: "INTEGER", Formtype: "TEXT", Default: "1"},
					{Name: "default_remote_dbserver", Label: "sysini.sites.default_remote_dbserver", Type: "text", Datatype: "INTEGER", Formtype: "TEXT"},
					{Name: "disable_client_remote_dbserver", Label: "sysini.sites.disable_client_remote_dbserver", Type: "checkbox", Datatype: "VARCHAR", Formtype: "CHECKBOX", Default: "n", Options: []Option{{Value: "n", Label: "no_txt"}, {Value: "y", Label: "yes_txt"}}},
					{Name: "ssh_authentication", Label: "sysini.sites.ssh_authentication", Type: "select", Datatype: "VARCHAR", Formtype: "SELECT", Options: []Option{
						{Value: "", Label: "sysini.ssh_auth_any"},
						{Value: "password", Label: "sysini.ssh_auth_password"},
						{Value: "key", Label: "sysini.ssh_auth_key"},
					}},
				},
			},
			{
				Name:  "mail",
				Label: "sysini.tab.mail",
				Fields: []FormFieldMeta{
					{Name: "enable_welcome_mail", Label: "sysini.mail.enable_welcome_mail", Type: "checkbox", Datatype: "VARCHAR", Formtype: "CHECKBOX", Default: "y", Options: []Option{{Value: "n", Label: "no_txt"}, {Value: "y", Label: "yes_txt"}}},
					{Name: "admin_mail", Label: "sysini.mail.admin_mail", Type: "text", Datatype: "VARCHAR", Formtype: "TEXT"},
					{Name: "admin_name", Label: "sysini.mail.admin_name", Type: "text", Datatype: "VARCHAR", Formtype: "TEXT"},
					{Name: "show_per_domain_relay_options", Label: "sysini.mail.show_per_domain_relay_options", Type: "checkbox", Datatype: "VARCHAR", Formtype: "CHECKBOX", Default: "n", Options: []Option{{Value: "n", Label: "no_txt"}, {Value: "y", Label: "yes_txt"}}},
					// Panel-wide Rspamd thresholds. They also have a dedicated
					// endpoint (PUT /api/rspamd/policy) that writes the same
					// keys through the same merge; this is their first screen.
					{Name: "rspamd_spam_tag_level", Label: "sysini.mail.rspamd_spam_tag_level", Type: "text", Datatype: "VARCHAR", Formtype: "TEXT"},
					{Name: "rspamd_spam_kill_level", Label: "sysini.mail.rspamd_spam_kill_level", Type: "text", Datatype: "VARCHAR", Formtype: "TEXT"},
					{Name: "rspamd_greylisting_level", Label: "sysini.mail.rspamd_greylisting_level", Type: "text", Datatype: "VARCHAR", Formtype: "TEXT"},
				},
			},
			{
				Name:  "misc",
				Label: "sysini.tab.misc",
				Fields: []FormFieldMeta{
					{Name: "min_password_length", Label: "sysini.misc.min_password_length", Type: "text", Datatype: "INTEGER", Formtype: "TEXT", Default: "8"},
					{Name: "min_password_strength", Label: "sysini.misc.min_password_strength", Type: "select", Datatype: "INTEGER", Formtype: "SELECT", Default: "3", Options: []Option{
						{Value: "", Label: "sysini.strength_any"},
						{Value: "1", Label: "sysini.strength_weak"},
						{Value: "2", Label: "sysini.strength_medium"},
						{Value: "3", Label: "sysini.strength_strong"},
					}},
				},
			},
		},
	}
}

// sysIniReadElsewhere are sys_ini keys the Go code reads but this form
// deliberately does not render, with the reason. The staleness test consults
// it, so a key can only be left out on purpose and with a note.
var sysIniReadElsewhere = map[string]string{
	// Read only as a fallback when [sites] ssh_authentication is empty —
	// where older ISPConfig3 versions stored it. The [sites] field above is
	// the one to edit; rendering both would offer two answers to one
	// question (internal/api/sites_ftp_shell.go).
	"misc.ssh_authentication": "legacy fallback for sites.ssh_authentication",
}

// sysIniMinPasswordLengthMax bounds what the panel will accept as a password
// policy. A minimum above this could not be satisfied by the passwords the
// panel itself generates, so the policy would lock out its own features.
const sysIniMinPasswordLengthMax = 64
