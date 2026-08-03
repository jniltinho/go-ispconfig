# Design: Monitor module (server metrics, logs, datalog history)

## Context

ISPConfig3's monitor stack is three pieces with almost no datalog write path of its own:

1. `server/lib/classes/cron.d/100-monitor_*.inc.php` — ~25 cron jobs on a `*/5 * * * *` (or `* * * * *` / hourly) schedule that collect metrics, service status and log tails, then `REPLACE INTO monitor_data (server_id, type, created, data, state)` with PHP `serialize($data)` and prune via `monitor_tools::delOldRecords` (delete rows older than 240 seconds for that `type` + `server_id`).
2. `server/lib/classes/monitor_tools.inc.php` — shared helpers: `monitorServices()` (TCP/FTP/UDP/MySQL probes gated by `server.web_server` / `mail_server` / `dns_server` / `file_server` / `db_server`), `_getLogData` (tail of distro log paths), `_setState` (monotonic severity), `delOldRecords`.
3. `interface/web/monitor/` — read-only views over `monitor_data`, `sys_log` and `sys_datalog` (`show_sys_state.php`, `show_data.php`, `show_log.php`, `log_list.php` + `log_del.php`, `datalog_list.php` jobqueue, `dataloghistory_*.php`). Module permission is `monitor` (`check_module_permissions('monitor')`). `server/mods-available/monitor_core_module.inc.php` is an empty shell (`onInstall` returns false; no table hooks).
4. `remote.d/monitor.inc.php` exposes only `monitor_jobqueue_count` (count of `sys_datalog` rows with `datalog_id > server.updated` for the target server(s)).

The foundation already provides: internal daemon scheduler with `sys_config` group `scheduler` mirrors (`_last_run` / `_status`), GORM models for `sys_datalog` / `sys_log` / `server`, `GET /api/system/scheduler` (admin), panel skeleton, and the rule that the interface never touches the OS. The `monitor_data` table exists byte-identical in `internal/database/ispconfig3.sql`; only a GORM model and the collection/read surfaces are missing.

## Goals / Non-Goals

**Goals:**
- Behavior-faithful subset of ISPConfig3 monitor: collect the in-scope check types into `monitor_data`, expose system-state overview, detail views, sys_log + key log tails, datalog history with old/new diff, pending jobqueue, scheduler job status, and dashboard dashlets.
- Dual-format readers (PHP `serialize` + JSON) so pre-cutover `monitor_data` and `sys_datalog.data` remain readable; all new `monitor_data` writes are JSON.
- Collection via gopsutil (and small port probes) on the daemon scheduler — never from the API process.
- Module-permission and admin write rules aligned with the PHP panel; no schema changes.

**Non-Goals:**
- External alerting, RRD/Munin/Monit embedding, multi-server aggregation, long-term graphs (see proposal).
- Checks with no counterpart in the current stack (OpenVZ, MongoDB, ClamAV/mail-queue until mail lands); raid / rkhunter / fail2ban / iptables may be stubbed or deferred (proposal).
- `dataloghistory_undo.php` (undo a datalog change).
- No schema changes of any kind.

## Decisions

### D1 — Collection is scheduler jobs, not a datalog Module/Plugin
Port the empty-shell nature of `monitor_core_module.inc.php`: there are no table hooks and no named events. Metric collection registers named jobs on the foundation scheduler (`engine.Scheduler.Register`) with cron specs matching upstream (`*/5 * * * *` for most checks, `* * * * *` for `sys_usage`, `0 * * * *` for `system_update` if implemented). Each job writes `monitor_data` and prunes. The API and UI only read (except log clear).
*Alternative*: fake a monitor plugin on a synthetic table — rejected: invents architecture PHP does not have and couples reads to the datalog consumer.

### D2 — gopsutil instead of `/proc` + shell-outs
Collection uses `shirou/gopsutil/v4` (CPU info, load, memory, disk partitions/usage, host uptime/OS/kernel, process existence where useful) — the pattern already proven in jniltinho's pf-report. Service liveness still uses TCP/UDP/FTP/MySQL probes (port of `monitor_tools::_checkTcp` / `_checkUdp` / `_checkFtp` and mysqli ping), not gopsutil alone, so state semantics stay binary ok/error like PHP. Log tails remain file reads of known paths (ispconfig log dir, Let's Encrypt/acme logs, syslog) performed **only by the daemon**, then stored in `monitor_data` — the serve process never opens those files (interface-never-touches-OS rule).

### D3 — `monitor_data` payload: JSON write, dual-format read
Schema (unchanged):

| Column | Type | Notes |
|---|---|---|
| `server_id` | int unsigned | PK part |
| `type` | varchar(255) | PK part — check id (`cpu_info`, `disk_usage`, …) |
| `created` | int unsigned | PK part — unix seconds |
| `data` | mediumtext | payload |
| `state` | enum(`no_state`,`unknown`,`ok`,`info`,`warning`,`critical`,`error`) | default `unknown` |

New writes store `data` as JSON (object or array matching the PHP structure for that type). Readers (`DecodeMonitorData`, datalog history decoder) try JSON first; on failure attempt PHP `serialize` decode so migrated history remains usable. Rollback implication (proposal): PHP panel cannot read post-cutover JSON `monitor_data`.
Primary write path mirrors PHP: insert new `(server_id, type, created=now, data, state)` then `DELETE FROM monitor_data WHERE type=? AND created < UNIX_TIMESTAMP()-240 AND server_id=?` (`delOldRecords` parity). `sys_usage` keeps its time-series **inside** the JSON payload (rolling arrays of up to 15 points for load/mem/net/time) and still prunes old rows the same way.

### D4 — In-scope `type` values (first ship)
Implemented collection types and their PHP sources:

| `type` | Source cron | State | Notes |
|---|---|---|---|
| `cpu_info` | `100-monitor_cpu` | `no_state` | CPU model/cores via gopsutil |
| `mem_usage` | `100-monitor_mem_usage` | `no_state` | MemTotal/MemAvailable/… bytes |
| `disk_usage` | `100-monitor_disk_usage` | ok→error by fill | partitions; skip iso9660/tmpfs/…; thresholds 75/80/90/95% with free-size gates (2000/1000/500/100 MiB) |
| `server_load` | `100-monitor_server` | ok→error by load_1 | load_1/5/15, uptime, users; thresholds load_1 >20/50/100/150 |
| `services` | `100-monitor_services` + `monitorServices` | `ok` or `error` | web/ftp/smtp/pop3/imap/bind/mysql flags from `server.*_server`; stack-relevant probes only (nginx :80, PureFTPd :21 when file_server, Bind :53/udp when dns_server, MariaDB when db_server); mail ports only when `mail_server=1` (else -1 unused) |
| `os_info` | `100-monitor_os_version` | `no_state` | distro name/version |
| `kernel_info` | `100-monitor_kernel_version` | `no_state` | uname |
| `ispc_info` | `100-monitor_ispconfig_version` | `no_state` | go-ispconfig version string |
| `sys_usage` | `100-monitor_sys_usage` (`* * * * *`) | `no_state` | rolling load%/mem%/net rx-tx series for dashlets |
| `sys_log` | `100-monitor_syslog` | ok/warning/error | derived from open `sys_log.loglevel` > 0 for this server (no payload body) |
| `log_ispconfig` | `100-monitor_ispconfig_log` | `ok` | tail of panel/daemon log |
| `log_letsencrypt` | `100-monitor_letsencrypt_log` | `no_state` | tail of acme/letsencrypt log |
| `log_messages` | part of `100-monitor_syslog` | `no_state` | tail of `/var/log/syslog` (Debian/Ubuntu) |
| `system_update` | `100-monitor_system_update` (hourly) | ok/info/`no_state` | apt update simulation summary when available; `no_state` if unsupported |

Deferred / stub (visible as empty or `no_state` if queried): `raid_state`, `rkhunter`, `log_fail2ban`, `iptables_rules`, `mailq`, `log_mail*`, `log_clamav`, `log_freshclam`, `openvz_*`, `database_size` (needs database module), `email_quota`, `harddisk_quota`, `backup_utils`. Do not invent collection for them in this change.

### D5 — State aggregation for System State UI
Port `_getServerState` / `_processDbState` from `show_sys_state.php`: for a server, take the **newest** row per relevant `type` and fold states with `_setState` weights (`no_state=0 < ok=1 < unknown=2 < info=3 < warning=4 < critical=5 < error=6`). Types that contribute messages: `disk_usage`, `server_load`, `services`, `system_update`, `sys_log` (and raid when present). `cpu_info` / `mem_usage` / pure info types do not raise severity. System overview lists every `server` row the caller may read (riud on `server`), each with aggregated state + per-check summary cards linking to detail by `type`.

### D6 — REST surface (read-heavy, admin clear)
New routes under `/api/monitor/*` (session/token auth, module permission `monitor` on the caller's `sys_user.modules` list; admin required for mutations). Style matches `internal/api/sites.go` / `dns.go` (Echo handlers, swaggo, JSON errors).

| Method | Path | Purpose |
|---|---|---|
| GET | `/api/monitor/state` | System overview (all accessible servers) or `?server_id=` single-server detail |
| GET | `/api/monitor/data` | Latest row(s) by `server_id` + optional `type` / `state` filter; decoded `data` |
| GET | `/api/monitor/data/{type}` | Latest detail for one type (`?server_id=`) |
| GET | `/api/monitor/sys-log` | Paginated `sys_log` (filters: server_id, loglevel, message, tstamp range) |
| POST | `/api/monitor/sys-log/clear` | Port of `log_del.php`: set `loglevel=0` for one `syslog_id` or batch by loglevel — **admin only** (does not DELETE rows) |
| GET | `/api/monitor/jobqueue` | Pending datalog rows (`datalog_id > server.updated` AND `server_id IN (0, server_id)`), filters like `datalog.list.php` |
| GET | `/api/monitor/jobqueue/count` | Port of `monitor_jobqueue_count` |
| GET | `/api/monitor/datalog` | Full history list (`dataloghistory_list.php` filters: tstamp, server_id, action i/u/d, dbtable, user) |
| GET | `/api/monitor/datalog/{id}` | Single entry with dual-decoded `{old,new}` field diff (port of `dataloghistory_view.php`; no undo) |

Scheduler listing stays on the existing `GET /api/system/scheduler` (foundation); this change extends the response with cron `spec` and computed `next_run` from registered jobs (see D9) so the Monitor UI can show a complete status table.

### D7 — Dual decode for datalog history
`sys_datalog.data` post-cutover is JSON `{"old":{...},"new":{...}}` (foundation). Pre-cutover rows may be PHP-serialized. The history detail endpoint MUST:

1. Try `json.Unmarshal` into `{old,new}` maps.
2. Else PHP-unserialize into the same shape when possible.
3. Else return `data_raw` + `decode_error` so the UI still shows something (foundation design note: pre-cutover history readable).

Field-level diff for updates: keys union of old/new; multi-line values may use a simple line diff in the UI (FineDiff parity is nice-to-have, not required byte-identical). Actions: `i` show new fields, `u` show changed pairs, `d` show old fields — same as `dataloghistory_view.php`.

### D8 — Permissions model
`monitor_data` and `sys_log` have **no** `sys_userid` / `sys_perm_*` columns. Access control is:

- **Module gate**: caller's `sys_user.modules` CSV must include `monitor` (seeded on admin: `dashboard,admin,client,mail,monitor,sites,dns,...`).
- **Server scope**: list/filter by `server` rows visible under the foundation riud scope on table `server` (admin sees all; non-admin only servers their group may read). Single-server first deployment still applies the same rules.
- **Mutations**: only `sys-log` clear (and any future clear of jobqueue entries if exposed) — **admin** (`sys_user.typ = admin`). Clients with the monitor module are read-only.
- Datalog history is readable under the monitor module gate; it is not filtered by the entity-level riud of the changed row (PHP list auth is `'no'` after module check) — document this parity explicitly.

### D9 — Scheduler status (extend foundation mirror)
Foundation already persists `{name}_last_run` / `{name}_status` in `sys_config` and exposes them on `GET /api/system/scheduler`. This change:

- Registers all monitor collection jobs with stable names (`monitor_cpu_info`, `monitor_mem_usage`, `monitor_disk_usage`, `monitor_server_load`, `monitor_services`, `monitor_os_info`, `monitor_kernel_info`, `monitor_ispc_info`, `monitor_sys_usage`, `monitor_sys_log`, `monitor_log_ispconfig`, `monitor_log_letsencrypt`, `monitor_log_messages`, optionally `monitor_system_update`).
- Extends the API DTO to include `spec` (cron expression) and `next_run` (RFC3339, computed from last run + spec or from cron next from "now" when last run empty), using the in-process job registry when the serve and daemon share code paths, or by encoding `spec` into an additional `sys_config` key `{name}_spec` written at daemon start so a standalone `serve` process can still list full metadata without talking to the daemon process (preferred: write `_spec` at registration/run time — interface never talks to daemon).
- Monitor UI "Scheduler jobs" page consumes this endpoint (admin).

### D10 — UI shape (Vue)
Module id `monitor` in `frontend/src/modules.ts` (icon already `Activity` / `module.monitor` i18n key exists):

- **System state** (start page) — port of `show_sys_state.php?state=system|server`: per-server cards with state color, counts of info/warning/critical/error messages, links into detail types.
- **Server selector** — when multiple servers exist; default first accessible.
- **Check detail** — port of `show_data.php?type=…`: structured tables for disk/mem/cpu/services/load; preformatted tail for log types.
- **ISPConfig log** — DataTable over `sys_log` with filters (server, loglevel Debug/Warning/Error, message); row clear + batch clear by level (admin).
- **Jobqueue** — DataTable of pending `sys_datalog` rows (tstamp, server, action, dbtable).
- **Data log history** — DataTable + detail drawer/page with decoded old/new diff; **no undo button**.
- **Scheduler jobs** — admin table (name, spec, last run, next run, last result).
- **Dashboard dashlets** — small widgets on the existing dashboard: server state summary, failed scheduler jobs, last datalog activity / jobqueue count (mirroring ISPConfig monitor-related dashlets / metrics).

All strings through `en.json`. Reuse `DataTable` for lists; cards for state overview (not TabbedForm — monitor is not a CRUD tform module).

### D11 — Package layout
- `internal/model`: add `MonitorData` for table `monitor_data` (explicit column tags); `SysLog` / `SysDatalog` / `Server` already exist.
- `internal/monitor`: collection jobs, state aggregation helpers, dual-format decode, prune, service probes (daemon-side).
- `internal/api`: `monitor.go` (+ swaggo doc file) handlers; wire routes next to existing system/sites/dns.
- Frontend: `frontend/src/views/monitor/*`, store, routes, module sections.

No changes to `ispconfig3.sql`. Dependency: `github.com/shirou/gopsutil/v4` plus a small PHP-serialize decoder for dual read (existing pure-Go library acceptable; pin in go.mod at implement time).

## Risks / Trade-offs

- [gopsutil field names differ from PHP `/proc` keys → UI/detail drift] → map collected metrics into the same JSON keys PHP used where the UI expects them (`MemTotal`, `load_1`, disk `fs`/`percent`/`mounted`, services `webserver`/`mysqlserver`/… as 1/0/-1); document any intentional renames in module docs.
- [Dual-format decode of hostile PHP serialize] → decode only server-side, size-cap payloads (mediumtext), never `eval`; quarantine/log decode failures without 500ing the list endpoint.
- [Log clear is UPDATE not DELETE] → easy to mis-implement as hard delete; tests must assert row remains with `loglevel=0`.
- [serve vs daemon split: scheduler next_run unknown to API] → persist `_spec` (and optionally precomputed next) in `sys_config` from the daemon (D9).
- [PHP cannot read JSON monitor_data after cutover] → accepted proposal rollback trade-off; document in migration notes.
- [Prune window 240s assumes job interval ≥ 5 min] → keep upstream constant; `sys_usage` (1 min) still safe because series lives inside one payload.
- [Service checks for mail when mail module absent] → only probe when `server.mail_server=1`; otherwise store -1 and do not raise error (PHP parity).

## Migration Plan

- Code-only: no schema migration. Existing `monitor_data` rows from PHP remain until pruned or dual-read.
- Fresh install: after daemon starts, first scheduler ticks fill `monitor_data`; panel shows unknown/empty until then.
- Cutover from PHP: drain datalog (foundation), stop PHP cron/server, start go-ispconfig daemon; old monitor rows readable via dual decoder until 240s prune replaces them with JSON.
- Rollback to PHP: post-cutover JSON `monitor_data` is opaque to PHP until new PHP cron cycles rewrite it; document loss of the JSON window.
- Enablement: gate collection jobs on config / always-on when daemon runs (single-server foundation); no new `server` flags.

## Open Questions

- Should `database_size` collection land as a thin stub that no-ops until `add-database-module`, or be omitted entirely from the type registry until then? (Recommendation: omit from registered jobs; API may still return 404/empty for unknown types.)
- SSH check is mentioned in the proposal narrative but not in PHP `monitorServices`; add as an extra probe on port 22 always, or stay strict PHP parity? (Recommendation: strict PHP parity for `services` keys; SSH can be a later additive field with its own key if desired.)
- Exact log path for go-ispconfig's own log (`log_ispconfig`) vs PHP `ispconfig_log_dir` — align with installer paths when `add-installer-cli` finalizes layout.
