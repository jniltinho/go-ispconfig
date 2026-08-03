package getconf

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"gorm.io/gorm"

	"go-ispconfig/internal/model"
)

// WebConfig is the typed [web] section of server.config. All values are kept
// as strings, exactly as ISPConfig stores them ('y'/'n' flags, numbers,
// paths with [placeholders]); key names follow server.ini.master.
type WebConfig struct {
	ServerType                  string `ini:"server_type"`
	WebsiteBasedir              string `ini:"website_basedir"`
	WebsitePath                 string `ini:"website_path"`
	WebsiteSymlinks             string `ini:"website_symlinks"`
	WebsiteSymlinksRel          string `ini:"website_symlinks_rel"`
	NetworkFilesystem           string `ini:"network_filesystem"`
	VhostRewriteV6              string `ini:"vhost_rewrite_v6"`
	VhostConfDir                string `ini:"vhost_conf_dir"`
	VhostConfEnabledDir         string `ini:"vhost_conf_enabled_dir"`
	ApacheInitScript            string `ini:"apache_init_script"`
	NginxVhostConfDir           string `ini:"nginx_vhost_conf_dir"`
	NginxVhostConfEnabledDir    string `ini:"nginx_vhost_conf_enabled_dir"`
	SecurityLevel               string `ini:"security_level"`
	User                        string `ini:"user"`
	Group                       string `ini:"group"`
	NginxUser                   string `ini:"nginx_user"`
	NginxGroup                  string `ini:"nginx_group"`
	AppsVhostEnabled            string `ini:"apps_vhost_enabled"`
	AppsVhostPort               string `ini:"apps_vhost_port"`
	AppsVhostIP                 string `ini:"apps_vhost_ip"`
	AppsVhostServername         string `ini:"apps_vhost_servername"`
	PHPOpenBasedir              string `ini:"php_open_basedir"`
	HtaccessAllowOverride       string `ini:"htaccess_allow_override"`
	AwstatsConfDir              string `ini:"awstats_conf_dir"`
	AwstatsDataDir              string `ini:"awstats_data_dir"`
	AwstatsPl                   string `ini:"awstats_pl"`
	AwstatsBuildstaticpagesPl   string `ini:"awstats_buildstaticpages_pl"`
	PHPIniPathApache            string `ini:"php_ini_path_apache"`
	PHPIniPathCGI               string `ini:"php_ini_path_cgi"`
	CheckApacheConfig           string `ini:"check_apache_config"`
	EnableSNI                   string `ini:"enable_sni"`
	SkipLeCheck                 string `ini:"skip_le_check"`
	EnableIPWildcard            string `ini:"enable_ip_wildcard"`
	OvertrafficNotifyAdmin      string `ini:"overtraffic_notify_admin"`
	OvertrafficNotifyReseller   string `ini:"overtraffic_notify_reseller"`
	OvertrafficNotifyClient     string `ini:"overtraffic_notify_client"`
	NginxCGISocket              string `ini:"nginx_cgi_socket"`
	PHPFPMInitScript            string `ini:"php_fpm_init_script"`
	PHPFPMIniPath               string `ini:"php_fpm_ini_path"`
	PHPFPMPoolDir               string `ini:"php_fpm_pool_dir"`
	PHPFPMStartPort             string `ini:"php_fpm_start_port"`
	PHPFPMSocketDir             string `ini:"php_fpm_socket_dir"`
	PHPDefaultHide              string `ini:"php_default_hide"`
	PHPDefaultName              string `ini:"php_default_name"`
	SetFolderPermissionsOnUpd   string `ini:"set_folder_permissions_on_update"`
	AddWebUsersToSshusersGroup  string `ini:"add_web_users_to_sshusers_group"`
	ConnectUserIDToWebID        string `ini:"connect_userid_to_webid"`
	ConnectUserIDToWebIDStart   string `ini:"connect_userid_to_webid_start"`
	WebFolderProtection         string `ini:"web_folder_protection"`
	WebFolderPermission         string `ini:"web_folder_permission"`
	PHPIniCheckMinutes          string `ini:"php_ini_check_minutes"`
	OvertrafficDisableWeb       string `ini:"overtraffic_disable_web"`
	OverquotaNotifyThreshold    string `ini:"overquota_notify_threshold"`
	OverquotaNotifyAdmin        string `ini:"overquota_notify_admin"`
	OverquotaNotifyReseller     string `ini:"overquota_notify_reseller"`
	OverquotaNotifyClient       string `ini:"overquota_notify_client"`
	OverquotaNotifyFreq         string `ini:"overquota_notify_freq"`
	OverquotaDBNotifyThreshold  string `ini:"overquota_db_notify_threshold"`
	OverquotaDBNotifyAdmin      string `ini:"overquota_db_notify_admin"`
	OverquotaDBNotifyReseller   string `ini:"overquota_db_notify_reseller"`
	OverquotaDBNotifyClient     string `ini:"overquota_db_notify_client"`
	OverquotaNotifyOnOk         string `ini:"overquota_notify_onok"`
	Logging                     string `ini:"logging"`
	PHPFPMReloadMode            string `ini:"php_fpm_reload_mode"`
	PHPFPMDefaultChroot         string `ini:"php_fpm_default_chroot"`
	VhostProxyProtocolEnabled   string `ini:"vhost_proxy_protocol_enabled"`
	VhostProxyProtocolProtocols string `ini:"vhost_proxy_protocol_protocols"`
	VhostProxyProtocolHTTPPort  string `ini:"vhost_proxy_protocol_http_port"`
	VhostProxyProtocolHTTPSPort string `ini:"vhost_proxy_protocol_https_port"`
	LeSignatureType             string `ini:"le_signature_type"`
	LeDeleteOnSiteRemove        string `ini:"le_delete_on_site_remove"`
	LeAutoCleanup               string `ini:"le_auto_cleanup"`
	LeRevokeBeforeDelete        string `ini:"le_revoke_before_delete"`
	LeAutoCleanupDenylist       string `ini:"le_auto_cleanup_denylist"`
}

// DNS backend identifiers for [dns] dns_backend (design D2 of
// add-dns-powerdns-module). Exactly one applying plugin is active per server.
const (
	DNSBackendBind     = "bind"
	DNSBackendPowerDNS = "powerdns"
)

// DefaultPowerDNSAXFRConf is the path written by restartPowerDNS for the
// global allow-axfr-ips list (PHP: /etc/powerdns/pdns.d/pdns.ispconfig-axfr).
const DefaultPowerDNSAXFRConf = "/etc/powerdns/pdns.d/pdns.ispconfig-axfr"

// DNSConfig is the typed [dns] section of server.config (Bind paths and
// ownership), key names as in server.ini.master plus Go additions for the
// PowerDNS backend (dns_backend, powerdns_axfr_conf).
type DNSConfig struct {
	BindUser               string `ini:"bind_user"`
	BindGroup              string `ini:"bind_group"`
	BindZonefilesDir       string `ini:"bind_zonefiles_dir"`
	BindKeyfilesDir        string `ini:"bind_keyfiles_dir"`
	BindZonefilesMasterPfx string `ini:"bind_zonefiles_masterprefix"`
	BindZonefilesSlavePfx  string `ini:"bind_zonefiles_slaveprefix"`
	NamedConfPath          string `ini:"named_conf_path"`
	NamedConfLocalPath     string `ini:"named_conf_local_path"`
	DisableBindLog         string `ini:"disable_bind_log"`
	// DNSSECResignDays is the dns_resign job threshold in days (Go
	// addition, design D6; empty or non-positive means the built-in
	// default). Bind backend only.
	DNSSECResignDays string `ini:"dnssec_resign_days"`
	// DNSBackend selects the applying DNS plugin: "bind" (default) or
	// "powerdns". Empty / unknown values normalize to bind.
	DNSBackend string `ini:"dns_backend"`
	// PowerDNSAXFRConf is the file rewritten on powerdns service restart
	// with allow-axfr-ips=… (default DefaultPowerDNSAXFRConf).
	PowerDNSAXFRConf string `ini:"powerdns_axfr_conf"`
}

// DefaultDNSConfig returns the Debian/Ubuntu defaults for the [dns]
// section. GetServerConfig applies them before decoding so missing keys
// (including dns_backend) behave as bind.
func DefaultDNSConfig() DNSConfig {
	return DNSConfig{
		BindUser:               "root",
		BindGroup:              "bind",
		BindZonefilesDir:       "/etc/bind",
		BindKeyfilesDir:        "/etc/bind",
		BindZonefilesMasterPfx: "pri.",
		BindZonefilesSlavePfx:  "slave/sec.",
		NamedConfPath:          "/etc/bind/named.conf",
		NamedConfLocalPath:     "/etc/bind/named.conf.local",
		DisableBindLog:         "n",
		DNSBackend:             DNSBackendBind,
		PowerDNSAXFRConf:       DefaultPowerDNSAXFRConf,
	}
}

// NormalizeDNSBackend maps empty/unknown values to "bind"; only "powerdns"
// (case-insensitive) selects the PowerDNS plugin.
func NormalizeDNSBackend(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case DNSBackendPowerDNS:
		return DNSBackendPowerDNS
	default:
		return DNSBackendBind
	}
}

// MailConfig is the typed [mail] section of server.config (design D13
// of add-mail-module). Values are strings exactly as ISPConfig stores
// them; key names follow server.ini.master.
type MailConfig struct {
	Module                   string `ini:"module"`
	MaildirPath              string `ini:"maildir_path"`
	HomedirPath              string `ini:"homedir_path"`
	MaildirFormat            string `ini:"maildir_format"`
	DKIMPath                 string `ini:"dkim_path"`
	DKIMStrength             string `ini:"dkim_strength"`
	ContentFilter            string `ini:"content_filter"`
	RspamdPassword           string `ini:"rspamd_password"`
	RspamdURL                string `ini:"rspamd_url"`
	RspamdRedisServers       string `ini:"rspamd_redis_servers"`
	RspamdRedisPasswd        string `ini:"rspamd_redis_passwd"`
	RspamdRedisBayesServers  string `ini:"rspamd_redis_bayes_servers"`
	RspamdRedisBayesPasswd   string `ini:"rspamd_redis_bayes_passwd"`
	// Global Rspamd action thresholds rendered into
	// local.d/actions.conf; per-identity settings files override them.
	RspamdSpamTagLevel     string `ini:"rspamd_spam_tag_level"`
	RspamdSpamKillLevel    string `ini:"rspamd_spam_kill_level"`
	RspamdGreylistingLevel string `ini:"rspamd_greylisting_level"`
	POP3IMAPDaemon           string `ini:"pop3_imap_daemon"`
	MailFilterSyntax         string `ini:"mail_filter_syntax"`
	MailuserUID              string `ini:"mailuser_uid"`
	MailuserGID              string `ini:"mailuser_gid"`
	MailuserName             string `ini:"mailuser_name"`
	MailuserGroup            string `ini:"mailuser_group"`
	MailboxVirtualUidgidMaps string `ini:"mailbox_virtual_uidgid_maps"`
	Relayhost                string `ini:"relayhost"`
	RelayhostUser            string `ini:"relayhost_user"`
	RelayhostPassword        string `ini:"relayhost_password"`
	MailboxSizeLimit         string `ini:"mailbox_size_limit"`
	MessageSizeLimit         string `ini:"message_size_limit"`
	// MailboxSoftDelete enables soft-deleted maildirs: 'y' or a positive
	// day count turn it on (the number doubles as purge retention);
	// '', '0' and 'n' disable.
	MailboxSoftDelete string `ini:"mailbox_soft_delete"`
	SendmailPath      string `ini:"sendmail_path"`
}

// DefaultMailConfig returns the Debian/Ubuntu defaults of this port:
// Dovecot + Rspamd (never courier/amavis), matching the installer seed.
// GetServerConfig applies them before decoding, so servers without a
// [mail] section (or with partial keys) behave sanely.
func DefaultMailConfig() MailConfig {
	return MailConfig{
		Module:                   "postfix_mysql",
		MaildirPath:              "/var/vmail/[domain]/[localpart]",
		HomedirPath:              "/var/vmail",
		MaildirFormat:            "maildir",
		DKIMPath:                 "/var/lib/rspamd/dkim",
		DKIMStrength:             "2048",
		ContentFilter:            "rspamd",
		POP3IMAPDaemon:           "dovecot",
		MailFilterSyntax:         "sieve",
		MailuserUID:              "5000",
		MailuserGID:              "5000",
		MailuserName:             "vmail",
		MailuserGroup:            "vmail",
		MailboxVirtualUidgidMaps: "n",
		RspamdRedisServers:       "127.0.0.1",
		RspamdRedisBayesServers:  "127.0.0.1",
		RspamdSpamTagLevel:       "6",
		RspamdSpamKillLevel:      "15",
		RspamdGreylistingLevel:   "4",
		MailboxSizeLimit:         "0",
		MessageSizeLimit:         "0",
		MailboxSoftDelete:        "0",
		SendmailPath:             "/usr/sbin/sendmail",
	}
}

// JailkitConfig is the typed [jailkit] section of server.config, consumed
// by the jailkit plugin of add-ftp-shell-module. Key names and defaults
// follow server.ini.master; jailkit_chroot_home keeps the [username]
// placeholder the plugin substitutes per shell user.
type JailkitConfig struct {
	ChrootHome               string `ini:"jailkit_chroot_home"`
	ChrootAppSections        string `ini:"jailkit_chroot_app_sections"`
	ChrootAppPrograms        string `ini:"jailkit_chroot_app_programs"`
	ChrootCronPrograms       string `ini:"jailkit_chroot_cron_programs"`
	ChrootAuthorizedKeysTmpl string `ini:"jailkit_chroot_authorized_keys_template"`
	Hardlinks                string `ini:"jailkit_hardlinks"`
}

// DefaultJailkitConfig returns the server.ini.master defaults of the
// [jailkit] section. GetServerConfig applies them before decoding, so a
// server whose config predates the section (or only overrides some keys)
// still builds usable jails.
func DefaultJailkitConfig() JailkitConfig {
	return JailkitConfig{
		ChrootHome: "/home/[username]",
		ChrootAppSections: "coreutils basicshell editors extendedshell netutils " +
			"ssh sftp scp jk_lsh mysql-client git",
		ChrootAppPrograms:        "lesspipe pico unzip zip patch which",
		ChrootCronPrograms:       "/usr/bin/php /usr/lib/php/ /usr/share/php/ /usr/share/zoneinfo/ /usr/bin/perl /usr/share/perl/",
		ChrootAuthorizedKeysTmpl: "/root/.ssh/authorized_keys",
		Hardlinks:                "allow",
	}
}

// ServerConfig is the parsed server.config of one server: the typed
// [web], [dns], [mail] and [jailkit] sections plus the raw section map for
// keys not (yet) typed.
type ServerConfig struct {
	Web     WebConfig
	DNS     DNSConfig
	Mail    MailConfig
	Jailkit JailkitConfig
	Raw     Sections
}

// ErrNotFound is returned when a requested server, sys_ini row or sys_config
// entry does not exist.
var ErrNotFound = errors.New("getconf: not found")

// GetServerConfig loads and parses the config INI of the given server
// (port of getconf::get_server_config).
func GetServerConfig(db *gorm.DB, serverID uint32) (*ServerConfig, error) {
	var server model.Server
	// COALESCE: server.config is `text` NULL; adopted ISPConfig databases can
	// hold NULL there, which must read as an empty INI, not an error.
	if err := db.Select("COALESCE(config, '') AS config").Take(&server, serverID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: server %d", ErrNotFound, serverID)
		}
		return nil, fmt.Errorf("loading server %d config: %w", serverID, err)
	}
	raw := ParseINI(StripSlashes(server.Config))
	cfg := &ServerConfig{
		Raw:     raw,
		DNS:     DefaultDNSConfig(),
		Mail:    DefaultMailConfig(),
		Jailkit: DefaultJailkitConfig(),
	}
	decodeSection(raw["web"], &cfg.Web)
	decodeSection(raw["dns"], &cfg.DNS)
	decodeSection(raw["mail"], &cfg.Mail)
	decodeSection(raw["jailkit"], &cfg.Jailkit)
	// Empty dns_backend (or garbage) must not leave the daemon without a
	// known applying plugin — normalize after decode.
	cfg.DNS.DNSBackend = NormalizeDNSBackend(cfg.DNS.DNSBackend)
	if cfg.DNS.PowerDNSAXFRConf == "" {
		cfg.DNS.PowerDNSAXFRConf = DefaultPowerDNSAXFRConf
	}
	return cfg, nil
}

// GetGlobalConfig loads and parses the panel-wide INI from sys_ini row 1
// (port of getconf::get_global_config).
func GetGlobalConfig(db *gorm.DB) (Sections, error) {
	var ini model.SysIni
	// COALESCE: sys_ini.config is `longtext` NULL in adopted databases.
	if err := db.Select("COALESCE(config, '') AS config").Take(&ini, 1).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: sys_ini row 1", ErrNotFound)
		}
		return nil, fmt.Errorf("loading sys_ini: %w", err)
	}
	return ParseINI(StripSlashes(ini.Config)), nil
}

// GetSysConfig reads one sys_config value by group and name.
func GetSysConfig(db *gorm.DB, group, name string) (string, error) {
	var entry model.SysConfig
	err := db.Where("`group` = ? AND `name` = ?", group, name).Take(&entry).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", fmt.Errorf("%w: sys_config %s.%s", ErrNotFound, group, name)
		}
		return "", fmt.Errorf("loading sys_config %s.%s: %w", group, name, err)
	}
	return entry.Value, nil
}

// decodeSection copies section values into the string fields of dst (a
// pointer to struct) guided by their `ini` tags; missing keys leave the
// zero value.
func decodeSection(section map[string]string, dst any) {
	v := reflect.ValueOf(dst).Elem()
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		key := t.Field(i).Tag.Get("ini")
		if val, ok := section[key]; ok {
			v.Field(i).SetString(val)
		}
	}
}
