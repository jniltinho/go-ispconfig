package model

// Cron is a client scheduled job tied to a website (table cron). Type
// distinguishes url / chrooted / full executions; the schedule is stored
// as five cron fields (run_min/run_hour/run_mday/run_month/run_wday),
// each a comma- or step-separated token list (@reboot is legal only in
// run_month). The daemon's cron module reads/writes this row through
// sys_datalog; the embedded cron plugin executes the job — see
// internal/cron.
type Cron struct {
	ID             uint32 `gorm:"column:id;primaryKey;autoIncrement"`
	SysUserID      uint32 `gorm:"column:sys_userid"`
	SysGroupID     uint32 `gorm:"column:sys_groupid"`
	SysPermUser    string `gorm:"column:sys_perm_user"`
	SysPermGroup   string `gorm:"column:sys_perm_group"`
	SysPermOther   string `gorm:"column:sys_perm_other"`
	ServerID       uint32 `gorm:"column:server_id"`
	ParentDomainID uint32 `gorm:"column:parent_domain_id"`
	Type           string `gorm:"column:type;default:url"`
	Command        string `gorm:"column:command"`
	RunMin         string `gorm:"column:run_min"`
	RunHour        string `gorm:"column:run_hour"`
	RunMday        string `gorm:"column:run_mday"`
	RunMonth       string `gorm:"column:run_month"`
	RunWday        string `gorm:"column:run_wday"`
	Log            string `gorm:"column:log;default:n"`
	Active         string `gorm:"column:active;default:y"`
}

// TableName maps Cron to the ISPConfig table cron.
func (Cron) TableName() string { return "cron" }

// CronType values (column type enum).
const (
	CronTypeURL      = "url"
	CronTypeChrooted = "chrooted"
	CronTypeFull     = "full"
)
