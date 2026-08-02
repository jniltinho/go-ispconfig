# monitor-ui

## ADDED Requirements

### Requirement: Monitor module navigation
The panel SHALL register a Monitor module (id `monitor`) visible when the signed-in user's `sys_user.modules` list includes `monitor`, with sidebar sections covering: System state overview, check detail entry points (CPU, memory, disk, load, services, OS/kernel, updates when available), ISPConfig log (`sys_log`), Jobqueue, Data log history, log tails relevant to the stack (ISPConfig log file, Let's Encrypt log, system log), and (admin) Scheduler jobs. All user-visible strings SHALL go through the i18n layer with English keys in `en.json`.

#### Scenario: User without monitor module sees no Monitor nav
- **WHEN** a user whose `modules` CSV omits `monitor` opens the panel
- **THEN** the Monitor top-level module is not listed

#### Scenario: Admin with monitor module sees full sidebar
- **WHEN** the seeded admin (modules include `monitor`) opens the panel
- **THEN** System state, ISPConfig log, Jobqueue, Data log history and Scheduler jobs sections are available

### Requirement: System state overview
The System state view SHALL port `show_sys_state.php`: an all-servers summary and a per-server detail with state-colored cards, aggregated severity from the newest `monitor_data` rows per type, and human-readable messages for disk, load, services, updates and sys_log (port of `_processDbState`). Optional auto-refresh intervals of 5/10/15/30/60 minutes (not below the 5-minute collection cadence) MAY be offered.

#### Scenario: Server card shows error when a service is down
- **WHEN** the latest `services` row for the selected server has `state = 'error'`
- **THEN** the server card severity is at least `error` and a services message links to the services detail view

#### Scenario: Empty monitor_data shows unknown-friendly empty state
- **WHEN** no `monitor_data` rows exist yet for the server
- **THEN** the UI shows an empty/unknown state without crashing

### Requirement: Check detail views
For each in-scope type the UI SHALL offer a detail view porting `show_data.php` / `show_log.php` consumers: structured tables for `cpu_info`, `mem_usage`, `disk_usage`, `server_load`, `services`, `os_info`, `kernel_info`, `ispc_info`, `system_update`; monospace/preformatted tail for `log_ispconfig`, `log_letsencrypt`, `log_messages`. Each view SHALL show the collection timestamp (`created`) and server name.

#### Scenario: Disk usage table lists filesystems
- **WHEN** the user opens Disk usage and a fresh `disk_usage` sample exists
- **THEN** each filesystem row shows mountpoint, size, used, available and percent

#### Scenario: Services list marks down services
- **WHEN** `webserver = 0` in the latest `services` data
- **THEN** the webserver row is shown as down/error in the UI

### Requirement: sys_log viewer with clear actions
The ISPConfig log screen SHALL list `sys_log` via DataTable with filters for `server_id`, `loglevel` (0 Debug, 1 Warning, 2 Error) and message substring, ordered by `tstamp DESC, syslog_id DESC` (port of `log_list.php` / `log.list.php`). Admin users SHALL be able to clear one entry or batch-clear by loglevel by setting `loglevel = 0` (port of `log_del.php` — not hard delete). Non-admins SHALL not see clear actions.

#### Scenario: Admin clears a warning entry
- **WHEN** an admin clears syslog_id 42
- **THEN** the row remains with `loglevel = 0` and disappears from the default warning/error filtered views if filtered to level 1/2

#### Scenario: Client cannot clear logs
- **WHEN** a non-admin monitor user opens the log list
- **THEN** no clear/batch-clear controls are rendered and clear API calls return 403

### Requirement: Dashboard dashlets
The existing dashboard SHALL gain small monitor dashlets: server state summary (worst severity / short label), failed scheduler jobs count (status not `ok`), and last datalog / jobqueue activity (pending count or latest tstamp). Dashlets SHALL call the monitor/system APIs and degrade gracefully when data is empty.

#### Scenario: Jobqueue dashlet shows pending count
- **WHEN** three datalog rows are pending for the local server
- **THEN** the jobqueue dashlet displays count 3

### Requirement: E2E coverage of the Monitor UI
agent-browser E2E tests SHALL cover: login as admin, open Monitor system state, open at least one metric detail (disk or services), open sys_log list, open jobqueue, open datalog history list and one detail, open scheduler jobs (admin). Screenshots MAY be written under `docs/prints/` for human review.

#### Scenario: E2E suite passes against a seeded panel
- **WHEN** the Monitor E2E suite runs against a built binary with MariaDB seed data and sample `monitor_data` / `sys_log` / `sys_datalog` rows
- **THEN** all listed flows complete without errors
