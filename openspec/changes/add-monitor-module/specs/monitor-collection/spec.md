# monitor-collection

## ADDED Requirements

### Requirement: Scheduler-driven metric collection into monitor_data
The daemon SHALL register named jobs on the foundation internal scheduler that collect server metrics and write rows into the existing `monitor_data` table (`server_id`, `type`, `created`, `data`, `state`) without any schema change. Jobs replace ISPConfig3's `server/lib/classes/cron.d/100-monitor_*.inc.php` schedule: most checks every 5 minutes (`*/5 * * * *`), `sys_usage` every minute (`* * * * *`), `system_update` hourly when implemented (`0 * * * *`). Collection SHALL run only in the daemon process (never in the API/serve process).

#### Scenario: Five-minute job writes a fresh row
- **WHEN** the `monitor_disk_usage` job runs successfully for `server_id = 1`
- **THEN** a new `monitor_data` row exists with `type = 'disk_usage'`, `server_id = 1`, `created` equal to the run unix timestamp, non-empty `data`, and a valid `state` enum value

#### Scenario: API process does not open /proc or log files
- **WHEN** any authenticated client calls a monitor read endpoint
- **THEN** the serve process answers solely from the database (and config) and does not read host metrics or log paths itself

### Requirement: In-scope check types and payloads
The system SHALL collect at least these `type` values with behavioral parity to the named PHP cron jobs: `cpu_info`, `mem_usage`, `disk_usage`, `server_load`, `services`, `os_info`, `kernel_info`, `ispc_info`, `sys_usage`, `sys_log`, `log_ispconfig`, `log_letsencrypt`, `log_messages`, and optionally `system_update`. Metric sources SHALL use gopsutil where applicable; service probes SHALL follow `monitor_tools::monitorServices` (TCP/UDP/FTP/DB) gated by `server.web_server`, `file_server`, `mail_server`, `dns_server`, `db_server`. JSON `data` keys SHALL match the PHP arrays the UI expects (e.g. disk rows with `fs`, `type`, `size`, `used`, `available`, `percent`, `mounted`; services keys `webserver`, `ftpserver`, `smtpserver`, `pop3server`, `imapserver`, `bindserver`, `mysqlserver` with values `1`/`0`/`-1`).

#### Scenario: Services state is error when nginx is down
- **WHEN** `server.web_server = 1` and TCP connect to localhost:80 fails
- **THEN** the `services` row has `data.webserver = 0` and `state = 'error'`

#### Scenario: Inactive service role is unused
- **WHEN** `server.dns_server = 0`
- **THEN** the `services` row stores `bindserver = -1` and that probe does not force `state = 'error'`

#### Scenario: Disk fill thresholds raise severity
- **WHEN** a non-tmpfs filesystem reports use percent > 90 and free size < 500 MiB
- **THEN** the `disk_usage` row `state` is at least `critical` (via monotonic `_setState` semantics)

#### Scenario: Load thresholds match PHP
- **WHEN** load_1 is 55
- **THEN** `server_load.state` is `warning` (thresholds: >20 info, >50 warning, >100 critical, >150 error)

### Requirement: JSON writes with dual-format readers
New `monitor_data.data` writes SHALL be JSON. Readers SHALL accept both JSON and PHP `serialize()` payloads so pre-cutover history remains readable. `state` MUST be one of the schema enum values: `no_state`, `unknown`, `ok`, `info`, `warning`, `critical`, `error`.

#### Scenario: Fresh write is JSON
- **WHEN** a collection job stores `mem_usage`
- **THEN** `data` is valid JSON and is not PHP-serialized

#### Scenario: Legacy PHP serialize row still decodes
- **WHEN** a pre-cutover `monitor_data` row contains a PHP-serialized array
- **THEN** the decoder returns the equivalent structured value without error

### Requirement: Prune old monitor_data rows
After each successful write for a given `(type, server_id)`, the job SHALL delete rows of that type and server with `created < now - 240` seconds (port of `monitor_tools::delOldRecords`), always scoping by `server_id` so multi-server clock skew cannot drop another server's newest sample.

#### Scenario: Rows older than four minutes are removed
- **WHEN** a job writes a new `cpu_info` sample and older samples exist for the same server with `created` older than 240 seconds
- **THEN** those older rows are deleted and the new row remains

#### Scenario: Prune never crosses server_id
- **WHEN** server 1 prunes `disk_usage`
- **THEN** no `disk_usage` row belonging to `server_id = 2` is deleted

### Requirement: sys_usage rolling series for dashlets
The `sys_usage` job SHALL maintain inside its JSON payload rolling arrays (max 15 points) for load percent, memory percent, network rx/tx KB/s and time labels (port of `100-monitor_sys_usage.inc.php`), suitable for dashboard metric widgets.

#### Scenario: Series length is capped
- **WHEN** `sys_usage` has already accumulated 15 load points and runs again
- **THEN** the oldest load point is dropped and the newest is appended (length stays 15)

### Requirement: State fold helper
A shared `_setState`-equivalent helper SHALL implement the PHP weight order (`no_state` < `ok` < `unknown` < `info` < `warning` < `critical` < `error`) and only promote severity, never demote.

#### Scenario: Critical is not lowered by ok
- **WHEN** the current state is `critical` and a check reports `ok`
- **THEN** the folded state remains `critical`
