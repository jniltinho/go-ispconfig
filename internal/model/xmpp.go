package model

// XMPPDomain is an XMPP (Prosody/ejabberd) virtual host and its modules
// (table xmpp_domain).
type XMPPDomain struct {
	DomainID                uint32 `gorm:"column:domain_id;primaryKey;autoIncrement"`
	SysUserID               uint32 `gorm:"column:sys_userid"`
	SysGroupID              uint32 `gorm:"column:sys_groupid"`
	SysPermUser             string `gorm:"column:sys_perm_user"`
	SysPermGroup            string `gorm:"column:sys_perm_group"`
	SysPermOther            string `gorm:"column:sys_perm_other"`
	ServerID                uint32 `gorm:"column:server_id"`
	Domain                  string `gorm:"column:domain"`
	ManagementMethod        string `gorm:"column:management_method"`
	PublicRegistration      string `gorm:"column:public_registration"`
	RegistrationURL         string `gorm:"column:registration_url"`
	RegistrationMessage     string `gorm:"column:registration_message"`
	DomainAdmins            string `gorm:"column:domain_admins"`
	UsePubsub               string `gorm:"column:use_pubsub"`
	UseProxy                string `gorm:"column:use_proxy"`
	UseAnonHost             string `gorm:"column:use_anon_host"`
	UseVJUD                 string `gorm:"column:use_vjud"`
	VJUDOptMode             string `gorm:"column:vjud_opt_mode"`
	UseMUCHost              string `gorm:"column:use_muc_host"`
	MUCName                 string `gorm:"column:muc_name"`
	MUCRestrictRoomCreation string `gorm:"column:muc_restrict_room_creation"`
	MUCAdmins               string `gorm:"column:muc_admins"`
	UsePastebin             string `gorm:"column:use_pastebin"`
	PastebinExpireAfter     int32  `gorm:"column:pastebin_expire_after"`
	PastebinTrigger         string `gorm:"column:pastebin_trigger"`
	UseHTTPArchive          string `gorm:"column:use_http_archive"`
	HTTPArchiveShowJoin     string `gorm:"column:http_archive_show_join"`
	HTTPArchiveShowStatus   string `gorm:"column:http_archive_show_status"`
	UseStatusHost           string `gorm:"column:use_status_host"`
	SSLState                string `gorm:"column:ssl_state"`
	SSLLocality             string `gorm:"column:ssl_locality"`
	SSLOrganisation         string `gorm:"column:ssl_organisation"`
	SSLOrganisationUnit     string `gorm:"column:ssl_organisation_unit"`
	SSLCountry              string `gorm:"column:ssl_country"`
	SSLEmail                string `gorm:"column:ssl_email"`
	SSLRequest              string `gorm:"column:ssl_request"`
	SSLCert                 string `gorm:"column:ssl_cert"`
	SSLBundle               string `gorm:"column:ssl_bundle"`
	SSLKey                  string `gorm:"column:ssl_key"`
	SSLAction               string `gorm:"column:ssl_action"`
	Active                  string `gorm:"column:active"`
}

// TableName maps XMPPDomain to the ISPConfig table xmpp_domain.
func (XMPPDomain) TableName() string { return "xmpp_domain" }

// XMPPUser is an XMPP account (table xmpp_user).
type XMPPUser struct {
	XMPPUserID   uint32 `gorm:"column:xmppuser_id;primaryKey;autoIncrement"`
	SysUserID    uint32 `gorm:"column:sys_userid"`
	SysGroupID   uint32 `gorm:"column:sys_groupid"`
	SysPermUser  string `gorm:"column:sys_perm_user"`
	SysPermGroup string `gorm:"column:sys_perm_group"`
	SysPermOther string `gorm:"column:sys_perm_other"`
	ServerID     uint32 `gorm:"column:server_id"`
	JID          string `gorm:"column:jid"`
	Password     string `gorm:"column:password"`
	Active       string `gorm:"column:active"`
}

// TableName maps XMPPUser to the ISPConfig table xmpp_user.
func (XMPPUser) TableName() string { return "xmpp_user" }
