# scheduler-status

## ADDED Requirements

### Requirement: Scheduler job status API includes spec and next run
The existing admin endpoint `GET /api/system/scheduler` (foundation) SHALL be extended so each job reports at least: `name`, `spec` (cron expression), `last_run` (RFC3339 or empty), `next_run` (RFC3339 or empty), and `status` (`ok`, `error: …`, or empty if never run). Job metadata MUST be readable by the standalone `serve` process without IPC to the daemon — by persisting `spec` (e.g. `sys_config` group `scheduler`, key `{name}_spec`) when the daemon registers/runs jobs, in addition to the existing `{name}_last_run` and `{name}_status` mirrors.

#### Scenario: Admin lists jobs after a monitor collection run
- **WHEN** an admin calls `GET /api/system/scheduler` after `monitor_disk_usage` has run once
- **THEN** the response includes an entry with `name` matching the monitor job, non-empty `spec`, a `last_run` timestamp, `status` of `ok` or an `error:` prefix, and a `next_run` at or after `last_run`

#### Scenario: Non-admin is rejected
- **WHEN** a non-admin session calls `GET /api/system/scheduler`
- **THEN** the API returns 403

#### Scenario: Serve process works without live daemon registry
- **WHEN** only `sys_config` scheduler keys are present and the API process has no in-memory job list
- **THEN** the endpoint still returns name/spec/last_run/status/next_run derived from those keys

### Requirement: Monitor collection jobs appear in the registry
Every monitor collection job registered under `monitor-collection` SHALL use a stable unique name (e.g. `monitor_cpu_info`, `monitor_mem_usage`, `monitor_disk_usage`, `monitor_server_load`, `monitor_services`, `monitor_os_info`, `monitor_kernel_info`, `monitor_ispc_info`, `monitor_sys_usage`, `monitor_sys_log`, `monitor_log_ispconfig`, `monitor_log_letsencrypt`, `monitor_log_messages`, and optionally `monitor_system_update`) and SHALL update the scheduler mirrors on each execution through the foundation `Scheduler.RunJob` path.

#### Scenario: Failed collection records error status
- **WHEN** a monitor job returns an error
- **THEN** `{name}_status` in `sys_config` is stored as `error: ` plus the error message and `{name}_last_run` is still updated

#### Scenario: Successful collection records ok
- **WHEN** a monitor job completes without error
- **THEN** `{name}_status` is `ok`

### Requirement: Scheduler jobs UI in the Monitor module
The Monitor panel SHALL include an admin-only "Scheduler jobs" view listing all jobs from `GET /api/system/scheduler` with columns for name, schedule (`spec`), last run, next run and last result, providing the go-ispconfig replacement for ISPConfig cron transparency.

#### Scenario: Admin opens Scheduler jobs
- **WHEN** an admin navigates to Monitor → Scheduler jobs
- **THEN** the table shows at least the registered monitor collection jobs and foundation jobs (e.g. datalog prune) when present

#### Scenario: Non-admin has no scheduler section
- **WHEN** a non-admin user with the monitor module open the Monitor sidebar
- **THEN** the Scheduler jobs section is hidden

### Requirement: Dashboard uses scheduler status for failed-jobs dashlet
The failed-jobs dashboard dashlet SHALL count jobs whose `status` is non-empty and not exactly `ok`, using the scheduler status API.

#### Scenario: One failed job increments the dashlet
- **WHEN** exactly one scheduler job has `status` starting with `error:`
- **THEN** the failed-jobs dashlet shows count 1
