package model

// HelpFaq is a question and answer of the support FAQ (table help_faq).
type HelpFaq struct {
	HfID         int32  `gorm:"column:hf_id;primaryKey;autoIncrement"`
	HfSection    *int32 `gorm:"column:hf_section"`
	HfOrder      *int32 `gorm:"column:hf_order"`
	HfQuestion   string `gorm:"column:hf_question"`
	HfAnswer     string `gorm:"column:hf_answer"`
	SysUserID    *int32 `gorm:"column:sys_userid"`
	SysGroupID   *int32 `gorm:"column:sys_groupid"`
	SysPermUser  string `gorm:"column:sys_perm_user"`
	SysPermGroup string `gorm:"column:sys_perm_group"`
	SysPermOther string `gorm:"column:sys_perm_other"`
}

// TableName maps HelpFaq to the ISPConfig table help_faq.
func (HelpFaq) TableName() string { return "help_faq" }

// HelpFaqSection groups FAQ entries (table help_faq_sections).
type HelpFaqSection struct {
	HfsID        int32  `gorm:"column:hfs_id;primaryKey;autoIncrement"`
	HfsName      string `gorm:"column:hfs_name"`
	HfsOrder     *int32 `gorm:"column:hfs_order"`
	SysUserID    *int32 `gorm:"column:sys_userid"`
	SysGroupID   *int32 `gorm:"column:sys_groupid"`
	SysPermUser  string `gorm:"column:sys_perm_user"`
	SysPermGroup string `gorm:"column:sys_perm_group"`
	SysPermOther string `gorm:"column:sys_perm_other"`
}

// TableName maps HelpFaqSection to the ISPConfig table help_faq_sections.
func (HelpFaqSection) TableName() string { return "help_faq_sections" }
