package model

import "time"

// WebDatabase maps the ISPConfig3 web_database table (one client MySQL
// database; the daemon mysql_clientdb plugin provisions the real DB).
type WebDatabase struct {
	DatabaseID            uint32     `gorm:"column:database_id;primaryKey;autoIncrement"`
	SysUserID             uint32     `gorm:"column:sys_userid"`
	SysGroupID            uint32     `gorm:"column:sys_groupid"`
	SysPermUser           string     `gorm:"column:sys_perm_user"`
	SysPermGroup          string     `gorm:"column:sys_perm_group"`
	SysPermOther          string     `gorm:"column:sys_perm_other"`
	ServerID              uint32     `gorm:"column:server_id"`
	ParentDomainID        uint32     `gorm:"column:parent_domain_id"`
	Type                  string     `gorm:"column:type;default:mysql"`
	DatabaseName          string     `gorm:"column:database_name"`
	DatabaseNamePrefix    string     `gorm:"column:database_name_prefix"`
	DatabaseQuota         *int32     `gorm:"column:database_quota"`
	QuotaExceeded         string     `gorm:"column:quota_exceeded;default:n"`
	LastQuotaNotification *time.Time `gorm:"column:last_quota_notification"`
	DatabaseUserID        *uint32    `gorm:"column:database_user_id"`
	DatabaseROUserID      *uint32    `gorm:"column:database_ro_user_id"`
	DatabaseCharset       string     `gorm:"column:database_charset"`
	RemoteAccess          string     `gorm:"column:remote_access;default:y"`
	RemoteIps             string     `gorm:"column:remote_ips"`
	BackupInterval        string     `gorm:"column:backup_interval;default:none"`
	BackupCopies          int32      `gorm:"column:backup_copies;default:1"`
	Active                string     `gorm:"column:active;default:y"`
}

// TableName implements the GORM naming override.
func (WebDatabase) TableName() string { return "web_database" }

// WebDatabaseUser maps web_database_user (a MySQL login shared by one or
// more web_database rows). Password columns hold hashes, never plaintext.
type WebDatabaseUser struct {
	DatabaseUserID           uint32 `gorm:"column:database_user_id;primaryKey;autoIncrement"`
	SysUserID                uint32 `gorm:"column:sys_userid"`
	SysGroupID               uint32 `gorm:"column:sys_groupid"`
	SysPermUser              string `gorm:"column:sys_perm_user"`
	SysPermGroup             string `gorm:"column:sys_perm_group"`
	SysPermOther             string `gorm:"column:sys_perm_other"`
	ServerID                 uint32 `gorm:"column:server_id"`
	DatabaseUser             string `gorm:"column:database_user"`
	DatabaseUserPrefix       string `gorm:"column:database_user_prefix"`
	DatabasePassword         string `gorm:"column:database_password"`
	DatabasePasswordSha2     string `gorm:"column:database_password_sha2"`
	DatabasePasswordMongo    string `gorm:"column:database_password_mongo"`
	DatabasePasswordPostgres string `gorm:"column:database_password_postgres"`
}

// TableName implements the GORM naming override.
func (WebDatabaseUser) TableName() string { return "web_database_user" }
