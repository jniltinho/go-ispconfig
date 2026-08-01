package model

// Server is a managed host with its service flags and serialized INI config
// (table server). Updated is the last sys_datalog id processed by the
// daemon; Config is parsed by package getconf.
type Server struct {
	ServerID       uint32 `gorm:"column:server_id;primaryKey;autoIncrement"`
	SysUserID      uint32 `gorm:"column:sys_userid"`
	SysGroupID     uint32 `gorm:"column:sys_groupid"`
	SysPermUser    string `gorm:"column:sys_perm_user"`
	SysPermGroup   string `gorm:"column:sys_perm_group"`
	SysPermOther   string `gorm:"column:sys_perm_other"`
	ServerName     string `gorm:"column:server_name"`
	MailServer     int8   `gorm:"column:mail_server"`
	WebServer      int8   `gorm:"column:web_server"`
	DNSServer      int8   `gorm:"column:dns_server"`
	FileServer     int8   `gorm:"column:file_server"`
	DBServer       int8   `gorm:"column:db_server"`
	VserverServer  int8   `gorm:"column:vserver_server"`
	ProxyServer    int8   `gorm:"column:proxy_server"`
	FirewallServer int8   `gorm:"column:firewall_server"`
	XMPPServer     int8   `gorm:"column:xmpp_server"`
	Config         string `gorm:"column:config"`
	Updated        uint64 `gorm:"column:updated"`
	MirrorServerID uint32 `gorm:"column:mirror_server_id"`
	DBVersion      uint32 `gorm:"column:dbversion"`
	Active         int8   `gorm:"column:active"`
}

// TableName maps Server to the ISPConfig table server.
func (Server) TableName() string { return "server" }

// ServerIP is an IP address assigned to a server for vhost binding
// (table server_ip).
type ServerIP struct {
	ServerIPID      uint32 `gorm:"column:server_ip_id;primaryKey;autoIncrement"`
	SysUserID       uint32 `gorm:"column:sys_userid"`
	SysGroupID      uint32 `gorm:"column:sys_groupid"`
	SysPermUser     string `gorm:"column:sys_perm_user"`
	SysPermGroup    string `gorm:"column:sys_perm_group"`
	SysPermOther    string `gorm:"column:sys_perm_other"`
	ServerID        uint32 `gorm:"column:server_id"`
	ClientID        uint32 `gorm:"column:client_id"`
	IPType          string `gorm:"column:ip_type"`
	IPAddress       string `gorm:"column:ip_address"`
	Virtualhost     string `gorm:"column:virtualhost"`
	VirtualhostPort string `gorm:"column:virtualhost_port"`
}

// TableName maps ServerIP to the ISPConfig table server_ip.
func (ServerIP) TableName() string { return "server_ip" }

// ServerPHP is an additional PHP version available on a server for
// per-site selection (table server_php).
type ServerPHP struct {
	ServerPHPID       uint32 `gorm:"column:server_php_id;primaryKey;autoIncrement"`
	SysUserID         uint32 `gorm:"column:sys_userid"`
	SysGroupID        uint32 `gorm:"column:sys_groupid"`
	SysPermUser       string `gorm:"column:sys_perm_user"`
	SysPermGroup      string `gorm:"column:sys_perm_group"`
	SysPermOther      string `gorm:"column:sys_perm_other"`
	ServerID          uint32 `gorm:"column:server_id"`
	ClientID          uint32 `gorm:"column:client_id"`
	Name              string `gorm:"column:name"`
	PHPFastCGIBinary  string `gorm:"column:php_fastcgi_binary"`
	PHPFastCGIIniDir  string `gorm:"column:php_fastcgi_ini_dir"`
	PHPFPMInitScript  string `gorm:"column:php_fpm_init_script"`
	PHPFPMIniDir      string `gorm:"column:php_fpm_ini_dir"`
	PHPFPMPoolDir     string `gorm:"column:php_fpm_pool_dir"`
	PHPFPMSocketDir   string `gorm:"column:php_fpm_socket_dir"`
	PHPCLIBinary      string `gorm:"column:php_cli_binary"`
	PHPJailkitSection string `gorm:"column:php_jk_section"`
	Active            string `gorm:"column:active"`
	SortPrio          int64  `gorm:"column:sortprio"`
}

// TableName maps ServerPHP to the ISPConfig table server_php.
func (ServerPHP) TableName() string { return "server_php" }
