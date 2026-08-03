# Tasks: add-monitor-module

## 1. Models and dual-format decode

- [x] 1.1 Add GORM model `MonitorData` for table `monitor_data` with explicit `gorm:"column:..."` tags matching `ispconfig3.sql` (`server_id`, `type`, `created`, `data`, `state`); composite primary key; unit-test round-trip against MariaDB. Commit.
- [x] 1.2 Implement dual-format decoder for `monitor_data.data` and `sys_datalog.data`: JSON first, then PHP `serialize`; size-capped; safe failure returns raw + error; table-driven tests with fixtures from PHP serialize and foundation JSON `{old,new}`. Commit.
- [x] 1.3 Implement `_setState` severity fold helper (`no_state` < `ok` < `unknown` < `info` < `warning` < `critical` < `error`) with unit tests for promote-only behavior. Commit.

## 2. Collection jobs (daemon)

- [x] 2.1 Add dependency `github.com/shirou/gopsutil/v4` and package `internal/monitor` with shared write helper: insert `monitor_data` as JSON + `delOldRecords` prune (240s, scoped by `server_id`+`type`); tests with sqlite/MariaDB. Commit.
- [x] 2.2 Implement collectors for `cpu_info`, `mem_usage`, `server_load`, `os_info`, `kernel_info`, `ispc_info` (gopsutil/host) with PHP-compatible JSON keys and `no_state` (or load thresholds for `server_load`); unit tests with injectable host stubs where practical. Commit.
- [x] 2.3 Implement `disk_usage` collector: partitions via gopsutil, skip iso9660/cramfs/udf/tmpfs/devtmpfs/udev, apply 75/80/90/95% + free-size thresholds; tests for each severity band. Commit.
- [x] 2.4 Implement `services` collector porting `monitorServices`: probe web :80, ftp :21, smtp :25, pop3 :110, imap :143, bind :53/udp, MariaDB connect; gate on `server.web_server`/`file_server`/`mail_server`/`dns_server`/`db_server`; values 1/0/-1; state ok/error only; tests with fake listeners. Commit.
- [x] 2.5 Implement `sys_usage` minute job: rolling load%/mem%/net series max 15 points inside JSON payload; tests for cap and interval math. Commit.
- [x] 2.6 Implement log-tail collectors `log_ispconfig`, `log_letsencrypt`, `log_messages` (daemon-only file read, max lines parity with PHP) and `sys_log` state rollup from open `sys_log.loglevel` rows; tests with temp log files. Commit.
- [x] 2.7 Optionally implement `system_update` hourly job (apt summary or `no_state` when unsupported); if deferred, document omission and skip registration. Commit.
- [x] 2.8 Register all monitor jobs on `engine.Scheduler` with stable names (`monitor_*`) and cron specs (`*/5 * * * *`, `* * * * *` for sys_usage, hourly for system_update); persist `{name}_spec` into `sys_config` group `scheduler` at registration/run; wire in daemon bootstrap; integration test one job end-to-end writes `monitor_data` + status mirror. Commit.

## 3. State aggregation and repositories

- [x] 3.1 Implement server/system state aggregation porting `show_sys_state.php` (`_getServerState` / `_processDbState`): newest row per type, fold states, build message groups for disk/load/services/updates/sys_log; unit tests with fixture rows. Commit.
- [x] 3.2 Implement monitor read repositories: latest-by-type, list with type/state/server filters, server-scoped by caller's readable `server` rows; permission helpers for `monitor` module membership. Commit.
- [x] 3.3 Implement sys_log list (filters server_id/loglevel/message, order tstamp DESC) and clear (single id + batch by loglevel → `loglevel=0`, admin-only, no DELETE); tests. Commit.
- [x] 3.4 Implement jobqueue query (`datalog_id > server.updated` per server), count (incl. `server_id=0` all-servers), and full datalog history list/detail with dual decode; tests for pending vs history and decode paths. Commit.

## 4. REST API (swaggo)

- [x] 4.1 Add monitor routes: `GET /api/monitor/state`, `GET /api/monitor/data`, `GET /api/monitor/data/{type}` with module gate + server scope; handler tests. Commit.
- [x] 4.2 Add `GET /api/monitor/sys-log` and admin `POST /api/monitor/sys-log/clear`; tests for 403 non-admin and loglevel-zero semantics. Commit.
- [ ] 4.3 Add `GET /api/monitor/jobqueue`, `GET /api/monitor/jobqueue/count`, `GET /api/monitor/datalog`, `GET /api/monitor/datalog/{id}`; tests for filters and dual decode. Commit.
- [ ] 4.4 Extend `GET /api/system/scheduler` DTO with `spec` and `next_run` (admin-only preserved); compute next_run from stored spec; tests. Commit.
- [ ] 4.5 Swaggo annotations for all new/changed endpoints; run `make swagger`; verify Swagger UI lists Monitor + updated scheduler schemas; CI staleness check green. Commit.

## 5. Panel UI (Vue + i18n)

- [ ] 5.1 Register Monitor module in `frontend/src/modules.ts` (sections: system state, key check details, ISPConfig log, jobqueue, datalog history, scheduler admin-only); router entries; English keys in `en.json`. Commit.
- [ ] 5.2 System state overview view (all servers + per-server cards, state colors, messages, link to details); optional refresh control. Commit.
- [ ] 5.3 Check detail views for metrics (cpu/mem/disk/load/services/os/kernel/ispc) and log tails (ispconfig/letsencrypt/messages) driven by monitor data API. Commit.
- [ ] 5.4 sys_log DataTable with filters; admin clear single + batch; wire store. Commit.
- [ ] 5.5 Jobqueue DataTable and datalog history DataTable + detail view with old/new field diff (no undo). Commit.
- [ ] 5.6 Scheduler jobs admin table (name, spec, last run, next run, status). Commit.
- [ ] 5.7 Dashboard dashlets: server state summary, failed scheduler jobs, jobqueue/last datalog activity. Commit.

## 6. E2E tests (agent-browser)

- [ ] 6.1 Seed helpers/fixtures for sample `monitor_data`, `sys_log`, pending + historical `sys_datalog`, scheduler `sys_config` keys. Commit.
- [ ] 6.2 agent-browser E2E: admin login → system state → one metric detail → sys_log → jobqueue → datalog history detail → scheduler jobs; screenshots to `docs/prints/`. Commit.

## 7. Docs

- [ ] 7.1 Write `docs/monitor-module.md`: architecture (scheduler jobs, no SO access from API), table/type catalog, dual-format note and PHP rollback implication, prune window, permissions, API overview, dashlets; link from README/ROADMAP if needed. Commit.
- [ ] 7.2 Mark this change's completed tasks in `tasks.md` as work lands; keep proposal Why/Non-goals in sync if any intentional deferral (e.g. `system_update`) is finalized. Commit.
