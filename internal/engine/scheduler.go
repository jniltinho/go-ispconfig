package engine

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/robfig/cron/v3"
	"gorm.io/gorm"

	"go-ispconfig/internal/model"
)

// schedulerConfigGroup is the sys_config group holding per-job last-run and
// status keys. Job state deliberately lives in sys_config, never in the
// legacy sys_cron table (design D1b: sys_cron means system crontabs in PHP
// ISPConfig and must stay untouched).
const schedulerConfigGroup = "scheduler"

// JobFunc is the body of a scheduled job.
type JobFunc func(ctx context.Context) error

// JobStatus describes one registered job with its persisted state, for the
// monitor API listing.
type JobStatus struct {
	// Name is the unique job identifier.
	Name string `json:"name"`
	// Spec is the cron expression ("0 3 * * *", "@daily", ...).
	Spec string `json:"spec"`
	// LastRun is the RFC3339 time of the last execution, empty if never run.
	LastRun string `json:"last_run"`
	// Status is "ok" or "error: <message>" from the last execution, empty if
	// never run.
	Status string `json:"status"`
}

// job is one registered scheduler entry.
type job struct {
	name string
	spec string
	fn   JobFunc
}

// Scheduler holds the named cron-spec jobs replacing every ISPConfig system
// cron (design D1b). Jobs are registered in code and executed as asynq
// periodic tasks (design D12): the daemon registers each job's spec with the
// queue worker, which enqueues a scheduler:job task per activation and
// routes it back to RunJob. Last-run time and status persist in sys_config
// group "scheduler" for the panel API.
type Scheduler struct {
	db   *gorm.DB
	log  *slog.Logger
	jobs []*job
}

// NewScheduler creates a Scheduler persisting job state through db and
// logging through log (nil means slog.Default).
func NewScheduler(db *gorm.DB, log *slog.Logger) *Scheduler {
	if log == nil {
		log = slog.Default()
	}
	return &Scheduler{db: db, log: log}
}

// Register adds a job under a unique name with a standard 5-field cron spec
// or a @daily/@hourly style descriptor. It fails on an invalid spec or a
// duplicate name.
func (s *Scheduler) Register(name, spec string, fn JobFunc) error {
	for _, j := range s.jobs {
		if j.name == name {
			return fmt.Errorf("engine: scheduler job %q already registered", name)
		}
	}
	if _, err := cron.ParseStandard(spec); err != nil {
		return fmt.Errorf("engine: parsing cron spec %q for job %q: %w", spec, name, err)
	}
	s.jobs = append(s.jobs, &job{name: name, spec: spec, fn: fn})
	// Mirror the spec so a standalone serve process can list job metadata
	// (and compute next_run) without talking to the daemon.
	s.setConfig(context.Background(), name+"_spec", spec)
	return nil
}

// RunJob executes the named job and persists its last-run/status mirror in
// sys_config. It is the scheduler:job task handler body: the queue worker
// calls it once per periodic activation. The job's own error is returned so
// the task is marked failed in asynq (observability), after the status was
// already recorded; an unknown name is an error too.
func (s *Scheduler) RunJob(ctx context.Context, name string) error {
	for _, j := range s.jobs {
		if j.name == name {
			return s.runJob(ctx, j, time.Now())
		}
	}
	return fmt.Errorf("engine: scheduler job %q is not registered", name)
}

// runJob executes one job, persists its state and returns the job error.
func (s *Scheduler) runJob(ctx context.Context, j *job, now time.Time) error {
	s.log.Info("scheduler job starting", "job", j.name)
	status := "ok"
	err := j.fn(ctx)
	if err != nil {
		status = "error: " + err.Error()
		s.log.Error("scheduler job failed", "job", j.name, "error", err)
	} else {
		s.log.Info("scheduler job finished", "job", j.name)
	}
	s.setConfig(ctx, j.name+"_last_run", now.Format(time.RFC3339))
	s.setConfig(ctx, j.name+"_status", status)
	return err
}

// Jobs lists every registered job with its persisted last-run/status (the
// data the monitor API endpoint exposes in group 6).
func (s *Scheduler) Jobs(ctx context.Context) []JobStatus {
	out := make([]JobStatus, 0, len(s.jobs))
	for _, j := range s.jobs {
		st := JobStatus{Name: j.name, Spec: j.spec}
		if last := s.lastRun(ctx, j.name); !last.IsZero() {
			st.LastRun = last.Format(time.RFC3339)
		}
		st.Status = s.getConfig(ctx, j.name+"_status")
		out = append(out, st)
	}
	return out
}

// lastRun reads the persisted last-run time of a job, zero when absent or
// unparsable.
func (s *Scheduler) lastRun(ctx context.Context, name string) time.Time {
	v := s.getConfig(ctx, name+"_last_run")
	if v == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}
	}
	return t
}

// getConfig reads one scheduler sys_config value, empty when absent.
func (s *Scheduler) getConfig(ctx context.Context, name string) string {
	var entry model.SysConfig
	err := s.db.WithContext(ctx).
		Where("`group` = ? AND `name` = ?", schedulerConfigGroup, name).
		First(&entry).Error
	if err != nil {
		return ""
	}
	return entry.Value
}

// setConfig upserts one scheduler sys_config value (composite PK
// group+name). A nil db (schedulers built for job-function-only tests) is a
// silent no-op rather than a panic.
func (s *Scheduler) setConfig(ctx context.Context, name, value string) {
	if s.db == nil {
		return
	}
	err := s.db.WithContext(ctx).
		Exec("REPLACE INTO sys_config (`group`, `name`, `value`) VALUES (?, ?, ?)",
			schedulerConfigGroup, name, value).Error
	if err != nil {
		s.log.Error("scheduler failed to persist job state", "name", name, "error", err)
	}
}

// RegisterDatalogPruning adds the built-in datalog pruning job: every day at
// 03:00 it deletes sys_datalog rows already processed by this server
// (datalog_id <= server.updated) older than retentionDays.
func (s *Scheduler) RegisterDatalogPruning(serverID uint32, retentionDays int) error {
	return s.Register("datalog_prune", "0 3 * * *", func(ctx context.Context) error {
		var srv model.Server
		if err := s.db.WithContext(ctx).Where("server_id = ?", serverID).First(&srv).Error; err != nil {
			return fmt.Errorf("loading server row: %w", err)
		}
		cutoff := time.Now().AddDate(0, 0, -retentionDays).Unix()
		res := s.db.WithContext(ctx).
			Where("datalog_id <= ? AND tstamp < ?", srv.Updated, cutoff).
			Delete(&model.SysDatalog{})
		if res.Error != nil {
			return fmt.Errorf("pruning sys_datalog: %w", res.Error)
		}
		s.log.Info("datalog pruning done", "deleted", res.RowsAffected, "retention_days", retentionDays)
		return nil
	})
}
