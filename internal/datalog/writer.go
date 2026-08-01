// Package datalog implements the sys_datalog change journal writer (design
// D2): every create/update/delete on a tracked table inserts, inside the same
// transaction, a sys_datalog row with a JSON {"old":{},"new":{}} diff that
// the daemon later consumes. It is the Go port of the PHP interface's
// db::datalogSave/diffrec.
package datalog

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"

	"go-ispconfig/internal/model"
)

// Tracked marks a model whose mutations are journaled into sys_datalog —
// the Go equivalent of the tform per-form db_history flag. Models opt in by
// implementing DBHistory() with a true return.
type Tracked interface {
	// DBHistory reports whether mutations of this model are datalogged.
	DBHistory() bool
}

// IsTracked reports whether rec opts into datalog journaling via Tracked.
func IsTracked(rec any) bool {
	t, ok := rec.(Tracked)
	return ok && t.DBHistory()
}

// Diff holds the {"old","new"} payload stored in sys_datalog.data. For
// inserts Old is empty; for deletes New is empty; for updates both contain
// only the changed fields (PHP db::diffrec semantics).
type Diff struct {
	Old map[string]any `json:"old"`
	New map[string]any `json:"new"`
}

// schemaCache caches gorm schema parses across writer calls.
var schemaCache = &sync.Map{}

// notifyMu guards notifyReady.
var notifyMu sync.RWMutex

// notifyReady is the registered instant-wake hook (design D12), nil when the
// queue is not wired.
var notifyReady func(serverID uint32)

// SetReadyNotifier registers fn as the datalog:ready hook: it receives the
// server_id of every journaled change so the owning daemon can be woken
// without waiting for its poll tick. fn must never fail the business
// transaction — an enqueue problem is a warning, sys_datalog stays the
// source of truth. A nil fn disables notification.
func SetReadyNotifier(fn func(serverID uint32)) {
	notifyMu.Lock()
	defer notifyMu.Unlock()
	notifyReady = fn
}

// collector accumulates the server ids journaled inside one transaction so
// the notifier fires only after commit (and never on rollback).
type collector struct {
	mu  sync.Mutex
	ids map[uint32]struct{}
}

// collectorKey is the context key carrying a *collector.
type collectorKey struct{}

// NotifyAfterCommit returns a derived context that defers ready
// notifications for datalog writes made under it, plus a flush function the
// caller runs after the transaction committed. Without this context the
// writer notifies at write time, which is only correct outside a
// transaction; the repository layer wraps every datalog transaction with it.
func NotifyAfterCommit(ctx context.Context) (context.Context, func()) {
	c := &collector{ids: map[uint32]struct{}{}}
	return context.WithValue(ctx, collectorKey{}, c), func() {
		c.mu.Lock()
		ids := c.ids
		c.ids = map[uint32]struct{}{}
		c.mu.Unlock()
		for id := range ids {
			fireReady(id)
		}
	}
}

// markReady records one journaled change for post-commit notification, or
// notifies immediately when ctx carries no collector.
func markReady(ctx context.Context, serverID uint32) {
	if c, ok := ctx.Value(collectorKey{}).(*collector); ok {
		c.mu.Lock()
		c.ids[serverID] = struct{}{}
		c.mu.Unlock()
		return
	}
	fireReady(serverID)
}

// fireReady invokes the registered notifier, if any.
func fireReady(serverID uint32) {
	notifyMu.RLock()
	fn := notifyReady
	notifyMu.RUnlock()
	if fn != nil {
		fn(serverID)
	}
}

// LogInsert journals a created record with action i and the full new record
// as data. It is a no-op when rec is not Tracked. Call it inside the same
// transaction that created the record.
func LogInsert(tx *gorm.DB, rec any, username string) error {
	return logChange(tx, "i", nil, rec, username)
}

// LogUpdate journals a modified record with action u and a diff containing
// only the changed fields. No row is written when nothing changed or when
// the record is not Tracked. Call it inside the updating transaction.
func LogUpdate(tx *gorm.DB, oldRec, newRec any, username string) error {
	return logChange(tx, "u", oldRec, newRec, username)
}

// LogDelete journals a removed record with action d and the full old record
// as data. It is a no-op when rec is not Tracked. Call it inside the same
// transaction that deleted the record.
func LogDelete(tx *gorm.DB, rec any, username string) error {
	return logChange(tx, "d", rec, nil, username)
}

// logChange builds and inserts the sys_datalog row for one mutation.
func logChange(tx *gorm.DB, action string, oldRec, newRec any, username string) error {
	ref := newRec
	if ref == nil {
		ref = oldRec
	}
	if !IsTracked(ref) {
		return nil
	}

	s, err := parseSchema(tx, ref)
	if err != nil {
		return err
	}
	oldMap := recordMap(s, oldRec)
	newMap := recordMap(s, newRec)

	diff, changed := buildDiff(action, oldMap, newMap)
	if !changed {
		return nil
	}

	pk := s.PrioritizedPrimaryField
	if pk == nil {
		return fmt.Errorf("datalog: %s has no primary key", s.Table)
	}
	pkVal, ok := newMap[pk.DBName]
	if !ok {
		pkVal = oldMap[pk.DBName]
	}

	data, err := json.Marshal(diff)
	if err != nil {
		return fmt.Errorf("datalog: encoding diff for %s: %w", s.Table, err)
	}
	if username == "" {
		username = "admin"
	}
	row := model.SysDatalog{
		ServerID: serverID(oldMap, newMap),
		DBTable:  s.Table,
		DBIdx:    fmt.Sprintf("%s:%v", pk.DBName, pkVal),
		Action:   action,
		Tstamp:   int32(time.Now().Unix()),
		User:     username,
		Data:     string(data),
		Status:   "ok",
	}
	if err := tx.Create(&row).Error; err != nil {
		return fmt.Errorf("datalog: inserting sys_datalog row for %s: %w", s.Table, err)
	}
	markReady(tx.Statement.Context, row.ServerID)
	return nil
}

// buildDiff assembles the Diff payload for an action. The second return is
// false when an update has no changed fields (no datalog row is written,
// PHP diff_num semantics).
func buildDiff(action string, oldMap, newMap map[string]any) (Diff, bool) {
	switch action {
	case "i":
		return Diff{Old: map[string]any{}, New: newMap}, true
	case "d":
		return Diff{Old: oldMap, New: map[string]any{}}, true
	default: // "u"
		diff := Diff{Old: map[string]any{}, New: map[string]any{}}
		for col, newVal := range newMap {
			if oldVal, ok := oldMap[col]; !ok || !reflect.DeepEqual(oldVal, newVal) {
				diff.Old[col] = oldMap[col]
				diff.New[col] = newVal
			}
		}
		return diff, len(diff.New) > 0
	}
}

// serverID ports the PHP rule: the record's own server_id column selects the
// target daemon, new value winning over old; absent column means 0
// (broadcast to every server).
func serverID(oldMap, newMap map[string]any) uint32 {
	id := toUint32(oldMap["server_id"])
	if v, ok := newMap["server_id"]; ok {
		id = toUint32(v)
	}
	return id
}

// parseSchema resolves the gorm schema of rec using the shared cache.
func parseSchema(tx *gorm.DB, rec any) (*schema.Schema, error) {
	s, err := schema.Parse(rec, schemaCache, tx.NamingStrategy)
	if err != nil {
		return nil, fmt.Errorf("datalog: parsing schema for %T: %w", rec, err)
	}
	return s, nil
}

// recordMap converts a model struct into a column-name → value map covering
// every persisted field. A nil rec yields an empty map.
func recordMap(s *schema.Schema, rec any) map[string]any {
	m := map[string]any{}
	if rec == nil {
		return m
	}
	ctx := context.Background()
	rv := reflect.ValueOf(rec)
	for _, f := range s.Fields {
		if f.DBName == "" {
			continue
		}
		v, _ := f.ValueOf(ctx, rv)
		m[f.DBName] = v
	}
	return m
}

// toUint32 normalizes the numeric types a model server_id field may use.
func toUint32(v any) uint32 {
	switch n := v.(type) {
	case uint32:
		return n
	case int32:
		return uint32(n)
	case uint64:
		return uint32(n)
	case int64:
		return uint32(n)
	case int:
		return uint32(n)
	case uint:
		return uint32(n)
	default:
		return 0
	}
}
