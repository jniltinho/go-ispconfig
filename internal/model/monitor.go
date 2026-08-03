package model

// MonitorData is one collected metric/status sample for a server
// (table monitor_data). Composite primary key is (server_id, type, created);
// data holds JSON (new writes) or PHP serialize (pre-cutover history).
// State is one of: no_state, unknown, ok, info, warning, critical, error.
type MonitorData struct {
	ServerID uint32 `gorm:"column:server_id;primaryKey"`
	Type     string `gorm:"column:type;primaryKey"`
	Created  uint32 `gorm:"column:created;primaryKey"`
	Data     string `gorm:"column:data"`
	State    string `gorm:"column:state;default:unknown"`
}

// TableName maps MonitorData to the ISPConfig table monitor_data.
func (MonitorData) TableName() string { return "monitor_data" }

// Monitor state constants matching the enum on monitor_data.state.
const (
	MonitorStateNoState  = "no_state"
	MonitorStateUnknown  = "unknown"
	MonitorStateOK       = "ok"
	MonitorStateInfo     = "info"
	MonitorStateWarning  = "warning"
	MonitorStateCritical = "critical"
	MonitorStateError    = "error"
)
