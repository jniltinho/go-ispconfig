# Proposal: Monitor Module (server metrics, logs, datalog history)

> Roadmap phase 2 — proposal only for now; design/specs/tasks will be authored when the module is scheduled. Depends on the foundation change `port-ispconfig3-to-go` (daemon scheduler, sys_datalog, panel skeleton).

## Why

Admins need to see, from the panel, whether the server is healthy: CPU/memory/disk, service status, system logs, and what the datalog engine has been doing. ISPConfig3 ships this as `monitor_core_module` plus a set of monitor cron jobs and the Monitor interface module; go-ispconfig ports the useful subset on top of its internal scheduler.

## What Changes

- **Metric collection jobs** on the daemon's internal scheduler (replacing ISPConfig3's `server/lib/classes/cron.d/100-monitor_*.inc.php` jobs): CPU, memory, disk usage, system load/uptime, service status (nginx, MariaDB, Bind, SSH, PureFTPd…), OS/kernel version, and mail-queue-style basics where applicable. Collection uses **gopsutil** (the pattern already proven in jniltinho's pf-report) instead of parsing `/proc` and shelling out.
- **monitor_data storage**: results stored in the existing `monitor_data` table (server_id, type, created, data, state ok/info/warning/critical/error) as JSON instead of PHP serialize; old entries pruned like upstream. Readers are **dual-format** (PHP `serialize()` and JSON) so pre-cutover history in `monitor_data` — and pre-cutover `sys_datalog` rows in the viewer — remain readable; all new writes are JSON and Go-only. Rollback implication: the PHP panel cannot read this JSON history, so monitor data written after cutover is lost to a PHP rollback. `server/mods-available/monitor_core_module.inc.php` is an empty shell in PHP (collection lives in cron.d) — in Go the module is the set of scheduler jobs plus the read API.
- **System State UI**: Monitor module in the Vue panel showing the server state overview (port of `interface/web/monitor/show_sys_state.php` and `show_data.php` views): per-check cards with state colors, service up/down list, disk/memory/CPU details.
- **Log viewers**: sys_log viewer (`log_list.php`, filter by server/state, delete/clear) and read-only tail views of key logs (ispconfig log, letsencrypt log — the `show_log.php` subset that maps to our stack).
- **Datalog history viewer**: port of `interface/web/monitor/dataloghistory_*.php` and `datalog_list.php` — browse `sys_datalog` entries (who changed what, old/new diff rendered from JSON, with dual serialize-PHP/JSON decoding for pre-cutover rows), plus the pending-jobs list (datalog entries not yet processed by the daemon).
- **Scheduler job status**: UI listing the daemon's internal scheduler jobs (name, last run, next run, last result) — the go-ispconfig replacement for ISPConfig's cron transparency; exposed by the daemon via the API.
- **Dashboard dashlets**: small widgets on the existing dashboard (server state summary, failed jobs, last datalog activity), mirroring ISPConfig3's monitor dashlets.
- **REST API**: read endpoints mirroring `remote.d/monitor` functions (monitor_data get by type/state, sys_log get, datalog get), Swagger-documented; write access limited to delete/clear log (admin).

## Capabilities

### New Capabilities

- `monitor-collection`: scheduler-driven metric/service collection via gopsutil writing `monitor_data` (JSON, pruned).
- `monitor-ui`: Monitor panel module — system state overview, log viewers, dashboard dashlets.
- `datalog-history`: sys_datalog browser with old/new diff view and pending-jobs listing.
- `scheduler-status`: API + UI exposing internal scheduler job status.

### Modified Capabilities

(none — reads existing sys_datalog/sys_log; adds scheduler jobs through the foundation's scheduler registration, whose requirements are unchanged)

## Impact

- Reference PHP sources: `server/mods-available/monitor_core_module.inc.php`, `server/lib/classes/cron.d/100-monitor_*.inc.php`, `server/lib/classes/monitor_tools.inc.php`, `interface/web/monitor/` (`show_sys_state.php`, `show_data.php`, `log_list.php`, `datalog_list.php`, `dataloghistory_*.php`), `remote.d/monitor` functions.
- Tables: `monitor_data`, `sys_log`, `sys_datalog` (read), `server`.
- New dependency: `shirou/gopsutil/v4`.
- Daemon: new scheduled jobs registered on the internal scheduler (interval per check, as upstream's 5-minute cron class).

## Non-goals

- External alerting (email/webhook notifications on state changes) — later phase.
- RRD/Munin/Monit embedding (`show_munin.php`, `show_monit.php`) and long-term historical graphs — later phase; `monitor_data` retention stays short.
- Multi-server monitoring aggregation (single-server first, like the foundation).
- Checks with no counterpart in our stack: OpenVZ, MongoDB, ClamAV/mail-queue (until the mail module lands), raid/rkhunter/fail2ban checks may be stubbed or deferred to design.
- `dataloghistory_undo.php` (undo a datalog change) — risky, out of scope.
- `system_update` is implemented but **opt-in** (`RegisterOptions.EnableSystemUpdate`): it only reads `apt-get -s upgrade`, so on non-apt hosts it can report nothing but `no_state` and stays unregistered by default.
