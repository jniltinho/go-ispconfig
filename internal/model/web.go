package model

import "time"

// WebDomain is a website, subdomain or alias domain with its vhost, PHP,
// SSL and backup settings (table web_domain). Type distinguishes
// vhost/vhostsubdomain/vhostalias records sharing this table.
type WebDomain struct {
	DomainID              uint32 `gorm:"column:domain_id;primaryKey;autoIncrement"`
	SysUserID             uint32 `gorm:"column:sys_userid"`
	SysGroupID            uint32 `gorm:"column:sys_groupid"`
	SysPermUser           string `gorm:"column:sys_perm_user"`
	SysPermGroup          string `gorm:"column:sys_perm_group"`
	SysPermOther          string `gorm:"column:sys_perm_other"`
	ServerID              uint32 `gorm:"column:server_id"`
	IPAddress             string `gorm:"column:ip_address"`
	IPv6Address           string `gorm:"column:ipv6_address"`
	Domain                string `gorm:"column:domain"`
	Type                  string `gorm:"column:type"`
	ParentDomainID        uint32 `gorm:"column:parent_domain_id"`
	VhostType             string `gorm:"column:vhost_type"`
	DocumentRoot          string `gorm:"column:document_root"`
	WebFolder             string `gorm:"column:web_folder"`
	SystemUser            string `gorm:"column:system_user"`
	SystemGroup           string `gorm:"column:system_group"`
	HdQuota               int64  `gorm:"column:hd_quota"`
	TrafficQuota          int64  `gorm:"column:traffic_quota"`
	CGI                   string `gorm:"column:cgi"`
	SSI                   string `gorm:"column:ssi"`
	Suexec                string `gorm:"column:suexec"`
	Errordocs             int8   `gorm:"column:errordocs"`
	IsSubdomainWWW        int8   `gorm:"column:is_subdomainwww"`
	Subdomain             string `gorm:"column:subdomain"`
	PHP                   string `gorm:"column:php"`
	Ruby                  string `gorm:"column:ruby"`
	Python                string `gorm:"column:python"`
	Perl                  string `gorm:"column:perl"`
	RedirectType          string `gorm:"column:redirect_type"`
	RedirectPath          string `gorm:"column:redirect_path"`
	SeoRedirect           string `gorm:"column:seo_redirect"`
	RewriteToHTTPS        string `gorm:"column:rewrite_to_https"`
	SSL                   string `gorm:"column:ssl"`
	SSLLetsencrypt        string `gorm:"column:ssl_letsencrypt"`
	SSLLetsencryptExclude string `gorm:"column:ssl_letsencrypt_exclude"`
	SSLState              string `gorm:"column:ssl_state"`
	SSLLocality           string `gorm:"column:ssl_locality"`
	SSLOrganisation       string `gorm:"column:ssl_organisation"`
	SSLOrganisationUnit   string `gorm:"column:ssl_organisation_unit"`
	SSLCountry            string `gorm:"column:ssl_country"`
	SSLDomain             string `gorm:"column:ssl_domain"`
	SSLRequest            string `gorm:"column:ssl_request"`
	SSLCert               string `gorm:"column:ssl_cert"`
	SSLBundle             string `gorm:"column:ssl_bundle"`
	SSLKey                string `gorm:"column:ssl_key"`
	SSLAction             string `gorm:"column:ssl_action"`
	StatsPassword         string `gorm:"column:stats_password"`
	StatsType             string `gorm:"column:stats_type"`
	AllowOverride         string `gorm:"column:allow_override"`
	ApacheDirectives      string `gorm:"column:apache_directives"`
	NginxDirectives       string `gorm:"column:nginx_directives"`
	PHPFPMUseSocket       string `gorm:"column:php_fpm_use_socket"`
	PHPFPMChroot          string `gorm:"column:php_fpm_chroot"`
	PM                    string `gorm:"column:pm"`
	PMMaxChildren         int32  `gorm:"column:pm_max_children"`
	PMStartServers        int32  `gorm:"column:pm_start_servers"`
	PMMinSpareServers     int32  `gorm:"column:pm_min_spare_servers"`
	PMMaxSpareServers     int32  `gorm:"column:pm_max_spare_servers"`
	PMProcessIdleTimeout  int32  `gorm:"column:pm_process_idle_timeout"`
	PMMaxRequests         int32  `gorm:"column:pm_max_requests"`
	PHPOpenBasedir        string `gorm:"column:php_open_basedir"`
	CustomPHPIni          string `gorm:"column:custom_php_ini"`
	BackupInterval        string `gorm:"column:backup_interval"`
	BackupCopies          int32  `gorm:"column:backup_copies"`
	BackupFormatWeb       string `gorm:"column:backup_format_web"`
	BackupFormatDB        string `gorm:"column:backup_format_db"`
	BackupEncrypt         string `gorm:"column:backup_encrypt"`
	BackupPassword        string `gorm:"column:backup_password"`
	BackupExcludes        string `gorm:"column:backup_excludes"`
	Active                string `gorm:"column:active"`
	// default tag: not a form field; the DB default 'n' must apply on
	// insert (a zero-value "" would fail the NOT NULL enum in strict mode).
	TrafficQuotaLock         string     `gorm:"column:traffic_quota_lock;default:'n'"`
	ProxyDirectives          string     `gorm:"column:proxy_directives"`
	LastQuotaNotification    *time.Time `gorm:"column:last_quota_notification"`
	RewriteRules             string     `gorm:"column:rewrite_rules"`
	AddedDate                *time.Time `gorm:"column:added_date"`
	AddedBy                  string     `gorm:"column:added_by"`
	DirectiveSnippetsID      uint32     `gorm:"column:directive_snippets_id"`
	EnablePagespeed          string     `gorm:"column:enable_pagespeed"`
	HTTPPort                 uint32     `gorm:"column:http_port"`
	HTTPSPort                uint32     `gorm:"column:https_port"`
	FolderDirectiveSnippets  string     `gorm:"column:folder_directive_snippets"`
	LogRetention             int32      `gorm:"column:log_retention"`
	ProxyProtocol            string     `gorm:"column:proxy_protocol"`
	ServerPHPID              uint32     `gorm:"column:server_php_id"`
	JailkitChrootAppSections string     `gorm:"column:jailkit_chroot_app_sections"`
	JailkitChrootAppPrograms string     `gorm:"column:jailkit_chroot_app_programs"`
	DeleteUnusedJailkit      string     `gorm:"column:delete_unused_jailkit"`
	LastJailkitUpdate        *time.Time `gorm:"column:last_jailkit_update"`
	LastJailkitHash          string     `gorm:"column:last_jailkit_hash"`
	DisableSymlinknotowner   string     `gorm:"column:disable_symlinknotowner"`
}

// TableName maps WebDomain to the ISPConfig table web_domain.
func (WebDomain) TableName() string { return "web_domain" }

// WebFolder is a password-protected folder inside a website
// (table web_folder).
type WebFolder struct {
	WebFolderID    int64  `gorm:"column:web_folder_id;primaryKey;autoIncrement"`
	SysUserID      int32  `gorm:"column:sys_userid"`
	SysGroupID     int32  `gorm:"column:sys_groupid"`
	SysPermUser    string `gorm:"column:sys_perm_user"`
	SysPermGroup   string `gorm:"column:sys_perm_group"`
	SysPermOther   string `gorm:"column:sys_perm_other"`
	ServerID       int32  `gorm:"column:server_id"`
	ParentDomainID int32  `gorm:"column:parent_domain_id"`
	Path           string `gorm:"column:path"`
	Active         string `gorm:"column:active"`
}

// TableName maps WebFolder to the ISPConfig table web_folder.
func (WebFolder) TableName() string { return "web_folder" }

// WebFolderUser is an HTTP auth user of a protected folder
// (table web_folder_user).
type WebFolderUser struct {
	WebFolderUserID int64  `gorm:"column:web_folder_user_id;primaryKey;autoIncrement"`
	SysUserID       int32  `gorm:"column:sys_userid"`
	SysGroupID      int32  `gorm:"column:sys_groupid"`
	SysPermUser     string `gorm:"column:sys_perm_user"`
	SysPermGroup    string `gorm:"column:sys_perm_group"`
	SysPermOther    string `gorm:"column:sys_perm_other"`
	ServerID        int32  `gorm:"column:server_id"`
	WebFolderID     int32  `gorm:"column:web_folder_id"`
	Username        string `gorm:"column:username"`
	Password        string `gorm:"column:password"`
	Active          string `gorm:"column:active"`
}

// TableName maps WebFolderUser to the ISPConfig table web_folder_user.
func (WebFolderUser) TableName() string { return "web_folder_user" }

// FTPUser is a virtual FTP account of a website (table ftp_user).
// PureFTPd authenticates against this table directly (design D2), so the
// daemon never creates an OS account for it: uid/gid are the site's
// system_user/system_group as plain strings, exactly as ISPConfig stores
// them.
type FTPUser struct {
	FTPUserID      uint32 `gorm:"column:ftp_user_id;primaryKey;autoIncrement"`
	SysUserID      uint32 `gorm:"column:sys_userid"`
	SysGroupID     uint32 `gorm:"column:sys_groupid"`
	SysPermUser    string `gorm:"column:sys_perm_user"`
	SysPermGroup   string `gorm:"column:sys_perm_group"`
	SysPermOther   string `gorm:"column:sys_perm_other"`
	ServerID       uint32 `gorm:"column:server_id"`
	ParentDomainID uint32 `gorm:"column:parent_domain_id"`
	Username       string `gorm:"column:username"`
	UsernamePrefix string `gorm:"column:username_prefix"`
	Password       string `gorm:"column:password"`
	QuotaSize      int64  `gorm:"column:quota_size"`
	// default tags: NOT NULL enum/set columns whose Go zero value ("")
	// is not a valid member under strict mode.
	Active      string     `gorm:"column:active;default:y"`
	UID         string     `gorm:"column:uid"`
	GID         string     `gorm:"column:gid"`
	Dir         string     `gorm:"column:dir"`
	QuotaFiles  int64      `gorm:"column:quota_files"`
	ULRatio     int32      `gorm:"column:ul_ratio"`
	DLRatio     int32      `gorm:"column:dl_ratio"`
	ULBandwidth int32      `gorm:"column:ul_bandwidth"`
	DLBandwidth int32      `gorm:"column:dl_bandwidth"`
	Expires     *time.Time `gorm:"column:expires"`
	UserType    string     `gorm:"column:user_type;default:user"`
	UserConfig  string     `gorm:"column:user_config"`
}

// TableName maps FTPUser to the ISPConfig table ftp_user.
func (FTPUser) TableName() string { return "ftp_user" }

// ShellUser is a (optionally jailkit-chrooted) SSH account of a website
// (table shell_user). Unlike FTPUser this maps onto a real system account
// created by the daemon; puser/pgroup are the site's system user/group.
type ShellUser struct {
	ShellUserID    uint32 `gorm:"column:shell_user_id;primaryKey;autoIncrement"`
	SysUserID      uint32 `gorm:"column:sys_userid"`
	SysGroupID     uint32 `gorm:"column:sys_groupid"`
	SysPermUser    string `gorm:"column:sys_perm_user"`
	SysPermGroup   string `gorm:"column:sys_perm_group"`
	SysPermOther   string `gorm:"column:sys_perm_other"`
	ServerID       uint32 `gorm:"column:server_id"`
	ParentDomainID uint32 `gorm:"column:parent_domain_id"`
	Username       string `gorm:"column:username"`
	UsernamePrefix string `gorm:"column:username_prefix"`
	Password       string `gorm:"column:password"`
	QuotaSize      int64  `gorm:"column:quota_size"`
	Active         string `gorm:"column:active;default:y"`
	PUser          string `gorm:"column:puser"`
	PGroup         string `gorm:"column:pgroup"`
	Shell          string `gorm:"column:shell;default:/bin/bash"`
	Dir            string `gorm:"column:dir"`
	Chroot         string `gorm:"column:chroot"`
	SSHRsa         string `gorm:"column:ssh_rsa"`
}

// TableName maps ShellUser to the ISPConfig table shell_user.
func (ShellUser) TableName() string { return "shell_user" }
