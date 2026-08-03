package model

import "time"

// MailDomain maps the ISPConfig3 mail_domain table (one row per mail
// domain served by Postfix; DKIM key material lives on the row).
type MailDomain struct {
	DomainID      uint32 `gorm:"column:domain_id;primaryKey;autoIncrement"`
	SysUserID     uint32 `gorm:"column:sys_userid"`
	SysGroupID    uint32 `gorm:"column:sys_groupid"`
	SysPermUser   string `gorm:"column:sys_perm_user"`
	SysPermGroup  string `gorm:"column:sys_perm_group"`
	SysPermOther  string `gorm:"column:sys_perm_other"`
	ServerID      uint32 `gorm:"column:server_id"`
	Domain        string `gorm:"column:domain"`
	DKIM          string `gorm:"column:dkim;default:n"`
	DKIMSelector  string `gorm:"column:dkim_selector;default:default"`
	DKIMPrivate   string `gorm:"column:dkim_private"`
	DKIMPublic    string `gorm:"column:dkim_public"`
	RelayHost     string `gorm:"column:relay_host"`
	RelayUser     string `gorm:"column:relay_user"`
	RelayPass     string `gorm:"column:relay_pass"`
	Active        string `gorm:"column:active;default:n"`
	LocalDelivery string `gorm:"column:local_delivery;default:y"`
}

// TableName implements the GORM naming override.
func (MailDomain) TableName() string { return "mail_domain" }

// MailUser maps the ISPConfig3 mail_user table (mailboxes; Postfix and
// Dovecot read it live through the SQL maps).
type MailUser struct {
	MailuserID             uint32     `gorm:"column:mailuser_id;primaryKey;autoIncrement"`
	SysUserID              uint32     `gorm:"column:sys_userid"`
	SysGroupID             uint32     `gorm:"column:sys_groupid"`
	SysPermUser            string     `gorm:"column:sys_perm_user"`
	SysPermGroup           string     `gorm:"column:sys_perm_group"`
	SysPermOther           string     `gorm:"column:sys_perm_other"`
	ServerID               uint32     `gorm:"column:server_id"`
	Email                  string     `gorm:"column:email"`
	Login                  string     `gorm:"column:login"`
	Password               string     `gorm:"column:password"`
	Name                   string     `gorm:"column:name"`
	UID                    int32      `gorm:"column:uid;default:5000"`
	GID                    int32      `gorm:"column:gid;default:5000"`
	Maildir                string     `gorm:"column:maildir"`
	MaildirFormat          string     `gorm:"column:maildir_format;default:maildir"`
	Quota                  int64      `gorm:"column:quota"`
	CC                     string     `gorm:"column:cc"`
	ForwardInLda           string     `gorm:"column:forward_in_lda;default:n"`
	SenderCC               string     `gorm:"column:sender_cc"`
	Homedir                string     `gorm:"column:homedir"`
	Autoresponder          string     `gorm:"column:autoresponder;default:n"`
	AutoresponderStartDate *time.Time `gorm:"column:autoresponder_start_date"`
	AutoresponderEndDate   *time.Time `gorm:"column:autoresponder_end_date"`
	AutoresponderSubject   string     `gorm:"column:autoresponder_subject;default:Out of office reply"`
	AutoresponderText      string     `gorm:"column:autoresponder_text"`
	MoveJunk               string     `gorm:"column:move_junk;default:y"`
	PurgeTrashDays         int32      `gorm:"column:purge_trash_days"`
	PurgeJunkDays          int32      `gorm:"column:purge_junk_days"`
	CustomMailfilter       string     `gorm:"column:custom_mailfilter"`
	Postfix                string     `gorm:"column:postfix;default:y"`
	Greylisting            string     `gorm:"column:greylisting;default:n"`
	Access                 string     `gorm:"column:access;default:y"`
	DisableIMAP            string     `gorm:"column:disableimap;default:n"`
	DisablePOP3            string     `gorm:"column:disablepop3;default:n"`
	DisableDeliver         string     `gorm:"column:disabledeliver;default:n"`
	DisableSMTP            string     `gorm:"column:disablesmtp;default:n"`
	DisableSieve           string     `gorm:"column:disablesieve;default:n"`
	DisableSieveFilter     string     `gorm:"column:disablesieve-filter;default:n"`
	DisableLda             string     `gorm:"column:disablelda;default:n"`
	DisableLmtp            string     `gorm:"column:disablelmtp;default:n"`
	DisableDoveadm         string     `gorm:"column:disabledoveadm;default:n"`
	LastAccess             *int32     `gorm:"column:last_access"`
	DisableQuotaStatus     string     `gorm:"column:disablequota-status;default:n"`
	DisableIndexerWorker   string     `gorm:"column:disableindexer-worker;default:n"`
	DisableReplicator      string     `gorm:"column:disablereplicator;default:n"`
	LastQuotaNotification  *time.Time `gorm:"column:last_quota_notification"`
	BackupInterval         string     `gorm:"column:backup_interval;default:none"`
	BackupCopies           int32      `gorm:"column:backup_copies;default:1"`
	IMAPPrefix             string     `gorm:"column:imap_prefix"`
}

// TableName implements the GORM naming override.
func (MailUser) TableName() string { return "mail_user" }

// MailForwarding maps mail_forwarding (aliases, alias domains, forwards
// and catchalls discriminated by type).
type MailForwarding struct {
	ForwardingID uint32 `gorm:"column:forwarding_id;primaryKey;autoIncrement"`
	SysUserID    uint32 `gorm:"column:sys_userid"`
	SysGroupID   uint32 `gorm:"column:sys_groupid"`
	SysPermUser  string `gorm:"column:sys_perm_user"`
	SysPermGroup string `gorm:"column:sys_perm_group"`
	SysPermOther string `gorm:"column:sys_perm_other"`
	ServerID     uint32 `gorm:"column:server_id"`
	Source       string `gorm:"column:source"`
	Destination  string `gorm:"column:destination"`
	Type         string `gorm:"column:type;default:alias"`
	Active       string `gorm:"column:active;default:n"`
	AllowSendAs  string `gorm:"column:allow_send_as;default:n"`
	Greylisting  string `gorm:"column:greylisting;default:n"`
}

// TableName implements the GORM naming override.
func (MailForwarding) TableName() string { return "mail_forwarding" }

// MailTransport maps mail_transport (Postfix transport map rows).
type MailTransport struct {
	TransportID  uint32 `gorm:"column:transport_id;primaryKey;autoIncrement"`
	SysUserID    uint32 `gorm:"column:sys_userid"`
	SysGroupID   uint32 `gorm:"column:sys_groupid"`
	SysPermUser  string `gorm:"column:sys_perm_user"`
	SysPermGroup string `gorm:"column:sys_perm_group"`
	SysPermOther string `gorm:"column:sys_perm_other"`
	ServerID     uint32 `gorm:"column:server_id"`
	Domain       string `gorm:"column:domain"`
	Transport    string `gorm:"column:transport"`
	SortOrder    uint32 `gorm:"column:sort_order;default:5"`
	Active       string `gorm:"column:active;default:n"`
}

// TableName implements the GORM naming override.
func (MailTransport) TableName() string { return "mail_transport" }

// MailAccess maps mail_access (Postfix access map / Rspamd global
// white-blacklist rows).
type MailAccess struct {
	AccessID     uint32 `gorm:"column:access_id;primaryKey;autoIncrement"`
	SysUserID    uint32 `gorm:"column:sys_userid"`
	SysGroupID   uint32 `gorm:"column:sys_groupid"`
	SysPermUser  string `gorm:"column:sys_perm_user"`
	SysPermGroup string `gorm:"column:sys_perm_group"`
	SysPermOther string `gorm:"column:sys_perm_other"`
	ServerID     int32  `gorm:"column:server_id"`
	Source       string `gorm:"column:source"`
	Access       string `gorm:"column:access"`
	Type         string `gorm:"column:type;default:recipient"`
	Active       string `gorm:"column:active;default:y"`
}

// TableName implements the GORM naming override.
func (MailAccess) TableName() string { return "mail_access" }

// MailGet maps mail_get (external POP3/IMAP accounts fetched by getmail
// into a local mailbox). SourcePassword is stored in cleartext because
// getmail must present it to the remote server; it is never returned by
// the API (add-getmail-module design D7).
type MailGet struct {
	MailgetID      uint32 `gorm:"column:mailget_id;primaryKey;autoIncrement"`
	SysUserID      uint32 `gorm:"column:sys_userid"`
	SysGroupID     uint32 `gorm:"column:sys_groupid"`
	SysPermUser    string `gorm:"column:sys_perm_user"`
	SysPermGroup   string `gorm:"column:sys_perm_group"`
	SysPermOther   string `gorm:"column:sys_perm_other"`
	ServerID       uint32 `gorm:"column:server_id"`
	Type           string `gorm:"column:type;default:pop3"`
	SourceServer   string `gorm:"column:source_server"`
	SourceUsername string `gorm:"column:source_username"`
	SourcePassword string `gorm:"column:source_password"`
	SourceDelete   string `gorm:"column:source_delete;default:y"`
	SourceReadAll  string `gorm:"column:source_read_all;default:y"`
	Destination    string `gorm:"column:destination"`
	Active         string `gorm:"column:active;default:y"`
}

// TableName implements the GORM naming override.
func (MailGet) TableName() string { return "mail_get" }
