# Design: Cron module

## Context

ISPConfig3's client cron stack is three pieces glued by `sys_datalog`:

1. `interface/web/sites/` (`cron_edit.php`, `cron_list.php`, `form/cron.tform.php`) plus `validate_cron.inc.php` — panel form writing the `cron` table with `{old,new}` datalog diffs; schedule-field validation and client limits (`limit_cron`, `limit_cron_type`, `limit_cron_frequency`); `remote.d/sites.inc.php` exposes `sites_cron_add/update/delete/get`.
2. `server/mods-available/cron_module.inc.php` — registers the `cron` table hook and raises three named events (`cron_insert` / `cron_update` / `cron_delete`).
3. `server/plugins-available/cron_plugin.inc.php` — on any event rewrites the per-site OS crontab files under `crontab_dir` (`ispc_<system_user>` and `ispc_chrooted_<system_user>`), assembling lines from all active jobs of that parent domain for the three types (`url` / `full` / `chrooted`).

The foundation change already provides everything this module plugs into: datalog consumer with table-hook/event registries, daemon internal scheduler (design D1b — no system crontab for panel maintenance), asynq worker, riud GORM scopes, validation engine, REST core (`RegisterEntity`), panel skeleton with Sites module, and the `client.limit_cron*` columns already on the GORM `Client` model. The `cron` table exists byte-identical in `internal/database/ispconfig3.sql`; only the GORM model and the module/plugin/API/UI are missing.

**Deliberate divergence from PHP:** go-ispconfig never writes OS crontab files. Client jobs run inside the daemon (in-process schedule evaluation + privileged-drop exec / HTTP GET). Legacy `ispc_*` files left by a PHP install are removed once at cutover so no job runs twice.

## Goals / Non-Goals

**Goals:**
- Behavior-faithful port of `cron_module` + the job-assembly / type semantics of `cron_plugin`, with the execution backend replaced by the daemon-side client-job runner.
- API/UI parity with the ISPConfig Sites → Cron form: schedule fields, type, command/URL, log, active; parent site linkage; client limit enforcement.
- Fail-safe privilege drop for `full`/`chrooted` jobs (never run as root).
- Per-job run history (start/end, exit status, output tail) stored in existing `sys_log` via a field convention — a capability the PHP original never had.
- Cutover that strips legacy per-user OS crontabs written by PHP.

**Non-Goals:**
- Jailkit chroot for cron jobs (`cron_jailkit_plugin.inc.php`) — deferred to `add-ftp-shell-module`; `chrooted` type runs as the site user without a jail until then.
- System/maintenance jobs of the panel itself (already covered by `engine.Scheduler`: datalog prune, LE renew, DNS re-sign).
- Per-user crontab file generation, vixie-cron or systemd-timer integration.
- Schema changes of any kind (no new tables; no column renames).
- Translations beyond English.

## Decisions

### D1 — One Go package, two registrations (module + plugin)
`internal/cron` contains both the `Module` (table hook → events, port of `cron_module.inc.php`) and the `Plugin` (scheduler bridge + executor, port of `cron_plugin.inc.php` with the crontab backend replaced). Both are wired explicitly in the daemon bootstrap, gated on `server.web_server = 1` and config.toml enablement (PHP `cron_plugin::onInstall` only enabled when the web service is true).

Keeping the two-level dispatch (hook → named event → plugin) preserves the foundation's registry architecture and matches the web/dns module shape.
*Alternative*: handle the `cron` table hook directly in the plugin — rejected: breaks the announced-events contract shared with future plugins (e.g., a monitoring plugin that watches client job failures).

### D2 — Client-job registry is distinct from the foundation system scheduler
`engine.Scheduler` is for **static, named, code-registered maintenance jobs** (D1b/D12: `datalog_prune`, `letsencrypt_renew`, `dns_resign`), mirrored into `sys_config` group `scheduler`. Client cron rows are dynamic CRUD entities; stuffing thousands of them into that registry would pollute the admin "scheduler status" surface and force a full asynq reschedule on every panel save.

The cron plugin therefore owns a **client-job runner** (`ClientJobRunner`) that:
- Holds an in-process schedule set keyed by `cron.id` (backed by `robfig/cron/v3`, already a foundation dependency).
- On `cron_insert` / `cron_update` / `cron_delete`, adds / replaces / removes the entry for that id (and, on insert/update of inactive jobs, ensures the entry is absent).
- On daemon start, loads every `cron WHERE active='y' AND server_id=<this>` and registers them (self-healing; same spirit as PHP rewriting the file from DB state).
- Does **not** write any `sys_config` rows for client jobs; run history lives in `sys_log` (D6).

The foundation system scheduler remains untouched. Client jobs plug into the same process, not into the same registry.
*Alternative*: one minute-tick system job that re-scans the `cron` table — rejected for latency (up to 60s) and for not matching the proposal's "register/update/remove" wording.

### D3 — Schedule composition from the five ISPConfig fields
The `cron` table stores schedule as five columns (`run_min`, `run_hour`, `run_mday`, `run_month`, `run_wday`), not a single cron string. Port of `cron_plugin::_write_crontab`:

- Normal jobs: compose a standard 5-field expression `run_min run_hour run_mday run_month run_wday` after stripping spaces (PHP does `str_replace(" ", "", …)` per field).
- Special case: when `run_month == '@reboot'`, the job is **not** a recurring cron entry — it runs once when the daemon (re)starts, then stays registered only for the next start (PHP emits a literal `@reboot` crontab line).

Validation of field syntax and minimum frequency is API-side (D8), port of `validate_cron.inc.php`. The runner trusts already-validated rows and only parses the composed expression (or handles `@reboot`).

### D4 — Job types and command assembly (PHP semantics, new backend)
Port of the type branches in `cron_plugin::_write_crontab`, with execution replaced:

| `type` | Execution | Working dir / notes |
|---|---|---|
| `url` | HTTP GET of `command` after `{DOMAIN}` substitution; timeout (default 7200s, PHP `wget -T 7200`); TLS verification **on** by default (deliberate hardening vs PHP `--no-check-certificate`) | n/a |
| `full` | Direct `exec` of the command as the site's `web_domain.system_user` / `system_group` (argv slice, **no shell**); cwd = `{document_root}/web` | Placeholders `{DOMAIN}`, `{DOCROOT_CLIENT}`, `[web_root]`, `{SITE_PHP}` expanded before split |
| `chrooted` | Same as `full` until jailkit lands: run as site user without a jail; if `command` is rooted under `document_root`, strip that prefix (PHP parity for in-jail paths) | Jailkit deferred (Non-goals) |

Type derivation on the API (port of `cron_edit.php::onSubmit`):
- If `command` matches `^https?://` (case-insensitive) → force `type = 'url'`.
- Else look up the parent site's owner `client.limit_cron_type`: `full` → `type = 'full'`; otherwise `type = 'chrooted'`. Sites owned by admin (no client row) default to `full`.

Commands containing `\n`, `\r`, or NUL are rejected at validation and re-checked at execution (PHP skips with a WARN). URL commands containing `\` are rejected. URL host must look like a hostname after `{DOMAIN}` expansion (port of `validate_cron::command_format`).

PHP-CLI binary for `{SITE_PHP}` comes from `web_domain` joined to `server_php.server_php_id` → `php_cli_binary`, falling back to `/usr/bin/php` (PHP parity).

### D5 — Fail-safe privilege drop for `full` / `chrooted`
Every non-URL job is launched via `os/exec` with:

- `SysProcAttr.Credential = {Uid, Gid}` resolved from `system_user` / `system_group` (must exist; root uid/gid is **always** refused — port of PHP `is_allowed_user`/`is_allowed_group` check that rejects root ownership).
- `SysProcAttr.NoNewPrivileges = true` (Linux) so the process cannot regain privileges.
- Direct argv (command string split with a shell-safe argv parser that does **not** invoke `/bin/sh`); no `sh -c`.
- Enforced timeout via `context.WithTimeout` + kill of the process group on expiry.
- If uid/gid resolution or the privilege drop fails for any reason, the job is **aborted and logged** — a job is never executed as root.

`url` jobs need no privilege drop (they are pure HTTP from the daemon process).

### D6 — Execution log via `sys_log` field convention
The embedded schema is inviolable, so there is no `cron_run` table. When a job finishes and `cron.log = 'y'` (or always on hard failure — privilege-drop abort, timeout, non-zero exit for logged jobs), the plugin inserts a `sys_log` row:

| Column | Value |
|---|---|
| `server_id` | this server |
| `datalog_id` | `0` (not tied to a datalog row; same as PHP's non-datalog log path) |
| `loglevel` | info / warn / error (reuse existing daemon loglevel constants) |
| `tstamp` | Unix start or end time |
| `message` | structured single-line: `cron_run id=<id> parent_domain_id=<pd> type=<type> status=<ok\|exit\|timeout\|error> exit=<code> start=<unix> end=<unix> output=<tail>` |

The panel run-history view filters `sys_log.message LIKE 'cron_run id=<id> %'` (and scopes by the caller's ability to read that cron row). Output tail is truncated (e.g. last 4 KiB) so a chatty job cannot blow up `sys_log`. When `log = 'n'`, successful runs produce no `sys_log` row; aborts that indicate a security problem (privilege-drop failure) are still logged at error level.

*Alternative*: a versioned additive DDL table — rejected for this change (proposal: reuse `sys_log`; any future table would be a separate additive migration, never a change to the embedded schema).

### D7 — Legacy OS crontab cutover
On plugin load the runner:

1. Reads getconf section `cron` (`crontab_dir`, default `/etc/cron.d`) for this server.
2. Lists files matching `ispc_*` and `ispc_chrooted_*` (and Gentoo-style `ispc_*.cron`) under that directory.
3. Unlinks them and logs each removal.
4. Registers every active DB job (D2).

This guarantees that after cutover a job never runs both in vixie-cron and in the daemon. Migrated ISPConfig3 databases keep their `cron` rows; the first daemon start re-arms them in-process. No import of crontab file contents is required — the DB is the source of truth (PHP already wrote from DB).

### D8 — API surface, validation, limits, ownership
Port of `sites_cron_*` and `cron.tform.php` / `cron_edit.php` / `validate_cron.inc.php` onto the foundation entity framework under `/api/sites/crons` (same Sites module as web domains):

**Fields** (exact `cron` columns): `id`, `sys_*` riud, `server_id`, `parent_domain_id`, `type` (`url`/`chrooted`/`full`), `command`, `run_min`, `run_hour`, `run_mday`, `run_month`, `run_wday`, `log` (`n`/`y`), `active` (`n`/`y`).

**Validation (API layer):**
- `parent_domain_id` required, must reference an accessible `web_domain` with `type='vhost'`; on create, `server_id` and `sys_groupid` are forced from the parent (port of `onSubmit` / `onAfterInsert`); `parent_domain_id` is immutable on update (PHP sets `edit_disabled`).
- Schedule fields via port of `validate_cron::run_time_format` / `run_month_format` (`@reboot` only legal in `run_month`); character set `0-9,-/*` (plus `@reboot` for month); range checks per field (min 0–59, hour 0–23, mday 1–31, month 1–12, wday 0–7); step and range syntax as in PHP.
- `command` NOTEMPTY + `command_format` (no CR/LF/NUL; URL rules when scheme present).
- `type` in the enum; after auto-derivation (D4) re-validated against client limits.

**Client limits (non-admin):**
- `limit_cron`: on create, if `>= 0`, reject when `COUNT(cron WHERE sys_groupid = client_group) >= limit_cron` (`-1` = unlimited, PHP parity).
- `limit_cron_type`: when `url`, reject non-URL types; when `chrooted`, reject `full`; `full` allows all.
- `limit_cron_frequency`: if `> 1`, reject when the schedule's minimum frequency in minutes (port of `cron_min_freq` computation in `validate_cron`) is strictly less than the limit.

**Datalog:** every create/update/delete writes `sys_datalog` with `dbtable='cron'`, `dbidx='id:<id>'`, action `i`/`u`/`d`, `{old,new}` JSON payload, `server_id` of the parent site — interface never touches the OS.

**Run history endpoint:** `GET /api/sites/crons/:id/runs` (paginated) reads `sys_log` by the D6 convention, riud-scoped through the parent cron row.

### D9 — UI shape mirrors cron.tform.php under Sites
Sites module gains a **Cron** sidebar section:

- List: DataTable over `/api/sites/crons` (columns: active, parent domain, schedule summary, type, command truncated, log flag); search/filter; add button.
- Form: TabbedForm single tab (the PHP form has one tab `cron`) with server (derived/read-only after parent pick), parent domain select (vhosts only; disabled on edit), the five schedule fields, command (with placeholder help for `{DOMAIN}` / `{DOCROOT_CLIENT}` / `{SITE_PHP}`), type (display; may be read-only when auto-derived), log, active.
- Run history: secondary view or form section listing recent `sys_log` cron_run lines for that job (start, duration, status, output tail).

All strings through i18n (`en.json`). Client-side validation mirrors the API rules; API field errors display inline.

### D10 — No system crontab is ever written
Even if getconf still exposes `crontab_dir` / `wget` (needed for cutover cleanup and for migrated `server` config sections), the plugin **never** calls the PHP `_write_crontab` path. Tests assert that no file under `crontab_dir` is created by the plugin during insert/update/delete of active jobs.

## Risks / Trade-offs

- [Daemon downtime skips `@reboot` and in-window schedules] → same class of risk as any in-process scheduler; document that the daemon must stay up; `@reboot` runs on daemon start, not host boot if the daemon is not enabled at boot (installer responsibility).
- [Privilege-drop bugs escalate to root] → hard fail closed (D5): missing user, uid 0, or `Credential` setup error aborts; unit tests cover the refusal paths; integration tests run a non-root helper binary.
- [Command argv splitting differs from shell crontab lines] → deliberate security improvement (no shell metacharacter injection). Document that clients must not rely on shell features (`>`, pipes, `&&`); URL type and simple absolute commands cover the common hosting cases. PHP already forbade CR/LF/NUL and `\` in URLs.
- [sys_log growth from chatty jobs] → output tail cap + only log when `log='y'` (plus security aborts); future monitor module can prune `sys_log`.
- [TLS verification breaks URL jobs against broken site certs] → deliberate hardening; operators can fix the cert. If needed later, a per-job or global toggle can be added without schema changes (config.toml).
- [Race: job fires while an update is mid-flight] → runner replaces the entry under a mutex; in-flight executions finish with the old command snapshot (acceptable; same as a crontab rewrite mid-minute).
- [Client limits not yet centralized in `RegisterLimitHook`] → enforce cron-specific limits inside the cron entity's `Prepare`/validators for this change; when `add-client-module` lands it can fold them into the shared hook.

## Migration Plan

- Ships as code only — no schema change. The `cron` getconf section (`crontab_dir`, `wget`, `init_script`) already exists in migrated `server` config and in the installer template; only `crontab_dir` is read (for cutover).
- Fresh installs: no legacy files; runner starts empty and waits for API-created rows.
- Migrated ISPConfig3 databases: existing `cron` rows work as-is; first daemon start removes `ispc_*` crontabs and registers active jobs in-process (self-healing).
- Rollback: disable the cron module in `config.toml`; client jobs stop running (and legacy files are already gone — operators who need PHP again must re-enable the PHP plugin to regenerate crontabs from DB).

## Open Questions

- Should URL jobs honor a configurable timeout shorter than PHP's 7200s default for panel UX (long-running HTTP holds a worker slot)? Start with 7200s parity; make it getconf/config.toml later if needed.
- When jailkit lands (`add-ftp-shell-module`), should `chrooted` jobs automatically start using `jk_chrootsh`, or require an explicit re-save? Prefer automatic when jail is present for that site user (detect and use).
