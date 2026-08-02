package model

import "time"

// Client is a customer record with contact data and per-service limits
// (table client). Limit columns use -1 for unlimited and 0 for disabled.
type Client struct {
	ClientID                uint32 `gorm:"column:client_id;primaryKey;autoIncrement"`
	SysUserID               uint32 `gorm:"column:sys_userid"`
	SysGroupID              uint32 `gorm:"column:sys_groupid"`
	SysPermUser             string `gorm:"column:sys_perm_user"`
	SysPermGroup            string `gorm:"column:sys_perm_group"`
	SysPermOther            string `gorm:"column:sys_perm_other"`
	CompanyName             string `gorm:"column:company_name"`
	CompanyID               string `gorm:"column:company_id"`
	Gender                  string `gorm:"column:gender"`
	ContactFirstname        string `gorm:"column:contact_firstname"`
	ContactName             string `gorm:"column:contact_name"`
	CustomerNo              string `gorm:"column:customer_no"`
	VatID                   string `gorm:"column:vat_id"`
	Street                  string `gorm:"column:street"`
	Zip                     string `gorm:"column:zip"`
	City                    string `gorm:"column:city"`
	State                   string `gorm:"column:state"`
	Country                 string `gorm:"column:country"`
	Telephone               string `gorm:"column:telephone"`
	Mobile                  string `gorm:"column:mobile"`
	Fax                     string `gorm:"column:fax"`
	Email                   string `gorm:"column:email"`
	Internet                string `gorm:"column:internet"`
	ICQ                     string `gorm:"column:icq"`
	Notes                   string `gorm:"column:notes"`
	BankAccountOwner        string `gorm:"column:bank_account_owner"`
	BankAccountNumber       string `gorm:"column:bank_account_number"`
	BankCode                string `gorm:"column:bank_code"`
	BankName                string `gorm:"column:bank_name"`
	BankAccountIban         string `gorm:"column:bank_account_iban"`
	BankAccountSwift        string `gorm:"column:bank_account_swift"`
	PaypalEmail             string `gorm:"column:paypal_email"`
	DefaultMailserver       uint32 `gorm:"column:default_mailserver"`
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
	DefaultXMPPServer       uint32 `gorm:"column:default_xmppserver"`
	XMPPServers             string `gorm:"column:xmpp_servers"`
	LimitXMPPDomain         int32  `gorm:"column:limit_xmpp_domain"`
	LimitXMPPUser           int32  `gorm:"column:limit_xmpp_user"`
	LimitXMPPMuc            string `gorm:"column:limit_xmpp_muc;default:n"`
	LimitXMPPAnon           string `gorm:"column:limit_xmpp_anon;default:n"`
	LimitXMPPAuthOptions    string `gorm:"column:limit_xmpp_auth_options"`
	LimitXMPPVjud           string `gorm:"column:limit_xmpp_vjud;default:n"`
	LimitXMPPProxy          string `gorm:"column:limit_xmpp_proxy;default:n"`
	LimitXMPPStatus         string `gorm:"column:limit_xmpp_status;default:n"`
	LimitXMPPPastebin       string `gorm:"column:limit_xmpp_pastebin;default:n"`
	LimitXMPPHttparchive    string `gorm:"column:limit_xmpp_httparchive;default:n"`
	DefaultWebserver        uint32 `gorm:"column:default_webserver"`
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
	DefaultDNSServer        uint32 `gorm:"column:default_dnsserver"`
	DBServers               string `gorm:"column:db_servers"`
	LimitDNSZone            int32  `gorm:"column:limit_dns_zone"`
	DefaultSlaveDNSServer   uint32 `gorm:"column:default_slave_dnsserver"`
	LimitDNSSlaveZone       int32  `gorm:"column:limit_dns_slave_zone"`
	LimitDNSRecord          int32  `gorm:"column:limit_dns_record"`
	DefaultDBServer         int32  `gorm:"column:default_dbserver"`
	DNSServers              string `gorm:"column:dns_servers"`
	LimitDatabase           int32  `gorm:"column:limit_database"`
	LimitDatabasePostgresql int32  `gorm:"column:limit_database_postgresql"`
	LimitDatabaseUser       int32  `gorm:"column:limit_database_user"`
	LimitDatabaseQuota      int32  `gorm:"column:limit_database_quota"`
	LimitCron               int32  `gorm:"column:limit_cron"`
	LimitCronType           string `gorm:"column:limit_cron_type;default:url"`
	LimitCronFrequency      int32  `gorm:"column:limit_cron_frequency"`
	LimitTrafficQuota       int32  `gorm:"column:limit_traffic_quota"`
	LimitClient             int32  `gorm:"column:limit_client"`
	LimitDomainmodule       int32  `gorm:"column:limit_domainmodule"`
	LimitMailmailinglist    int32  `gorm:"column:limit_mailmailinglist"`
	LimitOpenvzVM           int32  `gorm:"column:limit_openvz_vm"`
	LimitOpenvzVMTemplateID int32  `gorm:"column:limit_openvz_vm_template_id"`
	ParentClientID          uint32 `gorm:"column:parent_client_id"`
	Username                string `gorm:"column:username"`
	Password                string `gorm:"column:password"`
	Language                string `gorm:"column:language;default:en"`
	Usertheme               string `gorm:"column:usertheme;default:default"`
	TemplateMaster          uint32 `gorm:"column:template_master"`
	TemplateAdditional      string `gorm:"column:template_additional"`
	// autoCreateTime:false: the column is a unix-seconds bigint (DEFAULT
	// NULL in the DDL); gorm's CreatedAt name convention would try to
	// write a time.Time into it on insert.
	CreatedAt          *int64     `gorm:"column:created_at;autoCreateTime:false"`
	Locked             string     `gorm:"column:locked;default:n"`
	Canceled           string     `gorm:"column:canceled;default:n"`
	CanUseAPI          string     `gorm:"column:can_use_api;default:n"`
	TmpData            []byte     `gorm:"column:tmp_data"`
	IDRsa              string     `gorm:"column:id_rsa"`
	SSHRsa             string     `gorm:"column:ssh_rsa"`
	CustomerNoTemplate string     `gorm:"column:customer_no_template"`
	CustomerNoStart    int32      `gorm:"column:customer_no_start"`
	CustomerNoCounter  int32      `gorm:"column:customer_no_counter"`
	AddedDate          *time.Time `gorm:"column:added_date"`
	AddedBy            string     `gorm:"column:added_by"`
	ValidationStatus   string     `gorm:"column:validation_status;default:accept"`
	RiskScore          uint32     `gorm:"column:risk_score"`
	ActivationCode     string     `gorm:"column:activation_code"`
}

// TableName maps Client to the ISPConfig table client.
func (Client) TableName() string { return "client" }
