package model

// SpamfilterPolicy maps spamfilter_policy. Only the rspamd_* fields
// drive the Go daemon (amavis columns are kept for schema parity and
// legacy data, never interpreted).
type SpamfilterPolicy struct {
	ID           uint32 `gorm:"column:id;primaryKey;autoIncrement"`
	SysUserID    uint32 `gorm:"column:sys_userid"`
	SysGroupID   uint32 `gorm:"column:sys_groupid"`
	SysPermUser  string `gorm:"column:sys_perm_user"`
	SysPermGroup string `gorm:"column:sys_perm_group"`
	SysPermOther string `gorm:"column:sys_perm_other"`
	PolicyName   string `gorm:"column:policy_name"`

	VirusLover         string `gorm:"column:virus_lover;default:N"`
	SpamLover          string `gorm:"column:spam_lover;default:N"`
	BannedFilesLover   string `gorm:"column:banned_files_lover;default:N"`
	BadHeaderLover     string `gorm:"column:bad_header_lover;default:N"`
	BypassVirusChecks  string `gorm:"column:bypass_virus_checks;default:N"`
	BypassSpamChecks   string `gorm:"column:bypass_spam_checks;default:N"`
	BypassBannedChecks string `gorm:"column:bypass_banned_checks;default:N"`
	BypassHeaderChecks string `gorm:"column:bypass_header_checks;default:N"`
	SpamModifiesSubj   string `gorm:"column:spam_modifies_subj;default:N"`

	VirusQuarantineTo     string `gorm:"column:virus_quarantine_to"`
	SpamQuarantineTo      string `gorm:"column:spam_quarantine_to"`
	BannedQuarantineTo    string `gorm:"column:banned_quarantine_to"`
	BadHeaderQuarantineTo string `gorm:"column:bad_header_quarantine_to"`
	CleanQuarantineTo     string `gorm:"column:clean_quarantine_to"`
	OtherQuarantineTo     string `gorm:"column:other_quarantine_to"`

	SpamTagLevel              *float64 `gorm:"column:spam_tag_level"`
	SpamTag2Level             *float64 `gorm:"column:spam_tag2_level"`
	SpamKillLevel             *float64 `gorm:"column:spam_kill_level"`
	SpamDsnCutoffLevel        *float64 `gorm:"column:spam_dsn_cutoff_level"`
	SpamQuarantineCutoffLevel *float64 `gorm:"column:spam_quarantine_cutoff_level"`

	AddrExtensionVirus     string `gorm:"column:addr_extension_virus"`
	AddrExtensionSpam      string `gorm:"column:addr_extension_spam"`
	AddrExtensionBanned    string `gorm:"column:addr_extension_banned"`
	AddrExtensionBadHeader string `gorm:"column:addr_extension_bad_header"`

	WarnVirusRecip  string `gorm:"column:warnvirusrecip;default:N"`
	WarnBannedRecip string `gorm:"column:warnbannedrecip;default:N"`
	WarnBadhRecip   string `gorm:"column:warnbadhrecip;default:N"`

	NewvirusAdmin    string  `gorm:"column:newvirus_admin"`
	VirusAdmin       string  `gorm:"column:virus_admin"`
	BannedAdmin      string  `gorm:"column:banned_admin"`
	BadHeaderAdmin   string  `gorm:"column:bad_header_admin"`
	SpamAdmin        string  `gorm:"column:spam_admin"`
	SpamSubjectTag   string  `gorm:"column:spam_subject_tag"`
	SpamSubjectTag2  string  `gorm:"column:spam_subject_tag2"`
	MessageSizeLimit *uint32 `gorm:"column:message_size_limit"`
	BannedRulenames  string  `gorm:"column:banned_rulenames"`

	PolicydQuotaIn        int32  `gorm:"column:policyd_quota_in;default:-1"`
	PolicydQuotaInPeriod  int32  `gorm:"column:policyd_quota_in_period;default:24"`
	PolicydQuotaOut       int32  `gorm:"column:policyd_quota_out;default:-1"`
	PolicydQuotaOutPeriod int32  `gorm:"column:policyd_quota_out_period;default:24"`
	PolicydGreylist       string `gorm:"column:policyd_greylist;default:N"`

	RspamdGreylisting          string   `gorm:"column:rspamd_greylisting;default:n"`
	RspamdSpamGreylistingLevel *float64 `gorm:"column:rspamd_spam_greylisting_level"`
	RspamdSpamTagLevel         *float64 `gorm:"column:rspamd_spam_tag_level"`
	RspamdSpamTagMethod        string   `gorm:"column:rspamd_spam_tag_method;default:rewrite_subject"`
	RspamdSpamKillLevel        *float64 `gorm:"column:rspamd_spam_kill_level"`
}

// TableName implements the GORM naming override.
func (SpamfilterPolicy) TableName() string { return "spamfilter_policy" }

// SpamfilterUser maps spamfilter_users (per-identity policy assignment;
// email may be a full address or an @domain form).
type SpamfilterUser struct {
	ID           uint32 `gorm:"column:id;primaryKey;autoIncrement"`
	SysUserID    uint32 `gorm:"column:sys_userid"`
	SysGroupID   uint32 `gorm:"column:sys_groupid"`
	SysPermUser  string `gorm:"column:sys_perm_user"`
	SysPermGroup string `gorm:"column:sys_perm_group"`
	SysPermOther string `gorm:"column:sys_perm_other"`
	ServerID     uint32 `gorm:"column:server_id"`
	Priority     uint8  `gorm:"column:priority;default:7"`
	PolicyID     uint32 `gorm:"column:policy_id"`
	Email        string `gorm:"column:email"`
	Fullname     string `gorm:"column:fullname"`
	Local        string `gorm:"column:local"`
}

// TableName implements the GORM naming override.
func (SpamfilterUser) TableName() string { return "spamfilter_users" }

// SpamfilterWblist maps spamfilter_wblist (white/blacklist entries tied
// to a spamfilter_users row via rid).
type SpamfilterWblist struct {
	WblistID     uint32 `gorm:"column:wblist_id;primaryKey;autoIncrement"`
	SysUserID    uint32 `gorm:"column:sys_userid"`
	SysGroupID   uint32 `gorm:"column:sys_groupid"`
	SysPermUser  string `gorm:"column:sys_perm_user"`
	SysPermGroup string `gorm:"column:sys_perm_group"`
	SysPermOther string `gorm:"column:sys_perm_other"`
	ServerID     uint32 `gorm:"column:server_id"`
	WB           string `gorm:"column:wb;default:W"`
	RID          uint32 `gorm:"column:rid"`
	Email        string `gorm:"column:email"`
	Priority     uint8  `gorm:"column:priority"`
	Active       string `gorm:"column:active;default:y"`
}

// TableName implements the GORM naming override.
func (SpamfilterWblist) TableName() string { return "spamfilter_wblist" }
