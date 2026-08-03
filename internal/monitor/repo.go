package monitor

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"go-ispconfig/internal/auth"
	"go-ispconfig/internal/model"
	"go-ispconfig/internal/repository"
)

// HasMonitorModule reports whether the session may access the monitor module
// (sys_user.modules CSV includes "monitor", or admin).
func HasMonitorModule(sess *auth.SessionData) bool {
	if sess == nil {
		return false
	}
	if sess.Typ == "admin" {
		return true
	}
	for _, m := range strings.Split(sess.Modules, ",") {
		if strings.TrimSpace(m) == "monitor" {
			return true
		}
	}
	return false
}

// ReadableServers returns server rows the identity may read under riud scope
// on table server. Admin sees all active servers.
func ReadableServers(ctx context.Context, db *gorm.DB, id *repository.Identity) ([]model.Server, error) {
	var list []model.Server
	q := db.WithContext(ctx).Model(&model.Server{}).Where("active = ?", 1)
	if id == nil {
		return nil, nil
	}
	if !id.IsAdmin() {
		q = q.Scopes(repository.WithPerm(id, repository.PermRead))
	}
	if err := q.Order("server_id").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// ServerIDs extracts ids from a server list.
func ServerIDs(servers []model.Server) []uint32 {
	ids := make([]uint32, len(servers))
	for i, s := range servers {
		ids[i] = s.ServerID
	}
	return ids
}

// DataFilter filters monitor_data list queries.
type DataFilter struct {
	ServerIDs []uint32
	Type      string
	State     string
	// LatestOnly returns only the newest row per (server_id, type).
	LatestOnly bool
	Limit      int
	Offset     int
}

// ListData returns monitor_data rows matching filter, ordered by created DESC.
func ListData(ctx context.Context, db *gorm.DB, f DataFilter) ([]model.MonitorData, error) {
	q := db.WithContext(ctx).Model(&model.MonitorData{})
	if len(f.ServerIDs) > 0 {
		q = q.Where("server_id IN ?", f.ServerIDs)
	}
	if f.Type != "" {
		q = q.Where("type = ?", f.Type)
	}
	if f.State != "" {
		q = q.Where("state = ?", f.State)
	}
	q = q.Order("created DESC")
	if f.Limit > 0 {
		q = q.Limit(f.Limit)
	}
	if f.Offset > 0 {
		q = q.Offset(f.Offset)
	}
	var rows []model.MonitorData
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	if f.LatestOnly {
		seen := map[string]bool{}
		out := make([]model.MonitorData, 0, len(rows))
		for _, r := range rows {
			key := fmt.Sprintf("%d\x00%s", r.ServerID, r.Type)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, r)
		}
		return out, nil
	}
	return rows, nil
}

// LatestByType returns the newest monitor_data row for server+type, or
// gorm.ErrRecordNotFound.
func LatestByType(ctx context.Context, db *gorm.DB, serverID uint32, typ string) (model.MonitorData, error) {
	var row model.MonitorData
	err := db.WithContext(ctx).
		Where("server_id = ? AND type = ?", serverID, typ).
		Order("created DESC").
		First(&row).Error
	return row, err
}

// SysLogFilter filters sys_log list queries.
type SysLogFilter struct {
	ServerIDs []uint32
	ServerID  *uint32
	Loglevel  *int8
	Message   string
	TstampMin *uint32
	TstampMax *uint32
	Limit     int
	Offset    int
}

// ListSysLog returns sys_log rows ordered by tstamp DESC.
func ListSysLog(ctx context.Context, db *gorm.DB, f SysLogFilter) ([]model.SysLog, int64, error) {
	q := db.WithContext(ctx).Model(&model.SysLog{})
	if f.ServerID != nil {
		q = q.Where("server_id = ?", *f.ServerID)
	} else if len(f.ServerIDs) > 0 {
		q = q.Where("server_id IN ?", f.ServerIDs)
	}
	if f.Loglevel != nil {
		q = q.Where("loglevel = ?", *f.Loglevel)
	}
	if f.Message != "" {
		q = q.Where("message LIKE ?", "%"+f.Message+"%")
	}
	if f.TstampMin != nil {
		q = q.Where("tstamp >= ?", *f.TstampMin)
	}
	if f.TstampMax != nil {
		q = q.Where("tstamp <= ?", *f.TstampMax)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if f.Limit <= 0 {
		f.Limit = 50
	}
	var rows []model.SysLog
	err := q.Order("tstamp DESC").Limit(f.Limit).Offset(f.Offset).Find(&rows).Error
	return rows, total, err
}

// ClearSysLog sets loglevel=0 for one syslog_id (admin clear; does not DELETE).
func ClearSysLog(ctx context.Context, db *gorm.DB, syslogID uint32) (int64, error) {
	res := db.WithContext(ctx).Model(&model.SysLog{}).
		Where("syslog_id = ?", syslogID).
		Update("loglevel", 0)
	return res.RowsAffected, res.Error
}

// ClearSysLogByLevel sets loglevel=0 for all rows matching loglevel (batch clear).
func ClearSysLogByLevel(ctx context.Context, db *gorm.DB, loglevel int8, serverIDs []uint32) (int64, error) {
	q := db.WithContext(ctx).Model(&model.SysLog{}).Where("loglevel = ?", loglevel)
	if len(serverIDs) > 0 {
		q = q.Where("server_id IN ?", serverIDs)
	}
	res := q.Update("loglevel", 0)
	return res.RowsAffected, res.Error
}

// JobqueueFilter filters pending datalog rows.
type JobqueueFilter struct {
	ServerID *uint32
	// Servers is the list of local servers to compute pending against.
	Servers []model.Server
	DBTable string
	Action  string
	Limit   int
	Offset  int
}

// ListJobqueue returns pending sys_datalog rows (datalog_id > server.updated
// for matching server_id IN (0, server_id)).
func ListJobqueue(ctx context.Context, db *gorm.DB, f JobqueueFilter) ([]model.SysDatalog, int64, error) {
	if len(f.Servers) == 0 {
		return []model.SysDatalog{}, 0, nil
	}
	// Build OR of (server_id IN (0, sid) AND datalog_id > updated) per server.
	q := db.WithContext(ctx).Model(&model.SysDatalog{})
	or := db.Session(&gorm.Session{NewDB: true})
	first := true
	for _, s := range f.Servers {
		if f.ServerID != nil && s.ServerID != *f.ServerID {
			continue
		}
		cond := db.Session(&gorm.Session{NewDB: true}).
			Where("datalog_id > ? AND server_id IN ?", s.Updated, []uint32{0, s.ServerID})
		if first {
			or = cond
			first = false
		} else {
			or = or.Or(cond)
		}
	}
	if first {
		return []model.SysDatalog{}, 0, nil
	}
	q = q.Where(or)
	if f.DBTable != "" {
		q = q.Where("dbtable = ?", f.DBTable)
	}
	if f.Action != "" {
		q = q.Where("action = ?", f.Action)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if f.Limit <= 0 {
		f.Limit = 50
	}
	var rows []model.SysDatalog
	err := q.Order("datalog_id ASC").Limit(f.Limit).Offset(f.Offset).Find(&rows).Error
	return rows, total, err
}

// CountJobqueue returns the pending datalog count for accessible servers.
// When serverID is 0 or nil, counts across all provided servers (incl. server_id=0 rows).
func CountJobqueue(ctx context.Context, db *gorm.DB, servers []model.Server, serverID *uint32) (int64, error) {
	_, total, err := ListJobqueue(ctx, db, JobqueueFilter{
		Servers:  servers,
		ServerID: serverID,
		Limit:    1,
	})
	return total, err
}

// DatalogHistoryFilter filters full sys_datalog history.
type DatalogHistoryFilter struct {
	ServerID  *uint32
	ServerIDs []uint32
	Action    string
	DBTable   string
	User      string
	TstampMin *int32
	TstampMax *int32
	Limit     int
	Offset    int
}

// ListDatalogHistory returns historical sys_datalog rows (not limited to pending).
func ListDatalogHistory(ctx context.Context, db *gorm.DB, f DatalogHistoryFilter) ([]model.SysDatalog, int64, error) {
	q := db.WithContext(ctx).Model(&model.SysDatalog{})
	if f.ServerID != nil {
		q = q.Where("server_id = ?", *f.ServerID)
	} else if len(f.ServerIDs) > 0 {
		q = q.Where("server_id IN ?", f.ServerIDs)
	}
	if f.Action != "" {
		q = q.Where("action = ?", f.Action)
	}
	if f.DBTable != "" {
		q = q.Where("dbtable = ?", f.DBTable)
	}
	if f.User != "" {
		q = q.Where("user = ?", f.User)
	}
	if f.TstampMin != nil {
		q = q.Where("tstamp >= ?", *f.TstampMin)
	}
	if f.TstampMax != nil {
		q = q.Where("tstamp <= ?", *f.TstampMax)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if f.Limit <= 0 {
		f.Limit = 50
	}
	var rows []model.SysDatalog
	err := q.Order("datalog_id DESC").Limit(f.Limit).Offset(f.Offset).Find(&rows).Error
	return rows, total, err
}

// GetDatalog returns one sys_datalog row by id.
func GetDatalog(ctx context.Context, db *gorm.DB, id uint32) (model.SysDatalog, error) {
	var row model.SysDatalog
	err := db.WithContext(ctx).Where("datalog_id = ?", id).First(&row).Error
	return row, err
}
