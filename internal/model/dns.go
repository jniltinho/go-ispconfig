package model

import "time"

// DNSSoa is an authoritative DNS zone (table dns_soa). Origin is the FQDN
// with trailing dot; RenderedZone caches the last rendered Bind zone file.
// Unlike most ISPConfig tables, the Active, DNSSECInitialized and
// DNSSECWanted enums use uppercase 'N'/'Y' values.
type DNSSoa struct {
	ID                uint32 `gorm:"column:id;primaryKey;autoIncrement"`
	SysUserID         uint32 `gorm:"column:sys_userid"`
	SysGroupID        uint32 `gorm:"column:sys_groupid"`
	SysPermUser       string `gorm:"column:sys_perm_user"`
	SysPermGroup      string `gorm:"column:sys_perm_group"`
	SysPermOther      string `gorm:"column:sys_perm_other"`
	ServerID          int32  `gorm:"column:server_id"`
	Origin            string `gorm:"column:origin"`
	NS                string `gorm:"column:ns"`
	Mbox              string `gorm:"column:mbox"`
	Serial            uint32 `gorm:"column:serial"`
	Refresh           uint32 `gorm:"column:refresh"`
	Retry             uint32 `gorm:"column:retry"`
	Expire            uint32 `gorm:"column:expire"`
	Minimum           uint32 `gorm:"column:minimum"`
	TTL               uint32 `gorm:"column:ttl"`
	Active            string `gorm:"column:active"`
	Xfer              string `gorm:"column:xfer"`
	AlsoNotify        string `gorm:"column:also_notify"`
	UpdateACL         string `gorm:"column:update_acl"`
	DNSSECInitialized string `gorm:"column:dnssec_initialized"`
	DNSSECWanted      string `gorm:"column:dnssec_wanted"`
	DNSSECAlgo        string `gorm:"column:dnssec_algo"`
	DNSSECLastSigned  int64  `gorm:"column:dnssec_last_signed"`
	DNSSECInfo        string `gorm:"column:dnssec_info"`
	RenderedZone      string `gorm:"column:rendered_zone"`
}

// TableName maps DNSSoa to the ISPConfig table dns_soa.
func (DNSSoa) TableName() string { return "dns_soa" }

// DNSRr is a resource record inside a zone (table dns_rr). Zone references
// dns_soa.id; Aux carries the MX/SRV priority. Stamp and Serial are pointers:
// serial is DEFAULT NULL in the DDL, and a nil Stamp lets the database apply
// its CURRENT_TIMESTAMP default on insert.
type DNSRr struct {
	ID           uint32     `gorm:"column:id;primaryKey;autoIncrement"`
	SysUserID    uint32     `gorm:"column:sys_userid"`
	SysGroupID   uint32     `gorm:"column:sys_groupid"`
	SysPermUser  string     `gorm:"column:sys_perm_user"`
	SysPermGroup string     `gorm:"column:sys_perm_group"`
	SysPermOther string     `gorm:"column:sys_perm_other"`
	ServerID     int32      `gorm:"column:server_id"`
	Zone         uint32     `gorm:"column:zone"`
	Name         string     `gorm:"column:name"`
	Type         string     `gorm:"column:type"`
	Data         string     `gorm:"column:data"`
	Aux          uint32     `gorm:"column:aux"`
	TTL          uint32     `gorm:"column:ttl"`
	Active       string     `gorm:"column:active"`
	Stamp        *time.Time `gorm:"column:stamp;default:CURRENT_TIMESTAMP"`
	Serial       *uint32    `gorm:"column:serial"`
}

// TableName maps DNSRr to the ISPConfig table dns_rr.
func (DNSRr) TableName() string { return "dns_rr" }

// DNSSlave is a secondary zone transferred from an external master
// (table dns_slave).
type DNSSlave struct {
	ID           uint32 `gorm:"column:id;primaryKey;autoIncrement"`
	SysUserID    uint32 `gorm:"column:sys_userid"`
	SysGroupID   uint32 `gorm:"column:sys_groupid"`
	SysPermUser  string `gorm:"column:sys_perm_user"`
	SysPermGroup string `gorm:"column:sys_perm_group"`
	SysPermOther string `gorm:"column:sys_perm_other"`
	ServerID     int32  `gorm:"column:server_id"`
	Origin       string `gorm:"column:origin"`
	NS           string `gorm:"column:ns"`
	Active       string `gorm:"column:active"`
	Xfer         string `gorm:"column:xfer"`
}

// TableName maps DNSSlave to the ISPConfig table dns_slave.
func (DNSSlave) TableName() string { return "dns_slave" }

// DNSTemplate is a zone wizard template (table dns_template). Fields lists
// the placeholders; Template holds the [ZONE]/[DNS_RECORDS] definition.
type DNSTemplate struct {
	TemplateID   uint32 `gorm:"column:template_id;primaryKey;autoIncrement"`
	SysUserID    uint32 `gorm:"column:sys_userid"`
	SysGroupID   uint32 `gorm:"column:sys_groupid"`
	SysPermUser  string `gorm:"column:sys_perm_user"`
	SysPermGroup string `gorm:"column:sys_perm_group"`
	SysPermOther string `gorm:"column:sys_perm_other"`
	Name         string `gorm:"column:name"`
	Fields       string `gorm:"column:fields"`
	Template     string `gorm:"column:template"`
	Visible      string `gorm:"column:visible"`
}

// TableName maps DNSTemplate to the ISPConfig table dns_template.
func (DNSTemplate) TableName() string { return "dns_template" }
