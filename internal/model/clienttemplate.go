package model

// ClientTemplate is a reusable client limit template (table
// client_template). Column set mirrors the limit/default/server columns
// of Client; TemplateType is 'm' (master) or 'a' (additional). Numeric
// limits use ISPConfig semantics: -1 unlimited, 0 disabled.
type ClientTemplate struct {
	TemplateID              uint32 `gorm:"column:template_id;primaryKey;autoIncrement"`
	SysUserID               uint32 `gorm:"column:sys_userid"`
	SysGroupID              uint32 `gorm:"column:sys_groupid"`
	SysPermUser             string `gorm:"column:sys_perm_user"`
	SysPermGroup            string `gorm:"column:sys_perm_group"`
	SysPermOther            string `gorm:"column:sys_perm_other"`
	TemplateName            string `gorm:"column:template_name"`
	TemplateType            string `gorm:"column:template_type;default:m"`
	MailServers             string `gorm:"column:mail_servers"`
	LimitMaildomain         int32  `gorm:"column:limit_maildomain"`
	LimitMailbox            int32  `gorm:"column:limit_mailbox"`
	LimitMailalias          int32  `gorm:"column:limit_mailalias"`
	LimitMailaliasdomain    int32  `gorm:"column:limit_mailaliasdomain"`
	LimitMailforward        int32  `gorm:"column:limit_mailforward"`
	LimitMailcatchall       int32  `gorm:"column:limit_mailcatchall"`
	LimitMailrouting        int32  `gorm:"column:limit_mailrouting"`
	LimitMailWblist         int32  `gorm:"column:limit_mail_wblist"`
	LimitMailfilter         int32  `gorm:"column:limit_mailfilter"`
	LimitFetchmail          int32  `gorm:"column:limit_fetchmail"`
	LimitMailquota          int32  `gorm:"column:limit_mailquota"`
	LimitSpamfilterWblist   int32  `gorm:"column:limit_spamfilter_wblist"`
	LimitSpamfilterUser     int32  `gorm:"column:limit_spamfilter_user"`
	LimitSpamfilterPolicy   int32  `gorm:"column:limit_spamfilter_policy"`
	LimitMailBackup         string `gorm:"column:limit_mail_backup;default:y"`
	LimitRelayhost          string `gorm:"column:limit_relayhost;default:n"`
	DefaultXMPPServer       uint32 `gorm:"column:default_xmppserver;default:1"`
	XMPPServers             string `gorm:"column:xmpp_servers"`
	LimitXMPPDomain         int32  `gorm:"column:limit_xmpp_domain"`
	LimitXMPPUser           int32  `gorm:"column:limit_xmpp_user"`
	LimitXMPPMuc            string `gorm:"column:limit_xmpp_muc;default:n"`
	LimitXMPPAnon           string `gorm:"column:limit_xmpp_anon;default:n"`
	LimitXMPPVjud           string `gorm:"column:limit_xmpp_vjud;default:n"`
	LimitXMPPProxy          string `gorm:"column:limit_xmpp_proxy;default:n"`
	LimitXMPPStatus         string `gorm:"column:limit_xmpp_status;default:n"`
	LimitXMPPPastebin       string `gorm:"column:limit_xmpp_pastebin;default:n"`
	LimitXMPPHttparchive    string `gorm:"column:limit_xmpp_httparchive;default:n"`
	WebServers              string `gorm:"column:web_servers"`
	LimitWebIP              string `gorm:"column:limit_web_ip"`
	LimitWebDomain          int32  `gorm:"column:limit_web_domain"`
	LimitWebQuota           int32  `gorm:"column:limit_web_quota"`
	WebPHPOptions           string `gorm:"column:web_php_options"`
	LimitCGI                string `gorm:"column:limit_cgi;default:n"`
	LimitSSI                string `gorm:"column:limit_ssi;default:n"`
	LimitPerl               string `gorm:"column:limit_perl;default:n"`
	LimitRuby               string `gorm:"column:limit_ruby;default:n"`
	LimitPython             string `gorm:"column:limit_python;default:n"`
	ForceSuexec             string `gorm:"column:force_suexec;default:y"`
	LimitHterror            string `gorm:"column:limit_hterror;default:n"`
	LimitWildcard           string `gorm:"column:limit_wildcard;default:n"`
	LimitSSL                string `gorm:"column:limit_ssl;default:n"`
	LimitSSLLetsencrypt     string `gorm:"column:limit_ssl_letsencrypt;default:n"`
	LimitWebSubdomain       int32  `gorm:"column:limit_web_subdomain"`
	LimitWebAliasdomain     int32  `gorm:"column:limit_web_aliasdomain"`
	LimitFTPUser            int32  `gorm:"column:limit_ftp_user"`
	LimitShellUser          int32  `gorm:"column:limit_shell_user"`
	SSHChroot               string `gorm:"column:ssh_chroot"`
	LimitWebdavUser         int32  `gorm:"column:limit_webdav_user"`
	LimitBackup             string `gorm:"column:limit_backup;default:y"`
	LimitDirectiveSnippets  string `gorm:"column:limit_directive_snippets;default:n"`
	LimitAps                int32  `gorm:"column:limit_aps"`
	DNSServers              string `gorm:"column:dns_servers"`
	LimitDNSZone            int32  `gorm:"column:limit_dns_zone"`
	DefaultSlaveDNSServer   int32  `gorm:"column:default_slave_dnsserver"`
	LimitDNSSlaveZone       int32  `gorm:"column:limit_dns_slave_zone"`
	LimitDNSRecord          int32  `gorm:"column:limit_dns_record"`
	DBServers               string `gorm:"column:db_servers"`
	LimitDatabase           int32  `gorm:"column:limit_database"`
	LimitDatabasePostgresql int32  `gorm:"column:limit_database_postgresql"`
	LimitDatabaseUser       int32  `gorm:"column:limit_database_user"`
	LimitDatabaseQuota      int32  `gorm:"column:limit_database_quota"`
	LimitCron               int32  `gorm:"column:limit_cron"`
	LimitCronType           string `gorm:"column:limit_cron_type;default:url"`
	LimitCronFrequency      int32  `gorm:"column:limit_cron_frequency;default:5"`
	LimitTrafficQuota       int32  `gorm:"column:limit_traffic_quota"`
	LimitClient             int32  `gorm:"column:limit_client"`
	LimitDomainmodule       int32  `gorm:"column:limit_domainmodule"`
	LimitMailmailinglist    int32  `gorm:"column:limit_mailmailinglist"`
	LimitOpenvzVM           int32  `gorm:"column:limit_openvz_vm"`
	LimitOpenvzVMTemplateID int32  `gorm:"column:limit_openvz_vm_template_id"`
}

// TableName maps ClientTemplate to the ISPConfig table client_template.
func (ClientTemplate) TableName() string { return "client_template" }

// ClientTemplateAssigned links an additional limit template to a client
// (table client_template_assigned).
type ClientTemplateAssigned struct {
	AssignedTemplateID int64 `gorm:"column:assigned_template_id;primaryKey;autoIncrement"`
	ClientID           int64 `gorm:"column:client_id"`
	ClientTemplateID   int32 `gorm:"column:client_template_id"`
}

// TableName maps ClientTemplateAssigned to client_template_assigned.
func (ClientTemplateAssigned) TableName() string { return "client_template_assigned" }

// ClientMessageTemplate is an email template for client messaging
// (table client_message_template); TemplateType is welcome/gdpr/other.
type ClientMessageTemplate struct {
	ClientMessageTemplateID int64  `gorm:"column:client_message_template_id;primaryKey;autoIncrement"`
	SysUserID               int32  `gorm:"column:sys_userid"`
	SysGroupID              int32  `gorm:"column:sys_groupid"`
	SysPermUser             string `gorm:"column:sys_perm_user"`
	SysPermGroup            string `gorm:"column:sys_perm_group"`
	SysPermOther            string `gorm:"column:sys_perm_other"`
	TemplateType            string `gorm:"column:template_type"`
	TemplateName            string `gorm:"column:template_name"`
	Subject                 string `gorm:"column:subject"`
	Message                 string `gorm:"column:message"`
}

// TableName maps ClientMessageTemplate to client_message_template.
func (ClientMessageTemplate) TableName() string { return "client_message_template" }

// Country is the read-only ISO country lookup (table country) used by the
// client address forms.
type Country struct {
	ISO           string `gorm:"column:iso;primaryKey" json:"iso"`
	Name          string `gorm:"column:name" json:"name,omitempty"`
	PrintableName string `gorm:"column:printable_name" json:"printable_name"`
	ISO3          string `gorm:"column:iso3" json:"iso3,omitempty"`
	Numcode       *int16 `gorm:"column:numcode" json:"numcode,omitempty"`
	EU            string `gorm:"column:eu;default:n" json:"eu"`
}

// TableName maps Country to the ISPConfig table country.
func (Country) TableName() string { return "country" }

// ClientCircle groups clients so a reseller can act on several of them at
// once (table client_circle).
type ClientCircle struct {
	CircleID     int32  `gorm:"column:circle_id;primaryKey;autoIncrement"`
	SysUserID    int32  `gorm:"column:sys_userid"`
	SysGroupID   int32  `gorm:"column:sys_groupid"`
	SysPermUser  string `gorm:"column:sys_perm_user"`
	SysPermGroup string `gorm:"column:sys_perm_group"`
	SysPermOther string `gorm:"column:sys_perm_other"`
	CircleName   string `gorm:"column:circle_name"`
	ClientIds    string `gorm:"column:client_ids"`
	Description  string `gorm:"column:description"`
	Active       string `gorm:"column:active"`
}

// TableName maps ClientCircle to the ISPConfig table client_circle.
func (ClientCircle) TableName() string { return "client_circle" }
