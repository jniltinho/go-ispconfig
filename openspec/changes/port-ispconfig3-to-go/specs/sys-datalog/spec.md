# sys-datalog

## ADDED Requirements

### Requirement: Datalog writer on every tracked mutation
Every create/update/delete on a tracked table performed through the API layer SHALL, in the same transaction, insert a `sys_datalog` row with `server_id`, `dbtable`, `dbidx` (`<pk_column>:<value>`), `action` (`i`/`u`/`d`) and `data` as JSON `{"old":{...},"new":{...}}` containing only changed fields for updates.

#### Scenario: Update writes diff
- **WHEN** a `web_domain` row's `active` flag changes from `y` to `n`
- **THEN** a datalog row is inserted with action `u` and data containing old/new values for `active` only

#### Scenario: Rollback removes datalog
- **WHEN** the surrounding transaction fails after the datalog insert
- **THEN** neither the record change nor the datalog row is persisted

### Requirement: Persistent daemon consumes datalog in order
The daemon SHALL run as a persistent service (systemd unit, no system cron) with an internal ticker (default 10s, configurable) polling `sys_datalog` for rows with `datalog_id > server.updated AND (server_id = ? OR server_id = 0)`, processing them in `datalog_id` order (batch limit 1000), dispatching table-hooks, and advancing `server.updated` after each row (port of `server/server.php` + `modules.inc.php::processDatalog`).

#### Scenario: Pending change applied
- **WHEN** a datalog row for `dns_soa` exists with id greater than `server.updated`
- **THEN** the next daemon cycle dispatches the `dns_soa` table-hook and sets `server.updated` to that datalog_id

#### Scenario: Concurrent processing prevented
- **WHEN** a processing cycle is still running when the next tick fires
- **THEN** the tick is skipped (single in-process worker; a second daemon instance refuses to start)

#### Scenario: Non-JSON legacy row quarantined
- **WHEN** the consumer encounters a datalog row whose payload is not valid JSON (leftover PHP-serialize from before cutover)
- **THEN** the row is skipped with a datalogError log entry and quarantine marker, and `server.updated` advances only with that error recorded — the daemon does not crash

#### Scenario: Crash recovery resumes per row
- **WHEN** the daemon crashes mid-batch
- **THEN** on restart processing resumes at the first row after `server.updated` (per-row advancement, PHP semantics)

### Requirement: Remote action registry
The daemon SHALL port the `sys_remoteaction` mechanism: plugins register named actions (`RegisterAction`), the daemon polls pending rows after datalog processing and dispatches them (`RaiseAction`), recording ok/warning/error state per action.

#### Scenario: Pending action executed
- **WHEN** a `sys_remoteaction` row is pending for this server
- **THEN** the daemon dispatches it to the registered handler and stores the resulting state

### Requirement: Internal scheduler replaces ISPConfig cron jobs
The daemon SHALL provide an internal job scheduler with cron-style specs for periodic tasks (traffic accounting, backups, certificate renewal, datalog pruning — port of `cron_daily.sh` responsibilities), recording last-run time and status per job. Installation SHALL NOT create any system crontab entries.

#### Scenario: Daily job runs and is recorded
- **WHEN** a scheduled job's time spec elapses
- **THEN** the daemon executes it once and persists last-run timestamp and success/failure status queryable via the API

### Requirement: Two-level event dispatch
Modules SHALL register table-hooks and announce named events; plugins SHALL subscribe only to announced events; the dispatcher SHALL raise `<table>_<insert|update|delete>` events carrying the decoded old/new data (port of `plugins.inc.php` registries).

#### Scenario: Unannounced event rejected
- **WHEN** a plugin registers for an event no module announced
- **THEN** registration fails with an error at startup

### Requirement: Delayed service restarts
Plugins SHALL request service actions (`restart`/`reload`) through a services registry that deduplicates and executes them once at the end of a daemon run, with `reload` upgraded to `restart` when both are requested (port of `services.inc.php`).

#### Scenario: Many zone changes, one reload
- **WHEN** ten `dns_rr` datalog rows are processed in one run
- **THEN** bind is reloaded exactly once after the batch
