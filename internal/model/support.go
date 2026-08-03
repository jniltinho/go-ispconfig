package model

// SupportMessage is a message exchanged between a client and the
// administrator (table support_message).
type SupportMessage struct {
	SupportMessageID uint32 `gorm:"column:support_message_id;primaryKey;autoIncrement"`
	SysUserID        uint32 `gorm:"column:sys_userid"`
	SysGroupID       uint32 `gorm:"column:sys_groupid"`
	SysPermUser      string `gorm:"column:sys_perm_user"`
	SysPermGroup     string `gorm:"column:sys_perm_group"`
	SysPermOther     string `gorm:"column:sys_perm_other"`
	RecipientID      uint32 `gorm:"column:recipient_id"`
	SenderID         uint32 `gorm:"column:sender_id"`
	Subject          string `gorm:"column:subject"`
	Message          string `gorm:"column:message"`
	Tstamp           int32  `gorm:"column:tstamp"`
}

// TableName maps SupportMessage to the ISPConfig table support_message.
func (SupportMessage) TableName() string { return "support_message" }
