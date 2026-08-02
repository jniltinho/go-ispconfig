package model

// Firewall maps the ISPConfig3 firewall table (one row per server, UFW
// frontend: tcp_port / udp_port / active). The iptables table is
// intentionally NOT modelled here — it is a schema-compatibility no-op
// (add-firewall-module design D7).
type Firewall struct {
	FirewallID   uint32 `gorm:"column:firewall_id;primaryKey;autoIncrement"`
	SysUserID    uint32 `gorm:"column:sys_userid"`
	SysGroupID   uint32 `gorm:"column:sys_groupid"`
	SysPermUser  string `gorm:"column:sys_perm_user"`
	SysPermGroup string `gorm:"column:sys_perm_group"`
	SysPermOther string `gorm:"column:sys_perm_other"`
	ServerID     uint32 `gorm:"column:server_id"`
	TCPPort      string `gorm:"column:tcp_port"`
	UDPPort      string `gorm:"column:udp_port"`
	Active       string `gorm:"column:active;default:y"`
}

// TableName implements the GORM naming override.
func (Firewall) TableName() string { return "firewall" }

// DBHistory opts the firewall row into sys_datalog journaling (port of
// firewall.tform.php "db_history" => true). Every create/update/delete
// writes a {old,new} diff the daemon later consumes as firewall_*
// events.
func (Firewall) DBHistory() bool { return true }
