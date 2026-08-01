# Proposal: add-cron-module

> Roadmap phase 2 — proposal only. Design/specs/tasks will be written when this module is scheduled.

## Why

Hosting clients need scheduled jobs tied to their sites. This change ports the ISPConfig3 cron module so go-ispconfig can manage client cron jobs — but executed by the daemon's internal scheduler (foundation decision D1b: no system crontab), with execution logging the PHP original never had.

Reference PHP sources being ported (read-only under `base/ispconfig3_install/`):
- `server/mods-available/cron_module.inc.php` — table hook for `cron` → named events (`cron_insert/update/delete`)
- `server/plugins-available/cron_plugin.inc.php` — job assembly per type (full/chrooted/url), `_write_crontab()` writing per-user crontab files (replaced here by the internal scheduler)
- `interface/web/sites/` (`cron_edit.php`, `cron_list.php` + `form/cron.tform.php`) — panel UI, schedule field validation
- `interface/lib/classes/remote.d/sites.inc.php` — remote API surface (`sites_cron_add/update/delete/get`)

## What Changes

- **cron module (daemon side)**: Go `Module` registering the `cron` table hook and raising `cron_insert/update/delete` events.
- **cron plugin → internal scheduler**: instead of writing crontab files (`_write_crontab()`), the plugin registers/updates/removes jobs on the go-ispconfig daemon's internal scheduler (from the foundation). Cron expressions (`run_min/hour/mday/month/wday` fields) are parsed and evaluated in-process.
- **Job types** (port of `cron_plugin.inc.php` semantics):
  - `url`: HTTP GET of the configured URL with timeout.
  - `full`: command executed as the site's system user (`web_domain.system_user`), working dir at the document root.
  - `chrooted`: executed as the site user with the same restrictions; actual jailkit chroot deferred (see Non-goals).
- **Fail-safe privilege drop for `full`/`chrooted` jobs**: commands run via direct exec (argv slices, no shell interpolation) with the site's uid/gid, `NoNewPrivileges`, and an enforced timeout with kill; if the privilege drop fails for any reason the job is aborted and logged — a job is never executed as root.
- **Execution log**: per-job run history (start/end, exit status, output tail) — stored in `sys_log` via a field convention and viewable in the panel; honors `log` flag on the `cron` row.
- **Cutover step**: lists and removes the legacy per-user OS crontabs written by `cron_plugin.inc.php` (or imports still-active jobs into the internal scheduler), so no job ever runs twice after cutover.
- **Per-client limits**: enforce `client.limit_cron`, `limit_cron_type` and `limit_cron_frequency` in the API layer.
- **REST API**: port of the `sites_cron_*` surface with swaggo annotations, riud scopes and datalog writes.
- **UI (Vue 3)**: Sites → Cron — job list/form (schedule fields, type, command/URL, active, log toggle) plus a run-history view.
- **Testing**: unit tests for schedule parsing/matching, integration tests for the datalog→scheduler registration pipeline.

## Capabilities

### New Capabilities

- `cron-module-events`: daemon cron module — `cron` table hook and named event dispatch.
- `cron-scheduler-execution`: client cron jobs run by the daemon's internal scheduler — job types full/chrooted/url, execution as the site user, run logging (replaces crontab file writing from `cron_plugin.inc.php`).
- `cron-rest-api`: REST endpoints porting the `sites_cron_*` functions with swagger docs and client limit enforcement.
- `cron-panel-ui`: Vue Sites → Cron UI — job CRUD and execution history.

### Modified Capabilities

(none — foundation capabilities are consumed, not changed; jobs plug into the existing internal scheduler)

## Impact

- **Depends on `port-ispconfig3-to-go`** (datalog registries, internal scheduler, rest-api-core, auth-permissions, panel-skeleton) **and on `add-web-nginx-module`** (Sites panel module, `web_domain` parent, site system users).
- New Go packages: `internal/modules/cron` (module + plugin/scheduler bridge), REST handlers, Vue additions to the `sites` module.
- DB: uses the existing `cron` table; the execution log reuses `sys_log` with a field convention — the embedded `ispconfig3.sql` schema is inviolable, so no new table; any future table need would be versioned additive DDL, never a change to the embedded schema.
- No system crontab entries are ever written (deliberate divergence from `cron_plugin.inc.php`).

## Non-goals

- Jailkit chroot for cron jobs (`cron_jailkit_plugin.inc.php`) — deferred to the ftp-shell/jailkit change; `chrooted` type runs as the site user without a jail until then.
- System/maintenance cron jobs of the panel itself (already covered by the foundation scheduler).
- Per-user crontab file generation or vixie-cron/systemd-timer integration.
- Translations beyond English.
