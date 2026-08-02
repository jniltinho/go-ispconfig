package getconf

import (
	"errors"
	"fmt"
	"reflect"

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

// DNSConfig is the typed [dns] section of server.config (Bind paths and
// ownership), key names as in server.ini.master.
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
	// default).
	DNSSECResignDays string `ini:"dnssec_resign_days"`
}

// ServerConfig is the parsed server.config of one server: the typed [web]
// and [dns] sections plus the raw section map for keys not (yet) typed.
type ServerConfig struct {
	Web WebConfig
	DNS DNSConfig
	Raw Sections
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
	cfg := &ServerConfig{Raw: raw}
	decodeSection(raw["web"], &cfg.Web)
	decodeSection(raw["dns"], &cfg.DNS)
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
