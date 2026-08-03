package model

import "time"

// OpenVZIP is an IP address available to OpenVZ containers (table openvz_ip).
type OpenVZIP struct {
	IPAddressID  int64  `gorm:"column:ip_address_id;primaryKey;autoIncrement"`
	SysUserID    int32  `gorm:"column:sys_userid"`
	SysGroupID   int32  `gorm:"column:sys_groupid"`
	SysPermUser  string `gorm:"column:sys_perm_user"`
	SysPermGroup string `gorm:"column:sys_perm_group"`
	SysPermOther string `gorm:"column:sys_perm_other"`
	ServerID     int32  `gorm:"column:server_id"`
	IPAddress    string `gorm:"column:ip_address"`
	VMID         int32  `gorm:"column:vm_id"`
	Reserved     string `gorm:"column:reserved"`
	Additional   string `gorm:"column:additional"`
}

// TableName maps OpenVZIP to the ISPConfig table openvz_ip.
func (OpenVZIP) TableName() string { return "openvz_ip" }

// OpenVZOSTemplate is an OS template a container can be created from
// (table openvz_ostemplate).
type OpenVZOSTemplate struct {
	OSTemplateID int64  `gorm:"column:ostemplate_id;primaryKey;autoIncrement"`
	SysUserID    int32  `gorm:"column:sys_userid"`
	SysGroupID   int32  `gorm:"column:sys_groupid"`
	SysPermUser  string `gorm:"column:sys_perm_user"`
	SysPermGroup string `gorm:"column:sys_perm_group"`
	SysPermOther string `gorm:"column:sys_perm_other"`
	TemplateName string `gorm:"column:template_name"`
	TemplateFile string `gorm:"column:template_file"`
	ServerID     int32  `gorm:"column:server_id"`
	Allservers   string `gorm:"column:allservers"`
	Active       string `gorm:"column:active"`
	Description  string `gorm:"column:description"`
}

// TableName maps OpenVZOSTemplate to the ISPConfig table openvz_ostemplate.
func (OpenVZOSTemplate) TableName() string { return "openvz_ostemplate" }

// OpenVZTemplate is a resource profile applied to a container
// (table openvz_template).
type OpenVZTemplate struct {
	TemplateID   int64  `gorm:"column:template_id;primaryKey;autoIncrement"`
	SysUserID    int32  `gorm:"column:sys_userid"`
	SysGroupID   int32  `gorm:"column:sys_groupid"`
	SysPermUser  string `gorm:"column:sys_perm_user"`
	SysPermGroup string `gorm:"column:sys_perm_group"`
	SysPermOther string `gorm:"column:sys_perm_other"`
	TemplateName string `gorm:"column:template_name"`
	Diskspace    int32  `gorm:"column:diskspace"`
	Traffic      int32  `gorm:"column:traffic"`
	Bandwidth    int32  `gorm:"column:bandwidth"`
	RAM          int32  `gorm:"column:ram"`
	RAMBurst     int32  `gorm:"column:ram_burst"`
	CPUUnits     int32  `gorm:"column:cpu_units"`
	CPUNum       int32  `gorm:"column:cpu_num"`
	CPULimit     int32  `gorm:"column:cpu_limit"`
	IOPriority   int32  `gorm:"column:io_priority"`
	Active       string `gorm:"column:active"`
	Description  string `gorm:"column:description"`
	Numproc      string `gorm:"column:numproc"`
	Numtcpsock   string `gorm:"column:numtcpsock"`
	Numothersock string `gorm:"column:numothersock"`
	Vmguarpages  string `gorm:"column:vmguarpages"`
	Kmemsize     string `gorm:"column:kmemsize"`
	Tcpsndbuf    string `gorm:"column:tcpsndbuf"`
	Tcprcvbuf    string `gorm:"column:tcprcvbuf"`
	Othersockbuf string `gorm:"column:othersockbuf"`
	Dgramrcvbuf  string `gorm:"column:dgramrcvbuf"`
	Oomguarpages string `gorm:"column:oomguarpages"`
	Privvmpages  string `gorm:"column:privvmpages"`
	Lockedpages  string `gorm:"column:lockedpages"`
	Shmpages     string `gorm:"column:shmpages"`
	Physpages    string `gorm:"column:physpages"`
	Numfile      string `gorm:"column:numfile"`
	Avnumproc    string `gorm:"column:avnumproc"`
	Numflock     string `gorm:"column:numflock"`
	Numpty       string `gorm:"column:numpty"`
	Numsiginfo   string `gorm:"column:numsiginfo"`
	Dcachesize   string `gorm:"column:dcachesize"`
	Numiptent    string `gorm:"column:numiptent"`
	Swappages    string `gorm:"column:swappages"`
	Hostname     string `gorm:"column:hostname"`
	Nameserver   string `gorm:"column:nameserver"`
	CreateDNS    string `gorm:"column:create_dns"`
	Capability   string `gorm:"column:capability"`
	Features     string `gorm:"column:features"`
	Iptables     string `gorm:"column:iptables"`
	Custom       string `gorm:"column:custom"`
}

// TableName maps OpenVZTemplate to the ISPConfig table openvz_template.
func (OpenVZTemplate) TableName() string { return "openvz_template" }

// OpenVZTraffic is the daily traffic counter of a container
// (table openvz_traffic).
type OpenVZTraffic struct {
	VEID         int32      `gorm:"column:veid"`
	TrafficDate  *time.Time `gorm:"column:traffic_date"`
	TrafficBytes uint64     `gorm:"column:traffic_bytes"`
}

// TableName maps OpenVZTraffic to the ISPConfig table openvz_traffic.
func (OpenVZTraffic) TableName() string { return "openvz_traffic" }

// OpenVZVM is an OpenVZ virtual machine (table openvz_vm).
type OpenVZVM struct {
	VMID            int64      `gorm:"column:vm_id;primaryKey;autoIncrement"`
	SysUserID       int32      `gorm:"column:sys_userid"`
	SysGroupID      int32      `gorm:"column:sys_groupid"`
	SysPermUser     string     `gorm:"column:sys_perm_user"`
	SysPermGroup    string     `gorm:"column:sys_perm_group"`
	SysPermOther    string     `gorm:"column:sys_perm_other"`
	ServerID        int32      `gorm:"column:server_id"`
	VEID            uint32     `gorm:"column:veid"`
	OSTemplateID    int32      `gorm:"column:ostemplate_id"`
	TemplateID      int32      `gorm:"column:template_id"`
	IPAddress       string     `gorm:"column:ip_address"`
	Hostname        string     `gorm:"column:hostname"`
	VMPassword      string     `gorm:"column:vm_password"`
	StartBoot       string     `gorm:"column:start_boot"`
	Bootorder       int32      `gorm:"column:bootorder"`
	Active          string     `gorm:"column:active"`
	ActiveUntilDate *time.Time `gorm:"column:active_until_date"`
	Description     string     `gorm:"column:description"`
	Diskspace       int32      `gorm:"column:diskspace"`
	Traffic         int32      `gorm:"column:traffic"`
	Bandwidth       int32      `gorm:"column:bandwidth"`
	RAM             int32      `gorm:"column:ram"`
	RAMBurst        int32      `gorm:"column:ram_burst"`
	CPUUnits        int32      `gorm:"column:cpu_units"`
	CPUNum          int32      `gorm:"column:cpu_num"`
	CPULimit        int32      `gorm:"column:cpu_limit"`
	IOPriority      int32      `gorm:"column:io_priority"`
	Nameserver      string     `gorm:"column:nameserver"`
	CreateDNS       string     `gorm:"column:create_dns"`
	Capability      string     `gorm:"column:capability"`
	Features        string     `gorm:"column:features"`
	Iptabless       string     `gorm:"column:iptabless"`
	Config          string     `gorm:"column:config"`
	Custom          string     `gorm:"column:custom"`
}

// TableName maps OpenVZVM to the ISPConfig table openvz_vm.
func (OpenVZVM) TableName() string { return "openvz_vm" }
