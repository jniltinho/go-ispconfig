# datalog-history

## ADDED Requirements

### Requirement: Pending jobqueue listing
The system SHALL expose the pending datalog jobqueue as a paginated list of `sys_datalog` rows that the daemon has not yet consumed: for each `server` row, rows with `datalog_id > server.updated` and `server_id IN (0, server.server_id)` (port of `datalog_list.php` and `monitor_jobqueue_count`). Filters SHALL include `tstamp`, `server_id`, `action` (`i`/`u`/`d`) and `dbtable` (port of `datalog.list.php`). Access requires the `monitor` module permission.

#### Scenario: Pending row is listed
- **WHEN** server 1 has `updated = 100` and a `sys_datalog` row exists with `datalog_id = 101`, `server_id = 1`
- **THEN** the jobqueue list includes that row

#### Scenario: Already-consumed row is absent from jobqueue
- **WHEN** server 1 has `updated = 200` and a row has `datalog_id = 150`, `server_id = 1`
- **THEN** the jobqueue list does not include that row

#### Scenario: Jobqueue count matches remote API semantics
- **WHEN** `GET /api/monitor/jobqueue/count` is called with `server_id = 0` (all servers)
- **THEN** the returned count equals the number of pending rows across every server using the per-server `updated` watermark

### Requirement: Full datalog history browser
The system SHALL list all `sys_datalog` rows (not only pending) ordered by `tstamp DESC, datalog_id DESC` with filters for `tstamp`, `server_id`, `action`, `dbtable` and `user` (port of `dataloghistory_list.php` / `dataloghistory.list.php`). Columns shown SHALL include timestamp, server, action, table, index (`dbidx`), user and status.

#### Scenario: History includes processed rows
- **WHEN** a datalog row has already been applied (`datalog_id <= server.updated`)
- **THEN** it still appears in the history list

#### Scenario: Filter by action update
- **WHEN** the history list is filtered with `action = u`
- **THEN** only update rows are returned

### Requirement: Dual-decoded old/new detail view
`GET` of a single datalog entry SHALL return metadata plus a structured field diff decoded from `sys_datalog.data` (port of `dataloghistory_view.php` without undo). Decoding order: JSON `{"old":...,"new":...}` first; else PHP `serialize` dual-format; else expose raw payload and a decode error flag. Presentation rules: action `i` → new fields; `u` → per-key old/new (and optional line diff for multi-line values); `d` → old fields. The API and UI MUST NOT implement `dataloghistory_undo.php`.

#### Scenario: JSON datalog update shows field changes
- **WHEN** a row has action `u` and JSON data `{"old":{"domain":"a.example"},"new":{"domain":"b.example"}}`
- **THEN** the detail response lists `domain` with old `a.example` and new `b.example`

#### Scenario: PHP-serialized pre-cutover row is readable
- **WHEN** a migrated row's `data` is PHP-serialized `{old,new}`
- **THEN** the detail endpoint returns the same structured field view as for JSON

#### Scenario: Undecodable payload does not 500
- **WHEN** `data` is neither valid JSON nor a recognized PHP serialize payload
- **THEN** the endpoint returns HTTP 200 with raw data and a decode error indicator

#### Scenario: Undo is not offered
- **WHEN** a user opens any datalog history detail in the UI
- **THEN** no undo control is present and no undo endpoint exists

### Requirement: Monitor REST endpoints for logs and datalog
Under `/api/monitor`, the REST API SHALL expose: paginated `sys_log` list; admin-only clear (`loglevel = 0` for one id or batch by level, port of `log_del.php`); jobqueue list + count; datalog history list + detail. Endpoints SHALL use session/token auth, require `monitor` in modules, and carry swaggo annotations. Clear mutations SHALL NOT write `sys_datalog` rows for the loglevel update (PHP does a direct UPDATE).

#### Scenario: Clear warning batch
- **WHEN** an admin POSTs clear with `loglevel = 1`
- **THEN** every `sys_log` row with `loglevel = 1` is updated to `0` and no row is deleted

#### Scenario: Non-monitor user is forbidden
- **WHEN** a valid session without the `monitor` module calls `/api/monitor/datalog`
- **THEN** the API returns 403

### Requirement: Monitor_data read endpoints for the UI
The REST API SHALL expose read endpoints returning the latest (or filtered) `monitor_data` rows by `server_id` / `type` / `state` with dual-decoded `data`, supporting the System state and detail views (and the remote-style "get by type/state" surface called out in the proposal).

#### Scenario: Get latest disk_usage for server
- **WHEN** `GET /api/monitor/data/disk_usage?server_id=1` is called and samples exist
- **THEN** the newest row by `created` is returned with decoded filesystem array and `state`

#### Scenario: Filter by state
- **WHEN** monitor data is listed with `state = error`
- **THEN** only rows whose `state` is `error` are returned
