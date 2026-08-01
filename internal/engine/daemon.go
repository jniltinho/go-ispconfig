package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"gorm.io/gorm"

	"go-ispconfig/internal/model"
)

// sys_log loglevel values (ISPConfig LOGLEVEL_* constants).
const (
	logLevelWarn  = 1
	logLevelError = 2
)

// datalogBatchSize is the per-cycle row limit (LIMIT 1000 in
// modules.inc.php processDatalog).
const datalogBatchSize = 1000

// maxDatalogPayload is the sys_datalog.data size accepted for JSON decoding.
// A larger payload is quarantined without being parsed, so a giant row can
// never become a memory/CPU DoS vector inside the root daemon.
const maxDatalogPayload = 4 << 20 // 4 MiB

// Daemon is the persistent sys_datalog consumer (port of server.php): every
// tick it processes pending datalog rows in order, dispatches table hooks,
// advances server.updated per row, runs pending sys_remoteaction rows and
// flushes delayed service restarts.
//
// Idempotency contract: server.updated advances only after a row's hooks
// ran, so a crash between hook execution and the advance reprocesses that
// row on the next cycle. Module/plugin handlers MUST therefore be
// idempotent (by design — applying the same config change twice must be
// harmless).
type Daemon struct {
	db       *gorm.DB
	reg      *Registry
	services *Services
	log      *slog.Logger
	serverID uint32
	busy     sync.Mutex
}

// NewDaemon validates the server table (startup guard) and builds a Daemon
// bound to the single active server row. Scheduled jobs run as asynq
// periodic tasks through the queue worker (design D12), not inside the
// daemon cycle.
func NewDaemon(db *gorm.DB, reg *Registry, services *Services, log *slog.Logger) (*Daemon, error) {
	if log == nil {
		log = slog.Default()
	}
	srv, err := GuardServer(db)
	if err != nil {
		return nil, err
	}
	return &Daemon{
		db:       db,
		reg:      reg,
		services: services,
		log:      log,
		serverID: srv.ServerID,
	}, nil
}

// ServerID returns the server row id this daemon processes datalog for.
func (d *Daemon) ServerID() uint32 { return d.serverID }

// GuardServer loads the server table and enforces the single-server guard
// (design D2): exactly one active server row with mirror_server_id 0.
// Multi-server and mirror setups belong to PHP ISPConfig; refusing to start
// prevents corrupting a migrated multi-server database.
func GuardServer(db *gorm.DB) (*model.Server, error) {
	var servers []model.Server
	if err := db.Where("active = 1").Find(&servers).Error; err != nil {
		return nil, fmt.Errorf("engine: loading server table: %w", err)
	}
	switch {
	case len(servers) == 0:
		return nil, errors.New("engine: no active server row found; run migrate first")
	case len(servers) > 1:
		return nil, fmt.Errorf("engine: %d active server rows found; multi-server setups are not supported by go-ispconfig, refusing to start", len(servers))
	case servers[0].MirrorServerID != 0:
		return nil, fmt.Errorf("engine: server %d has mirror_server_id %d; mirror setups are not supported by go-ispconfig, refusing to start",
			servers[0].ServerID, servers[0].MirrorServerID)
	}
	return &servers[0], nil
}

// LockPath returns the single-instance lock file location: /run when
// writable (root), the system temp dir otherwise.
func LockPath() string {
	dir := "/run"
	if f, err := os.OpenFile(filepath.Join(dir, ".go-ispconfig-probe"), os.O_CREATE|os.O_WRONLY, 0o600); err != nil {
		dir = os.TempDir()
	} else {
		_ = f.Close()
		_ = os.Remove(f.Name())
	}
	return filepath.Join(dir, "go-ispconfig-daemon.lock")
}

// acquireLock takes an exclusive non-blocking flock on path, the
// single-instance guard (port of the server.php pidfile check). The
// returned file must stay open for the daemon's lifetime.
func acquireLock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("engine: opening lock file %s: %w", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("engine: another daemon instance holds %s: %w", path, err)
	}
	return f, nil
}

// Run acquires the single-instance lock and processes cycles on a ticker
// until ctx is canceled. Cycle errors are logged, never fatal: the next
// tick retries (the database may be temporarily down).
func (d *Daemon) Run(ctx context.Context, tick time.Duration) error {
	lock, err := acquireLock(LockPath())
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()

	d.log.Info("daemon started", "server_id", d.serverID, "tick", tick.String())
	ticker := time.NewTicker(tick)
	defer ticker.Stop()

	for {
		if err := d.RunCycle(ctx); err != nil && !errors.Is(err, context.Canceled) {
			d.log.Error("daemon cycle failed", "error", err)
		}
		select {
		case <-ctx.Done():
			d.drain()
			d.log.Info("daemon stopped")
			return nil
		case <-ticker.C:
		}
	}
}

// drain waits for an in-flight cycle (e.g. a concurrent Wake) to finish and
// flushes any delayed service actions it left queued, so SIGTERM never drops
// a pending nginx/bind reload.
func (d *Daemon) drain() {
	d.busy.Lock()
	defer d.busy.Unlock()
	if d.services != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		d.services.ProcessDelayedActions(ctx)
	}
}

// RunCycle executes one full processing cycle: datalog batch, remote
// actions, delayed service restarts. If a previous cycle is still running
// the call is skipped (single in-process worker).
func (d *Daemon) RunCycle(ctx context.Context) error {
	if !d.busy.TryLock() {
		d.log.Warn("daemon tick skipped: previous cycle still running")
		return nil
	}
	defer d.busy.Unlock()
	return d.cycle(ctx)
}

// Wake runs a processing cycle for a datalog:ready task (design D12 instant
// wake). Unlike the tick path it waits for an in-flight cycle instead of
// skipping, so a change committed while a cycle runs is still processed
// immediately afterwards; the single-cycle lock is always respected.
func (d *Daemon) Wake(ctx context.Context) error {
	d.busy.Lock()
	defer d.busy.Unlock()
	return d.cycle(ctx)
}

// cycle is the shared cycle body; callers must hold busy.
func (d *Daemon) cycle(ctx context.Context) error {
	if err := d.processDatalog(ctx); err != nil {
		return err
	}
	if err := d.processActions(ctx); err != nil {
		return err
	}
	if d.services != nil {
		d.services.ProcessDelayedActions(ctx)
	}
	return nil
}

// processDatalog ports modules.inc.php processDatalog (single-server path):
// pending rows in datalog_id order, JSON decode with non-JSON quarantine,
// table-hook dispatch and per-row server.updated advancement (PHP crash
// recovery semantics: a crash mid-batch resumes at the first unprocessed
// row).
func (d *Daemon) processDatalog(ctx context.Context) error {
	var srv model.Server
	if err := d.db.WithContext(ctx).Where("server_id = ?", d.serverID).First(&srv).Error; err != nil {
		return fmt.Errorf("engine: loading server row %d: %w", d.serverID, err)
	}

	var rows []model.SysDatalog
	err := d.db.WithContext(ctx).
		Where("datalog_id > ? AND (server_id = ? OR server_id = 0)", srv.Updated, d.serverID).
		Order("datalog_id").Limit(datalogBatchSize).Find(&rows).Error
	if err != nil {
		return fmt.Errorf("engine: loading pending datalog rows: %w", err)
	}
	if len(rows) == 0 {
		return nil
	}
	d.log.Info("processing datalog changes", "count", len(rows))

	for i := range rows {
		row := &rows[i]
		if err := ctx.Err(); err != nil {
			return err
		}

		var data Data
		if len(row.Data) > maxDatalogPayload {
			if err := d.quarantineRow(ctx, row, fmt.Sprintf("datalogError: payload for datalog_id %d (table %s) exceeds %d bytes, refusing to parse", row.DatalogID, row.DBTable, maxDatalogPayload)); err != nil {
				return err
			}
		} else if err := json.Unmarshal([]byte(row.Data), &data); err != nil {
			// Non-JSON payload (leftover PHP serialize from before the
			// cutover): quarantine, record the error, advance (design D2).
			if err := d.quarantineRow(ctx, row, fmt.Sprintf("datalogError: non-JSON payload for datalog_id %d (table %s): %v", row.DatalogID, row.DBTable, err)); err != nil {
				return err
			}
		} else if len(data.Old) == 0 && len(data.New) == 0 {
			if err := d.sysLog(ctx, row.DatalogID, logLevelWarn, fmt.Sprintf("Data array was empty for datalog_id %d", row.DatalogID)); err != nil {
				d.log.Error("failed to write sys_log entry", "error", err)
			}
		} else if err := d.reg.RaiseTableHook(row.DBTable, row.Action, data); err != nil {
			if err := d.quarantineRow(ctx, row, fmt.Sprintf("datalogError: processing datalog_id %d (table %s) failed: %v", row.DatalogID, row.DBTable, err)); err != nil {
				return err
			}
		}

		if err := d.advance(ctx, row.DatalogID); err != nil {
			return err
		}
		d.log.Debug("processed datalog row", "datalog_id", row.DatalogID, "table", row.DBTable, "action", row.Action)
	}
	return nil
}

// advance sets server.updated to the just-processed datalog id (per-row,
// PHP semantics).
func (d *Daemon) advance(ctx context.Context, datalogID uint32) error {
	err := d.db.WithContext(ctx).Model(&model.Server{}).
		Where("server_id = ?", d.serverID).Update("updated", datalogID).Error
	if err != nil {
		return fmt.Errorf("engine: advancing server.updated to %d: %w", datalogID, err)
	}
	return nil
}

// quarantineRow marks a datalog row as failed (status/error columns) and
// writes a sys_log error entry. It returns an error when either write fails:
// server.updated must never advance past a bad row without the error being
// recorded (design D2), so a failed quarantine aborts the cycle and the next
// tick retries the row.
func (d *Daemon) quarantineRow(ctx context.Context, row *model.SysDatalog, msg string) error {
	d.log.Error("datalog row quarantined", "datalog_id", row.DatalogID, "table", row.DBTable, "message", msg)
	err := d.db.WithContext(ctx).Model(&model.SysDatalog{}).
		Where("datalog_id = ?", row.DatalogID).
		Updates(map[string]any{"status": "error", "error": msg}).Error
	if err != nil {
		return fmt.Errorf("engine: marking datalog row %d as quarantined: %w", row.DatalogID, err)
	}
	if err := d.sysLog(ctx, row.DatalogID, logLevelError, msg); err != nil {
		return fmt.Errorf("engine: recording quarantine of datalog row %d: %w", row.DatalogID, err)
	}
	return nil
}

// sysLog writes a sys_log row, the DB-visible daemon log ISPConfig tools
// and the monitor UI read.
func (d *Daemon) sysLog(ctx context.Context, datalogID uint32, level int8, msg string) error {
	entry := model.SysLog{
		ServerID:  d.serverID,
		DatalogID: datalogID,
		Loglevel:  level,
		Tstamp:    uint32(time.Now().Unix()),
		Message:   msg,
	}
	return d.db.WithContext(ctx).Create(&entry).Error
}

// processActions ports modules.inc.php processActions: pending
// sys_remoteaction rows for this server are dispatched through the action
// registry and their resulting state stored (4.4b).
func (d *Daemon) processActions(ctx context.Context) error {
	var actions []model.SysRemoteAction
	err := d.db.WithContext(ctx).
		Where("server_id = ? AND action_state = 'pending'", d.serverID).
		Order("action_id").Find(&actions).Error
	if err != nil {
		return fmt.Errorf("engine: loading pending remote actions: %w", err)
	}
	for _, a := range actions {
		if err := ctx.Err(); err != nil {
			return err
		}
		state := d.reg.RaiseAction(a.ActionType, a.ActionParam)
		err := d.db.WithContext(ctx).Model(&model.SysRemoteAction{}).
			Where("action_id = ?", a.ActionID).Update("action_state", state).Error
		if err != nil {
			return fmt.Errorf("engine: storing state for action %d: %w", a.ActionID, err)
		}
		d.log.Info("processed remote action", "action_id", a.ActionID, "type", a.ActionType, "state", state)
	}
	return nil
}
