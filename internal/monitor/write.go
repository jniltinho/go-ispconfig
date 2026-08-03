package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"

	"go-ispconfig/internal/model"
)

// PruneAgeSeconds is the delOldRecords window (4 minutes), matching PHP.
const PruneAgeSeconds = 240

// Store inserts a monitor_data row as JSON and prunes older samples for the
// same (server_id, type). created defaults to now when zero. data is marshaled
// with encoding/json (never PHP serialize).
func Store(ctx context.Context, db *gorm.DB, serverID uint32, typ string, data any, state string, created uint32) error {
	if typ == "" {
		return fmt.Errorf("monitor: empty type")
	}
	if created == 0 {
		created = uint32(time.Now().Unix())
	}
	state = NormalizeState(state)

	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("monitor: marshal %s: %w", typ, err)
	}

	row := model.MonitorData{
		ServerID: serverID,
		Type:     typ,
		Created:  created,
		Data:     string(payload),
		State:    state,
	}
	if err := db.WithContext(ctx).Create(&row).Error; err != nil {
		return fmt.Errorf("monitor: insert %s: %w", typ, err)
	}
	if err := DelOldRecords(ctx, db, typ, serverID); err != nil {
		return err
	}
	return nil
}

// DelOldRecords deletes monitor_data rows for type+server_id older than
// PruneAgeSeconds (port of monitor_tools::delOldRecords). Always scopes by
// server_id so multi-server clock skew cannot drop another server's newest
// sample.
func DelOldRecords(ctx context.Context, db *gorm.DB, typ string, serverID uint32) error {
	cutoff := uint32(time.Now().Unix()) - PruneAgeSeconds
	res := db.WithContext(ctx).
		Where("type = ? AND server_id = ? AND created < ?", typ, serverID, cutoff).
		Delete(&model.MonitorData{})
	if res.Error != nil {
		return fmt.Errorf("monitor: prune %s server %d: %w", typ, serverID, res.Error)
	}
	return nil
}

// UpsertType updates the single current row for sys_usage-style series (PHP
// updates in place when a row exists). Falls back to Store when none exist.
// Always prunes after write.
func UpsertType(ctx context.Context, db *gorm.DB, serverID uint32, typ string, data any, state string) error {
	state = NormalizeState(state)
	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("monitor: marshal %s: %w", typ, err)
	}
	now := uint32(time.Now().Unix())

	var existing model.MonitorData
	err = db.WithContext(ctx).
		Where("type = ? AND server_id = ?", typ, serverID).
		Order("created DESC").
		First(&existing).Error
	if err == gorm.ErrRecordNotFound {
		return Store(ctx, db, serverID, typ, data, state, now)
	}
	if err != nil {
		return fmt.Errorf("monitor: load %s: %w", typ, err)
	}

	res := db.WithContext(ctx).Model(&model.MonitorData{}).
		Where("server_id = ? AND type = ? AND created = ?", existing.ServerID, existing.Type, existing.Created).
		Updates(map[string]any{
			"data":    string(payload),
			"created": now,
			"state":   state,
		})
	if res.Error != nil {
		return fmt.Errorf("monitor: update %s: %w", typ, res.Error)
	}
	return DelOldRecords(ctx, db, typ, serverID)
}
