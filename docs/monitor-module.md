# Monitor module

Port of the ISPConfig3 monitor (`server/mods-available/monitor_core_module.inc.php`,
`monitor_tools.inc.php` and `admin/lib/*/show_sys_state.php`): the daemon samples
the host through scheduler jobs and writes them into `monitor_data`; the panel
reads only the database.

**The API process never touches the operating system.** Every metric reaches the
panel through `monitor_data` rows written by the daemon on the server being
measured. A `serve`-only deployment therefore shows whatever the daemons last
persisted and nothing else — no shelling out, no remote probes from the web tier.

## Collection jobs (daemon)

`monitor.RegisterJobs` mounts the collectors on `engine.Scheduler`
(`internal/monitor/register.go`). Names are prefixed `monitor_` so they are
distinguishable in the scheduler listing.

| Job | Spec | `monitor_data.type` | Source |
|-----|------|---------------------|--------|
| `monitor_cpu_info` | `*/5 * * * *` | `cpu_info` | gopsutil `cpu.Info` |
| `monitor_mem_usage` | `*/5 * * * *` | `mem_usage` | gopsutil `mem.VirtualMemory` |
| `monitor_disk_usage` | `*/5 * * * *` | `disk_usage` | gopsutil `disk.Partitions`/`Usage` |
| `monitor_server_load` | `*/5 * * * *` | `server_load` | gopsutil `load.Avg`, `host.Uptime`, `host.Users` |
| `monitor_services` | `*/5 * * * *` | `services` | TCP/UDP probes + MariaDB connect |
| `monitor_os_info` | `*/5 * * * *` | `os_info` | gopsutil `host.Info` |
| `monitor_kernel_info` | `*/5 * * * *` | `kernel_info` | gopsutil `host.KernelVersion` |
| `monitor_ispc_info` | `*/5 * * * *` | `ispc_info` | build version |
| `monitor_sys_usage` | `* * * * *` | `sys_usage` | rolling load/mem/net series |
| `monitor_quota` | `*/15 * * * *` | `harddisk_quota`, `email_quota`, `database_size` | quota collectors, see below |
| `monitor_sys_log` | `*/5 * * * *` | `sys_log` | severity rollup of open `sys_log` rows |
| `monitor_log_ispconfig` | `*/5 * * * *` | `log_ispconfig` | tail of the ISPConfig log |
| `monitor_log_letsencrypt` | `*/5 * * * *` | `log_letsencrypt` | tail of the Let's Encrypt log |
| `monitor_log_messages` | `*/5 * * * *` | `log_messages` | tail of `/var/log/messages` |
| `monitor_system_update` | `0 * * * *` | `system_update` | `apt-get -s upgrade`, **opt-in** |

`monitor_system_update` is registered only when `RegisterOptions.EnableSystemUpdate`
is set: on non-apt hosts it can only report `no_state`, so it stays off by default
rather than filling the overview with a permanently unknown check.

Log tails are daemon-only file reads capped at `MaxLogLines`, matching the PHP
line budget.

### Quota collectors

`RunQuotaCollectors` (`internal/monitor/quota.go`) is the port of the PHP
`monitor_*_quota` collectors. Each writes one row per type through `UpsertType`
(one current row per server+type — quotas are a snapshot, not a history):

| Type | Source | Unit |
|------|--------|------|
| `harddisk_quota` | `repquota -au` per site `system_user`; hosts without quotas fall back to `du` of the document root, and the site's `hd_quota` (MB) supplies soft/hard when the filesystem has none | KB |
| `email_quota` | `doveadm quota get -A` | bytes |
| `database_size` | one `information_schema` scan joined with the server's `web_database` rows | bytes |

`repquota`, `doveadm` and `du` are daemon-only calls bounded by a 60 s timeout;
a missing binary yields an empty result instead of an error, so a host without
disk quotas still reports usage.

### Write path and pruning

`monitor.Store` inserts one row and then calls `DelOldRecords`, deleting rows of
the same `(server_id, type)` older than **240 s** (`PruneAgeSeconds`) — the same
window as PHP's `delOldRecords`. `monitor_data` is a rolling buffer of the last
few samples, not a history table; long-term trends live inside the `sys_usage`
payload, which keeps at most 15 points.

### State severity

`monitor.SetState` is a port of `monitor_tools::_setState`: promote-only folding
over `no_state < ok < unknown < info < warning < critical < error`. An unrecognised
label normalises to `unknown`. Disk fill uses the PHP bands — 75/80/90/95 % gated
by 2000/1000/500/100 MiB free, so a large mostly-full disk with plenty of absolute
free space does not alarm.

## Dual-format payload decode

`monitor_data.data` and `sys_datalog.data` are decoded JSON-first with a PHP
`serialize()` fallback (`internal/monitor/decode.go`), size-capped, returning the
raw string plus a `decode_error` instead of failing the request.

**Rollback implication:** go-ispconfig always *writes* JSON. If a host is rolled
back to PHP ISPConfig, PHP will not be able to read rows written by the Go daemon
(it expects `serialize()`); the affected checks simply show as stale until the PHP
cron overwrites them, which happens within one 5-minute cycle. Nothing else in the
schema changes, so the rollback is otherwise lossless.

## Permissions

`monitor.HasMonitorModule` gates every `/api/monitor/*` route: admins always pass,
other users need `monitor` in their `sys_user.modules` CSV. Data is further scoped
to the servers the caller may read (`ReadableServers`, standard riud columns on
`server`). `POST /api/monitor/sys-log/clear` is admin-only on top of that.

Clearing a `sys_log` entry never issues a `DELETE`: it sets `loglevel = 0`, the
same "acknowledged" semantics PHP uses, so the audit trail survives.

## REST API

All routes require an authenticated session with the monitor module.

| Route | Purpose |
|-------|---------|
| `GET /api/monitor/state` | aggregated per-server system state (port of `show_sys_state.php`) |
| `GET /api/monitor/data` | monitor rows, `?latest=1` for newest per server+type, filters `server_id`/`type`/`state` |
| `GET /api/monitor/data/{type}` | rows for one check type |
| `GET /api/monitor/sys-log` | paginated `sys_log`, filters `server_id`/`loglevel`/`message`, `tstamp DESC` |
| `POST /api/monitor/sys-log/clear` | admin: acknowledge one id or a whole loglevel |
| `GET /api/monitor/jobqueue` | pending `sys_datalog` (`datalog_id > server.updated`) |
| `GET /api/monitor/jobqueue/count` | pending count, including the `server_id = 0` all-servers rows |
| `GET /api/monitor/datalog` | full datalog history |
| `GET /api/monitor/datalog/{id}` | one datalog entry with its decoded `{old,new}` payload |
| `GET /api/system/scheduler` | admin: job name, cron spec, last run, status, computed next run |
| `GET /api/monitor/limits` | the caller's own account limits (sites, mailboxes, databases, DNS zones…) with current usage — **outside** the module guard: it is a dashboard dashlet every logged-in user gets, port of the legacy `dashlet_limits` |

The datalog detail view is **read-only** — there is no undo. `sys_datalog` is the
replication journal; rewriting it would desynchronise servers that already
consumed the entry.

`engine.Scheduler.Register` mirrors `{name}_spec` into `sys_config` group
`scheduler` alongside the `{name}_last_run` / `{name}_status` keys the daemon
writes after each run, which is how a standalone `serve` process can list job
metadata and derive `next_run` without talking to the daemon.

## Panel UI

**Monitor** module in the topbar, sections:

- *System state* — one card per server with the folded state, OS/ISPConfig
  version and the disk/load/services/updates/sys_log messages.
- *Check details* — the raw payload of one check type per server.
- *System log* — `sys_log` table with filters and the admin clear actions.
- *Job queue* — pending datalog entries per server.
- *Datalog history* — every entry with an old/new field diff in the detail view.
- *Scheduler jobs* (admin) — name, spec, last run, next run, status.

### Dashboard dashlets

Rendered in the legacy order — modules, then metrics, then quotas:

1. *Modules* — shortcut tiles, plus the monitor dashlet with server states and
   the pending job-queue depth.
2. *Metrics* — `MetricChart.vue` (Chart.js via `vue-chartjs`) plotting the
   `sys_usage` series (load, memory, network) with the panel's own palette in
   both light and dark mode.
3. *Quotas* — `QuotaBlock.vue` once per quota type (disk, mailbox, database),
   fed by the `harddisk_quota` / `email_quota` / `database_size` rows and
   hidden when the caller owns none.
4. *Account limits* — `LimitBlock.vue` from `GET /api/monitor/limits`
   (used/limit per resource, port of `dashboard/dashlets/limits.php`). Limits
   of `0` are omitted, `-1` renders as unlimited, and admins — who have no
   client row — get `{"unlimited": true}` and a one-line dashlet.
