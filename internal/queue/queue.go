// Package queue wraps the asynq task queue (design D12): per-server queues
// on Redis/Valkey for system jobs and multiserver coordination. Producers
// enqueue tasks targeted at the owning server's queue ("server:<id>"); each
// daemon consumes only its own queue through an embedded worker.
//
// asynq transports work, never state: sys_datalog remains the source of
// truth for configuration changes, so a lost Redis degrades to warnings on
// the producer side and tick polling on the consumer side — it never loses
// configuration and never brings the daemon down.
package queue

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/hibiken/asynq"

	"go-ispconfig/internal/config"
)

// Task type names transported through the queue.
const (
	// TypeDatalogReady is the instant-wake signal: committed sys_datalog
	// rows are waiting for the owning server's daemon. Payload: none.
	TypeDatalogReady = "datalog:ready"
	// TypeSchedulerJob triggers one named scheduler job (a D1b cron job
	// registered as an asynq periodic task). Payload: the job name.
	TypeSchedulerJob = "scheduler:job"
)

// datalogReadyTTL is the uniqueness window of datalog:ready tasks: while one
// wake is pending for a server, further wakes are dropped (the daemon
// processes the whole backlog in one cycle anyway).
const datalogReadyTTL = 30 * time.Second

// Name returns the per-server queue name ("server:<id>") every task
// targeted at that server is enqueued on.
func Name(serverID uint32) string {
	return fmt.Sprintf("server:%d", serverID)
}

// redisOpt converts the [queue] config section into an asynq connection.
func redisOpt(cfg config.QueueConfig) asynq.RedisClientOpt {
	return asynq.RedisClientOpt{Addr: cfg.Addr, DB: cfg.DB, Password: cfg.Password}
}

// Client enqueues tasks onto per-server queues. Creating a Client never
// dials Redis; enqueue calls fail (and are expected to be degraded to
// warnings by callers) while Redis is unreachable.
type Client struct {
	c   *asynq.Client
	log *slog.Logger
}

// NewClient builds a Client from the [queue] config section, logging through
// log (nil means slog.Default).
func NewClient(cfg config.QueueConfig, log *slog.Logger) *Client {
	if log == nil {
		log = slog.Default()
	}
	return &Client{c: asynq.NewClient(redisOpt(cfg)), log: log}
}

// Close releases the underlying Redis connections.
func (c *Client) Close() error { return c.c.Close() }

// Enqueue puts a task on serverID's queue. Passed options are appended after
// the queue routing, so callers may add uniqueness, retry limits or
// deadlines; asynq's default retry/backoff policy applies otherwise.
func (c *Client) Enqueue(serverID uint32, task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	return c.c.Enqueue(task, append([]asynq.Option{asynq.Queue(Name(serverID))}, opts...)...)
}

// EnqueueDatalogReady signals serverID's daemon that committed datalog rows
// are waiting. The task is unique per server for a short TTL (duplicate
// wakes are dropped) and any enqueue failure is a warning only — the tick
// polling fallback picks the change up (design D12).
func (c *Client) EnqueueDatalogReady(serverID uint32) {
	_, err := c.Enqueue(serverID, asynq.NewTask(TypeDatalogReady, nil),
		asynq.Unique(datalogReadyTTL), asynq.MaxRetry(2))
	switch {
	case err == nil:
	case errors.Is(err, asynq.ErrDuplicateTask):
		c.log.Debug("datalog:ready wake already pending", "server_id", serverID)
	default:
		c.log.Warn("failed to enqueue datalog:ready wake, daemon will catch up on next tick",
			"server_id", serverID, "error", err)
	}
}

// ReadyNotifier returns a datalog.SetReadyNotifier-compatible function that
// enqueues a datalog:ready wake for the changed record's server. A record
// with server_id 0 is an every-server broadcast; with the single-server
// guard in place it targets defaultServerID (the panel's own active server).
func (c *Client) ReadyNotifier(defaultServerID uint32) func(serverID uint32) {
	return func(serverID uint32) {
		if serverID == 0 {
			serverID = defaultServerID
		}
		c.EnqueueDatalogReady(serverID)
	}
}

// periodicEntry is one cron entry registered before Run starts the embedded
// asynq scheduler.
type periodicEntry struct {
	spec string
	name string
}

// Worker is the daemon-embedded asynq consumer for one server: it processes
// only that server's queue and, when periodic entries are registered, also
// runs the asynq scheduler that enqueues them on their cron specs. A Redis
// outage never stops it — asynq retries the broker connection internally and
// processing resumes when Redis is back.
type Worker struct {
	serverID   uint32
	redis      asynq.RedisClientOpt
	mux        *asynq.ServeMux
	log        *slog.Logger
	periodic   []periodicEntry
	retryDelay asynq.RetryDelayFunc // test knob; nil = asynq default backoff
}

// NewWorker builds a Worker consuming serverID's queue with the [queue]
// config section, logging through log (nil means slog.Default).
func NewWorker(cfg config.QueueConfig, serverID uint32, log *slog.Logger) *Worker {
	if log == nil {
		log = slog.Default()
	}
	return &Worker{serverID: serverID, redis: redisOpt(cfg), mux: asynq.NewServeMux(), log: log}
}

// Handle registers fn for one task type. Returning an error marks the task
// failed and asynq retries it per the task's retry policy.
func (w *Worker) Handle(taskType string, fn func(ctx context.Context, payload []byte) error) {
	w.mux.HandleFunc(taskType, func(ctx context.Context, t *asynq.Task) error {
		return fn(ctx, t.Payload())
	})
}

// HandleSchedulerJobs registers run as the executor of scheduler:job tasks;
// it receives the job name carried in the payload. Periodic jobs are not
// retried on failure: their status is mirrored to sys_config and the next
// activation runs them again (D1b parity).
func (w *Worker) HandleSchedulerJobs(run func(ctx context.Context, name string) error) {
	w.Handle(TypeSchedulerJob, func(ctx context.Context, payload []byte) error {
		return run(ctx, string(payload))
	})
}

// RegisterPeriodic adds a cron entry enqueueing a scheduler:job task for the
// named job on this worker's queue. spec accepts standard 5-field cron plus
// @descriptors (including "@every <duration>"); it is validated when Run
// starts the scheduler.
func (w *Worker) RegisterPeriodic(spec, name string) {
	w.periodic = append(w.periodic, periodicEntry{spec: spec, name: name})
}

// Run starts the asynq server (and scheduler, when periodic entries exist),
// blocks until ctx is canceled and then shuts both down gracefully, waiting
// for in-flight handlers. The returned error only reports startup failures;
// runtime Redis outages are logged and retried internally by asynq.
func (w *Worker) Run(ctx context.Context) error {
	srv := asynq.NewServer(w.redis, asynq.Config{
		Queues:         map[string]int{Name(w.serverID): 1},
		Concurrency:    4,
		Logger:         slogAdapter{w.log},
		RetryDelayFunc: w.retryDelay,
	})
	if err := srv.Start(w.mux); err != nil {
		return fmt.Errorf("queue: starting worker: %w", err)
	}
	defer srv.Shutdown()

	if len(w.periodic) > 0 {
		sched := asynq.NewScheduler(w.redis, &asynq.SchedulerOpts{
			Logger: slogAdapter{w.log},
			EnqueueErrorHandler: func(task *asynq.Task, _ []asynq.Option, err error) {
				if !errors.Is(err, asynq.ErrDuplicateTask) {
					w.log.Warn("periodic task enqueue failed", "type", task.Type(), "error", err)
				}
			},
		})
		for _, p := range w.periodic {
			_, err := sched.Register(p.spec, asynq.NewTask(TypeSchedulerJob, []byte(p.name)),
				asynq.Queue(Name(w.serverID)), asynq.Unique(time.Minute), asynq.MaxRetry(0))
			if err != nil {
				return fmt.Errorf("queue: registering periodic task %q (%s): %w", p.name, p.spec, err)
			}
		}
		if err := sched.Start(); err != nil {
			return fmt.Errorf("queue: starting periodic scheduler: %w", err)
		}
		defer sched.Shutdown()
	}

	<-ctx.Done()
	w.log.Info("queue worker stopping")
	return nil
}

// slogAdapter bridges *slog.Logger to the asynq.Logger interface.
type slogAdapter struct{ l *slog.Logger }

// Debug logs at debug level.
func (a slogAdapter) Debug(args ...any) { a.l.Debug(fmt.Sprint(args...)) }

// Info logs at info level.
func (a slogAdapter) Info(args ...any) { a.l.Info(fmt.Sprint(args...)) }

// Warn logs at warn level.
func (a slogAdapter) Warn(args ...any) { a.l.Warn(fmt.Sprint(args...)) }

// Error logs at error level.
func (a slogAdapter) Error(args ...any) { a.l.Error(fmt.Sprint(args...)) }

// Fatal logs at error level; asynq only calls it on unrecoverable startup
// problems and the daemon must keep running regardless.
func (a slogAdapter) Fatal(args ...any) { a.l.Error(fmt.Sprint(args...)) }
