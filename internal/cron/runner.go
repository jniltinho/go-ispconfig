package cron

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	robfig "github.com/robfig/cron/v3"
)

// JobFunc is the body of one client cron job execution.
type JobFunc func(ctx context.Context)

// Job describes a client cron row for registration on the runner.
type Job struct {
	// ID is the cron table primary key (map key in the runner).
	ID uint32
	// Schedule fields from the cron table (spaces stripped on compose).
	RunMin   string
	RunHour  string
	RunMday  string
	RunMonth string
	RunWday  string
	// Run is invoked when the schedule fires (or once at Start for @reboot).
	Run JobFunc
}

// ClientJobRunner is the in-process client-job schedule set (design D2):
// keyed by cron.id, backed by robfig/cron/v3, distinct from engine.Scheduler.
// @reboot jobs run once when Start is called (daemon start), not as recurring
// entries. The runner never writes under crontab_dir.
type ClientJobRunner struct {
	mu      sync.Mutex
	c       *robfig.Cron
	entries map[uint32]robfig.EntryID
	reboot  map[uint32]JobFunc
	log     *slog.Logger
	// runCtx is cancelled on Stop; job funcs receive it.
	runCtx    context.Context
	runCancel context.CancelFunc
	started   bool
}

// NewClientJobRunner creates a stopped runner. Call Start after registering
// jobs (or register while running — Add/Replace are safe either way).
func NewClientJobRunner(log *slog.Logger) *ClientJobRunner {
	if log == nil {
		log = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &ClientJobRunner{
		c:         robfig.New(),
		entries:   map[uint32]robfig.EntryID{},
		reboot:    map[uint32]JobFunc{},
		log:       log,
		runCtx:    ctx,
		runCancel: cancel,
	}
}

// ComposeExpression builds a standard 5-field cron expression from the
// ISPConfig schedule columns after stripping spaces per field (port of
// cron_plugin::_write_crontab). When run_month is @reboot, returns the
// literal "@reboot" (not a robfig descriptor).
func ComposeExpression(runMin, runHour, runMday, runMonth, runWday string) string {
	month := strings.ReplaceAll(runMonth, " ", "")
	if month == "@reboot" {
		return "@reboot"
	}
	return strings.Join([]string{
		strings.ReplaceAll(runMin, " ", ""),
		strings.ReplaceAll(runHour, " ", ""),
		strings.ReplaceAll(runMday, " ", ""),
		month,
		strings.ReplaceAll(runWday, " ", ""),
	}, " ")
}

// Add registers a job. If an entry with the same ID already exists it is
// replaced. For @reboot schedules the job is stored for run-once-on-start
// (and run immediately if the runner has already Started).
func (r *ClientJobRunner) Add(job Job) error {
	if job.ID == 0 {
		return fmt.Errorf("cron: job id is required")
	}
	if job.Run == nil {
		return fmt.Errorf("cron: job %d has nil Run func", job.ID)
	}
	expr := ComposeExpression(job.RunMin, job.RunHour, job.RunMday, job.RunMonth, job.RunWday)

	r.mu.Lock()
	defer r.mu.Unlock()
	r.removeLocked(job.ID)

	if expr == "@reboot" {
		r.reboot[job.ID] = job.Run
		if r.started {
			go r.safeRun(job.ID, job.Run)
		}
		return nil
	}

	sched, err := robfig.ParseStandard(expr)
	if err != nil {
		return fmt.Errorf("cron: parsing expression %q for job %d: %w", expr, job.ID, err)
	}
	id := job.ID
	fn := job.Run
	entryID := r.c.Schedule(sched, robfig.FuncJob(func() {
		r.safeRun(id, fn)
	}))
	r.entries[job.ID] = entryID
	return nil
}

// Replace is an alias of Add (insert or update the entry for job.ID).
func (r *ClientJobRunner) Replace(job Job) error { return r.Add(job) }

// Remove drops the scheduled entry (recurring or @reboot) for id.
func (r *ClientJobRunner) Remove(id uint32) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.removeLocked(id)
}

func (r *ClientJobRunner) removeLocked(id uint32) {
	if entryID, ok := r.entries[id]; ok {
		r.c.Remove(entryID)
		delete(r.entries, id)
	}
	delete(r.reboot, id)
}

// Has reports whether id is currently registered (recurring or @reboot).
func (r *ClientJobRunner) Has(id uint32) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, rec := r.entries[id]
	_, reb := r.reboot[id]
	return rec || reb
}

// Len returns the number of registered jobs (recurring + @reboot).
func (r *ClientJobRunner) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.entries) + len(r.reboot)
}

// ExpressionOf returns the composed expression for a job's schedule fields.
// Convenience for tests and callers that need the string without registering.
func ExpressionOf(job Job) string {
	return ComposeExpression(job.RunMin, job.RunHour, job.RunMday, job.RunMonth, job.RunWday)
}

// Start begins the in-process scheduler and runs every currently registered
// @reboot job once (design D3). Safe to call once; subsequent calls are no-ops.
func (r *ClientJobRunner) Start() {
	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		return
	}
	r.started = true
	r.c.Start()
	// Snapshot reboot funcs under the lock, then run outside.
	reboot := make(map[uint32]JobFunc, len(r.reboot))
	for id, fn := range r.reboot {
		reboot[id] = fn
	}
	r.mu.Unlock()

	for id, fn := range reboot {
		go r.safeRun(id, fn)
	}
}

// Stop cancels in-flight job contexts and stops the scheduler. It waits for
// robfig's running jobs to finish (best-effort).
func (r *ClientJobRunner) Stop() {
	r.mu.Lock()
	if !r.started {
		r.mu.Unlock()
		return
	}
	r.started = false
	r.runCancel()
	// Fresh context for any future restart (tests).
	r.runCtx, r.runCancel = context.WithCancel(context.Background())
	ctx := r.c.Stop()
	r.mu.Unlock()
	<-ctx.Done()
}

func (r *ClientJobRunner) safeRun(id uint32, fn JobFunc) {
	defer func() {
		if rec := recover(); rec != nil {
			r.log.Error("cron: job panicked", "id", id, "panic", rec)
		}
	}()
	r.mu.Lock()
	ctx := r.runCtx
	r.mu.Unlock()
	fn(ctx)
}
