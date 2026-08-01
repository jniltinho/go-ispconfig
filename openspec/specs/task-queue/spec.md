# task-queue Specification

## Purpose
TBD - created by archiving change port-ispconfig3-to-go. Update Purpose after archive.
## Requirements
### Requirement: asynq-backed system task queue
The system SHALL run system jobs through asynq (github.com/hibiken/asynq) backed by Redis or Valkey, configured via the `[queue]` section of config.toml. Each server's daemon SHALL consume only its own queue (`server:<server_id>`); producers (API, scheduler, modules) SHALL enqueue jobs targeted at the owning server's queue. Tasks SHALL declare retry/backoff and, where applicable, uniqueness.

#### Scenario: Job routed to owning server
- **WHEN** a job for server 2 is enqueued on a panel handling multiple servers
- **THEN** only server 2's daemon executes it, with retries on failure per the task's policy

#### Scenario: Queue observable
- **WHEN** an operator points asynqmon (or asynq CLI) at the configured Redis
- **THEN** pending/active/failed tasks and queues are inspectable

### Requirement: Scheduler jobs as asynq periodic tasks
The internal scheduler's cron jobs (D1b) SHALL be registered as asynq periodic tasks; per-job last-run/status SHALL continue to be mirrored to `sys_config` (group `scheduler`) for the panel API.

#### Scenario: Periodic job runs once across restarts
- **WHEN** the daemon restarts around a job's scheduled time
- **THEN** the job executes exactly once for that activation and its status is recorded

### Requirement: Instant datalog wake with polling fallback
After committing a tracked change, the datalog writer SHALL enqueue a lightweight `datalog:ready` task for the owning server so its daemon processes the change immediately. sys_datalog SHALL remain the source of truth: with Redis unavailable, the daemon SHALL keep functioning via its tick polling and the writer SHALL NOT fail the business transaction because of an enqueue error (log-only).

#### Scenario: Change applied without waiting for tick
- **WHEN** a tracked record changes and Redis is healthy
- **THEN** the daemon starts processing the datalog before the next tick fires

#### Scenario: Redis outage degrades gracefully
- **WHEN** Redis is down and a tracked record changes
- **THEN** the write succeeds, a warning is logged, and the change is applied on the next poll tick

