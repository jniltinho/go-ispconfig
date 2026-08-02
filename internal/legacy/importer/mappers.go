package importer

import (
	"go-ispconfig/internal/legacy/client"
	"go-ispconfig/internal/model"
)

// PlaceholderHash is the unusable password stamped onto recreated panel
// sys_users: the remote API never exposes sys_user.passwort, and
// auth.VerifyPassword matches no password against it, so every recreated
// panel login requires a password reset (design D5).
const PlaceholderHash = "!"

// MapClient maps a legacy client record onto the local client model.
// Legacy ids (client_id, parent_client_id, sys_userid, sys_groupid) stay
// as fetched; the planner rewrites them through the remap table. The riud
// permission strings are carried verbatim.
func MapClient(rec client.Record) (*model.Client, error) {
	var c model.Client
	err := mapRecord(rec, &c)
	return &c, err
}

// MapWebDomain maps a legacy web_domain record, including the SSL fields
// (ssl, ssl_letsencrypt, certificate/key/bundle text) exactly as returned
// — the report warns that certificates must be re-issued on the new host.
func MapWebDomain(rec client.Record) (*model.WebDomain, error) {
	var d model.WebDomain
	err := mapRecord(rec, &d)
	return &d, err
}

// MapWebFolder maps a legacy web_folder record.
func MapWebFolder(rec client.Record) (*model.WebFolder, error) {
	var f model.WebFolder
	err := mapRecord(rec, &f)
	return &f, err
}

// MapWebFolderUser maps a legacy web_folder_user record; crypt password
// hashes ($1$/$5$/$6$) are carried verbatim (design D5).
func MapWebFolderUser(rec client.Record) (*model.WebFolderUser, error) {
	var u model.WebFolderUser
	err := mapRecord(rec, &u)
	return &u, err
}

// MapDNSSoa maps a legacy dns_soa record.
func MapDNSSoa(rec client.Record) (*model.DNSSoa, error) {
	var z model.DNSSoa
	err := mapRecord(rec, &z)
	return &z, err
}

// MapDNSRr maps a legacy dns_rr record; zone stays the legacy dns_soa id
// until the planner remaps it.
func MapDNSRr(rec client.Record) (*model.DNSRr, error) {
	var rr model.DNSRr
	err := mapRecord(rec, &rr)
	return &rr, err
}

// MapDNSSlave maps a legacy dns_slave record.
func MapDNSSlave(rec client.Record) (*model.DNSSlave, error) {
	var s model.DNSSlave
	err := mapRecord(rec, &s)
	return &s, err
}

// MapDNSTemplate maps a legacy dns_template record.
func MapDNSTemplate(rec client.Record) (*model.DNSTemplate, error) {
	var t model.DNSTemplate
	err := mapRecord(rec, &t)
	return &t, err
}

// DeriveSysGroup builds the sys_group recreated for an imported client,
// as ISPConfig does on client creation: one group named after the client
// login. ClientID is filled by apply once the local client id exists.
func DeriveSysGroup(c *model.Client) *model.SysGroup {
	return &model.SysGroup{
		Name:        c.Username,
		Description: c.ContactName,
	}
}

// DeriveSysUser builds the panel sys_user recreated for an imported
// client. The password is always PlaceholderHash — panel hashes cannot be
// fetched over the remote API — so the user is created unable to log in
// and flagged for the bulk password-reset flow. Group links (groups,
// default_group, client_id) are filled by apply once the local sys_group
// exists.
func DeriveSysUser(c *model.Client) *model.SysUser {
	return &model.SysUser{
		SysUserID:    1, // owned by admin, like ISPConfig-created client users
		SysPermUser:  "riud",
		SysPermGroup: "riud",
		Username:     c.Username,
		Passwort:     PlaceholderHash,
		Modules:      "dashboard,sites,dns,tools,help",
		Startmodule:  "dashboard",
		AppTheme:     "default",
		Typ:          "user",
		Active:       1,
		Language:     c.Language,
	}
}
