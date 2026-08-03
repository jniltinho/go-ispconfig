package model

// RemoteSession is an authenticated remote API session (table remote_session).
type RemoteSession struct {
	RemoteSession   string `gorm:"column:remote_session"`
	RemoteUserID    uint32 `gorm:"column:remote_userid"`
	RemoteFunctions string `gorm:"column:remote_functions"`
	ClientLogin     uint8  `gorm:"column:client_login"`
	Tstamp          uint32 `gorm:"column:tstamp"`
	RemoteIP        string `gorm:"column:remote_ip"`
}

// TableName maps RemoteSession to the ISPConfig table remote_session.
func (RemoteSession) TableName() string { return "remote_session" }

// RemoteUser is a remote API account and the functions it may call
// (table remote_user).
type RemoteUser struct {
	RemoteUserID    uint32 `gorm:"column:remote_userid;primaryKey;autoIncrement"`
	SysUserID       uint32 `gorm:"column:sys_userid"`
	SysGroupID      uint32 `gorm:"column:sys_groupid"`
	SysPermUser     string `gorm:"column:sys_perm_user"`
	SysPermGroup    string `gorm:"column:sys_perm_group"`
	SysPermOther    string `gorm:"column:sys_perm_other"`
	RemoteUsername  string `gorm:"column:remote_username"`
	RemotePassword  string `gorm:"column:remote_password"`
	RemoteAccess    string `gorm:"column:remote_access"`
	RemoteIPs       string `gorm:"column:remote_ips"`
	RemoteFunctions string `gorm:"column:remote_functions"`
}

// TableName maps RemoteUser to the ISPConfig table remote_user.
func (RemoteUser) TableName() string { return "remote_user" }
