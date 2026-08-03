// Package model defines GORM models mapping the original ISPConfig3 schema.
// Column names, types and defaults come verbatim from the embedded
// ispconfig3.sql (design D9): every field carries an explicit gorm column tag
// and every model a TableName method, so no GORM naming convention can drift
// from the real schema. Enum('y','n') style columns map to plain strings.
package model

import "time"

// SysUser is a panel login account (table sys_user). The admin account is
// userid 1. Passwords live in the legacy-named passwort column and may be
// bcrypt or ISPConfig crypt hashes ($6$/$1$, design D10).
type SysUser struct {
	UserID             uint32     `gorm:"column:userid;primaryKey;autoIncrement"`
	SysUserID          uint32     `gorm:"column:sys_userid"`
	SysGroupID         uint32     `gorm:"column:sys_groupid"`
	SysPermUser        string     `gorm:"column:sys_perm_user"`
	SysPermGroup       string     `gorm:"column:sys_perm_group"`
	SysPermOther       string     `gorm:"column:sys_perm_other"`
	Username           string     `gorm:"column:username"`
	Passwort           string     `gorm:"column:passwort"`
	Modules            string     `gorm:"column:modules"`
	Startmodule        string     `gorm:"column:startmodule"`
	AppTheme           string     `gorm:"column:app_theme"`
	Typ                string     `gorm:"column:typ"`
	Active             int8       `gorm:"column:active"`
	Language           string     `gorm:"column:language"`
	Groups             string     `gorm:"column:groups"`
	DefaultGroup       uint32     `gorm:"column:default_group"`
	ClientID           uint32     `gorm:"column:client_id"`
	IDRsa              string     `gorm:"column:id_rsa"`
	SSHRsa             string     `gorm:"column:ssh_rsa"`
	LostPasswordFunc   int8       `gorm:"column:lost_password_function"`
	LostPasswordHash   string     `gorm:"column:lost_password_hash"`
	LostPasswordReqAt  *time.Time `gorm:"column:lost_password_reqtime"`
	OtpType            string     `gorm:"column:otp_type"`
	OtpData            string     `gorm:"column:otp_data"`
	OtpRecovery        string     `gorm:"column:otp_recovery"`
	OtpAttempts        int8       `gorm:"column:otp_attempts"`
	LastPasswordChange *time.Time `gorm:"column:last_password_change"`
}

// TableName maps SysUser to the ISPConfig table sys_user.
func (SysUser) TableName() string { return "sys_user" }

// SysGroup is a permission group (table sys_group). Group 1 is admin; each
// client gets its own group linked via client_id.
type SysGroup struct {
	GroupID     uint32 `gorm:"column:groupid;primaryKey;autoIncrement"`
	Name        string `gorm:"column:name"`
	Description string `gorm:"column:description"`
	ClientID    uint32 `gorm:"column:client_id"`
}

// TableName maps SysGroup to the ISPConfig table sys_group.
func (SysGroup) TableName() string { return "sys_group" }

// SysDatalog is the change journal consumed by the daemon (table sys_datalog).
// Action is i/u/d; Data holds the serialized {old,new} diff.
type SysDatalog struct {
	DatalogID uint32 `gorm:"column:datalog_id;primaryKey;autoIncrement"`
	ServerID  uint32 `gorm:"column:server_id"`
	DBTable   string `gorm:"column:dbtable"`
	DBIdx     string `gorm:"column:dbidx"`
	Action    string `gorm:"column:action"`
	Tstamp    int32  `gorm:"column:tstamp"`
	User      string `gorm:"column:user"`
	Data      string `gorm:"column:data"`
	Status    string `gorm:"column:status"`
	Error     string `gorm:"column:error"`
	SessionID string `gorm:"column:session_id"`
}

// TableName maps SysDatalog to the ISPConfig table sys_datalog.
func (SysDatalog) TableName() string { return "sys_datalog" }

// SysRemoteAction is a queued server-side action such as an OS update or
// ISPConfig update trigger (table sys_remoteaction).
type SysRemoteAction struct {
	ActionID    uint32 `gorm:"column:action_id;primaryKey;autoIncrement"`
	ServerID    uint32 `gorm:"column:server_id"`
	Tstamp      int32  `gorm:"column:tstamp"`
	ActionType  string `gorm:"column:action_type"`
	ActionParam string `gorm:"column:action_param"`
	ActionState string `gorm:"column:action_state"`
	Response    string `gorm:"column:response"`
}

// TableName maps SysRemoteAction to the ISPConfig table sys_remoteaction.
func (SysRemoteAction) TableName() string { return "sys_remoteaction" }

// SysConfig is a key/value configuration entry namespaced by group
// (table sys_config, composite primary key group+name).
type SysConfig struct {
	Group string `gorm:"column:group;primaryKey"`
	Name  string `gorm:"column:name;primaryKey"`
	Value string `gorm:"column:value"`
}

// TableName maps SysConfig to the ISPConfig table sys_config.
func (SysConfig) TableName() string { return "sys_config" }

// SysIni holds the global panel configuration as serialized INI text
// (table sys_ini, single row with sysini_id 1).
type SysIni struct {
	SysIniID    uint32 `gorm:"column:sysini_id;primaryKey;autoIncrement"`
	Config      string `gorm:"column:config"`
	DefaultLogo string `gorm:"column:default_logo"`
	CustomLogo  string `gorm:"column:custom_logo"`
}

// TableName maps SysIni to the ISPConfig table sys_ini.
func (SysIni) TableName() string { return "sys_ini" }

// SysLog is a system log line written by the daemon (table sys_log).
type SysLog struct {
	SyslogID  uint32 `gorm:"column:syslog_id;primaryKey;autoIncrement" json:"syslog_id"`
	ServerID  uint32 `gorm:"column:server_id" json:"server_id"`
	DatalogID uint32 `gorm:"column:datalog_id" json:"datalog_id"`
	Loglevel  int8   `gorm:"column:loglevel" json:"loglevel"`
	Tstamp    uint32 `gorm:"column:tstamp" json:"tstamp"`
	Message   string `gorm:"column:message" json:"message"`
}

// TableName maps SysLog to the ISPConfig table sys_log.
func (SysLog) TableName() string { return "sys_log" }

// AttemptsLogin is a failed-login counter row used for brute-force lockout
// (table attempts_login, no primary key). IP holds the md5 hex of the remote
// address, exactly as PHP ISPConfig stores it.
type AttemptsLogin struct {
	IP        string    `gorm:"column:ip"`
	Times     *int32    `gorm:"column:times"`
	LoginTime time.Time `gorm:"column:login_time"`
}

// TableName maps AttemptsLogin to the ISPConfig table attempts_login.
func (AttemptsLogin) TableName() string { return "attempts_login" }

// SysSession is a DB-backed panel session (table sys_session).
type SysSession struct {
	SessionID   string     `gorm:"column:session_id;primaryKey"`
	DateCreated *time.Time `gorm:"column:date_created"`
	LastUpdated *time.Time `gorm:"column:last_updated"`
	Permanent   string     `gorm:"column:permanent"`
	SessionData string     `gorm:"column:session_data"`
}

// TableName maps SysSession to the ISPConfig table sys_session.
func (SysSession) TableName() string { return "sys_session" }

// SysCron is the run bookkeeping of one panel cron job (table sys_cron).
type SysCron struct {
	Name    string     `gorm:"column:name;primaryKey"`
	LastRun *time.Time `gorm:"column:last_run"`
	NextRun *time.Time `gorm:"column:next_run"`
	Running uint8      `gorm:"column:running"`
}

// TableName maps SysCron to the ISPConfig table sys_cron.
func (SysCron) TableName() string { return "sys_cron" }

// SysDBSync is a database mirroring job between two ISPConfig databases
// (table sys_dbsync).
type SysDBSync struct {
	ID                  uint32 `gorm:"column:id;primaryKey;autoIncrement"`
	Jobname             string `gorm:"column:jobname"`
	SyncIntervalMinutes uint32 `gorm:"column:sync_interval_minutes"`
	DBType              string `gorm:"column:db_type"`
	DBHost              string `gorm:"column:db_host"`
	DBName              string `gorm:"column:db_name"`
	DBUsername          string `gorm:"column:db_username"`
	DBPassword          string `gorm:"column:db_password"`
	DBTables            string `gorm:"column:db_tables"`
	EmptyDatalog        uint32 `gorm:"column:empty_datalog"`
	SyncDatalogExternal uint32 `gorm:"column:sync_datalog_external"`
	Active              int8   `gorm:"column:active"`
	LastDatalogID       uint32 `gorm:"column:last_datalog_id"`
}

// TableName maps SysDBSync to the ISPConfig table sys_dbsync.
func (SysDBSync) TableName() string { return "sys_dbsync" }

// SysFileSync is a wput based file mirroring job (table sys_filesync).
type SysFileSync struct {
	ID                  uint32 `gorm:"column:id;primaryKey;autoIncrement"`
	Jobname             string `gorm:"column:jobname"`
	SyncIntervalMinutes uint32 `gorm:"column:sync_interval_minutes"`
	FTPHost             string `gorm:"column:ftp_host"`
	FTPPath             string `gorm:"column:ftp_path"`
	FTPUsername         string `gorm:"column:ftp_username"`
	FTPPassword         string `gorm:"column:ftp_password"`
	LocalPath           string `gorm:"column:local_path"`
	WputOptions         string `gorm:"column:wput_options"`
	Active              int8   `gorm:"column:active"`
}

// TableName maps SysFileSync to the ISPConfig table sys_filesync.
func (SysFileSync) TableName() string { return "sys_filesync" }

// SysMessage is a panel notification shown to a user (table sys_message).
type SysMessage struct {
	MessageID    uint32     `gorm:"column:message_id;primaryKey;autoIncrement"`
	SysUserID    uint32     `gorm:"column:sys_userid"`
	SysGroupID   uint32     `gorm:"column:sys_groupid"`
	SysPermUser  string     `gorm:"column:sys_perm_user"`
	SysPermGroup string     `gorm:"column:sys_perm_group"`
	SysPermOther string     `gorm:"column:sys_perm_other"`
	MessageState string     `gorm:"column:message_state"`
	MessageDate  *time.Time `gorm:"column:message_date"`
	MessageAck   string     `gorm:"column:message_ack"`
	Relation     string     `gorm:"column:relation"`
	Message      string     `gorm:"column:message"`
}

// TableName maps SysMessage to the ISPConfig table sys_message.
func (SysMessage) TableName() string { return "sys_message" }

// SysTheme overrides the template and logo of a user or group
// (table sys_theme).
type SysTheme struct {
	SysUserID    uint32 `gorm:"column:sys_userid"`
	SysGroupID   uint32 `gorm:"column:sys_groupid"`
	SysPermUser  string `gorm:"column:sys_perm_user"`
	SysPermGroup string `gorm:"column:sys_perm_group"`
	SysPermOther string `gorm:"column:sys_perm_other"`
	VarID        uint32 `gorm:"column:var_id;primaryKey;autoIncrement"`
	TplName      string `gorm:"column:tpl_name"`
	Username     string `gorm:"column:username"`
	LogoURL      string `gorm:"column:logo_url"`
}

// TableName maps SysTheme to the ISPConfig table sys_theme.
func (SysTheme) TableName() string { return "sys_theme" }
