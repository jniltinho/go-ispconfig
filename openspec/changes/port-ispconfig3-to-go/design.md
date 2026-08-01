# Design: go-ispconfig foundation

## Context

ISPConfig3 (reference: `base/ispconfig3_install/`, v3.3.1p1) is three PHP layers decoupled **only through the database**:

1. `interface/` — panel; writes records + `{old,new}` diffs into `sys_datalog` (PHP `serialize`).
2. `server/` — cron daemon; polls `sys_datalog WHERE datalog_id > server.updated`, dispatches table-hooks (modules) → named events (plugins) that render `.master` templates and write service configs.
3. `install/` — distro-aware installer.

go-ispconfig collapses all three into one Go binary while keeping the same DB schema and event architecture. Project conventions come from go-cubemail (Echo v5, GORM, Cobra+Viper, Vue3+Vite+Tailwind embedded via `embed.FS`, Makefile with `CGO_ENABLED=0` + ldflags version injection, GitHub release action).

## Goals / Non-Goals

**Goals:**
- Single binary: `go-ispconfig serve` (panel+API), `go-ispconfig daemon` (datalog consumer), `go-ispconfig migrate`, `go-ispconfig init`, `go-ispconfig install` (later change).
- Faithful port of the sys_datalog event pipeline and the riud permission model.
- Reuse ISPConfig `.master` templates for nginx/bind config generation.
- MariaDB schema: **full identical DDL** (embedded `ispconfig3.sql`, ~80 tables); GORM models only for the subset the initial modules use.

**Non-Goals:**
- Mail/FTP/firewall/VM module logic, Apache, PowerDNS, multi-server mirroring, remote multi-host migration (that's `add-legacy-migration`). Same-host shared-DB cutover IS in scope (see Migration Plan).

## Decisions

### D1 — One binary, two run modes (serve / daemon)
`serve` runs Echo (API + embedded SPA). `daemon` is a **persistent process** (one systemd unit each). Rationale: mirrors ISPConfig separation (interface never touches the OS; only the daemon writes configs and restarts services), which keeps privilege separation — `serve` can run unprivileged, `daemon` runs as root.
*Alternative considered*: goroutine inside `serve` — rejected: forces the panel to run as root.

### D1b — Internal scheduler replaces every ISPConfig cron
ISPConfig relies on system cron (`server.sh` every minute, `cron_daily.sh` for traffic/backups/cleanup). go-ispconfig's daemon replaces all of it with an internal scheduler:
- **Datalog loop**: ticker (default 10s, configurable) running the processDatalog cycle — faster reaction than the 1-minute cron, single in-process mutex instead of pidfile locking.
- **Scheduled jobs**: named jobs with cron-style specs registered in code (daily traffic accounting, backup runs, cert renewals, datalog pruning), executed by the same daemon, with per-job last-run/status persisted in `sys_config` keys (NOT in the legacy `sys_cron` table — that table means system crontabs in ISPConfig3 and must stay untouched for schema compatibility) and exposed in the monitor UI.
Rationale: one supervised service, no crontab mutation on install, observable job history, graceful shutdown. *Alternative considered*: keep system cron — rejected: fragile install-time crontab editing and no job observability.

### D2 — sys_datalog with JSON payload
Same table/columns (`server_id`, `dbtable`, `dbidx`, `action` i/u/d, `data`), but `data` written by go-ispconfig is JSON `{"old":{...},"new":{...}}` instead of PHP serialize. Migration is a cutover with the pending queue drained first (see Migration Plan). Defensive rule: the consumer detects non-JSON payloads (leftover PHP-serialize rows), skips them with a `datalogError` log entry and quarantine marker instead of crashing, and never advances `server.updated` silently past a decode error without recording it. Pre-cutover datalog history remains readable only as raw text (documented; the monitor's future datalog viewer needs a dual serialize/JSON reader).
Daemon loop (port of `server.php`): single-instance lock → load server config → count pending (`server_id = ? OR server_id = 0`) → process (LIMIT 1000) → raise hooks → advance `server.updated` **per row** (PHP semantics — defines crash recovery: a crash mid-batch resumes at the first unprocessed row) → process `sys_remoteaction` via the action registry (`RegisterAction`/`RaiseAction`, port of `plugins.inc.php` actions) → flush delayed service restarts.
Startup guard: if `server.mirror_server_id != 0` or more than one active `server` row exists, the daemon refuses to start (multi-server/mirror is out of scope; guard prevents corrupting a migrated multi-server database).

### D3 — Registries as typed Go interfaces
Two-level dispatch ported directly:
- `Module` interface: `OnLoad()`, table-hook registration (`RegisterTableHook(table, fn)`), translates table changes into named events (`web_domain_update`, `dns_soa_insert`, …).
- `Plugin` interface: `OnLoad()`, subscribes to announced events (`RegisterEvent(name, fn)`).
- `Services` registry with **delayed restarts** (dedup restart/reload per service at end of run, port of `services.inc.php`).
Registration in code (Go `init()`/explicit wiring), not filesystem scanning of `*-enabled/` dirs. Rationale: compile-time safety; enabling/disabling modules moves to `config.toml`.

### D4 — riud permissions as middleware + GORM scopes
Every ported table keeps `sys_userid, sys_groupid, sys_perm_user, sys_perm_group, sys_perm_other` (strings like `riud`). A single GORM scope `WithPerm(user, 'r'|'i'|'u'|'d')` builds the WHERE clause (user match / group match / other), used by every repository. Auth levels: admin (sys_user id 1 semantics preserved), reseller, client. The reseller model ports the full ISPConfig graph — `sys_user.groups` (multi-group list), `default_group`, `client.parent_client_id` — not just a single groupid; the permission scope and its isolation test suite must cover reseller→client-group access explicitly.
Sessions: server-side (`sys_session` table). The SPA authenticates with an HTTP-only session cookie **plus CSRF protection** (SameSite=Strict and a per-session CSRF token required on every mutating endpoint); non-browser API clients use the same session id presented as a bearer token (one session mechanism, two transports — no separate token system in phase 1).
The ISPConfig `remote_user`/`remote_functions`/`remote_session` granular-grant model is **not ported in this change** — a deliberate breaking change for migrated remote automations (see proposal Non-goals; a `/remote/json.php`-compatible adapter may come as a later change).

### D5 — Validation engine mirroring tform validators
Port the tform validator set as composable Go validators: REGEX, UNIQUE, NOTEMPTY, ISEMAIL, ISINT, ISPOSITIVE, ISIPV4, ISIPV6, ISIP, CUSTOM. Field definitions live in Go structs per entity (replacing `form/*.tform.php`), consumed both by the API (validation) and exported to the frontend as JSON metadata (form rendering hints). Rationale: keeps ISPConfig's declarative-form character without PHP arrays.

### D6 — `.master` template engine port
Small lexer/renderer for `<tmpl_var>`, `<tmpl_if op= value=>`, `<tmpl_else>`, `<tmpl_loop>` (port of `server/lib/classes/tpl.inc.php` subset used by nginx/bind templates). Rationale: lets us copy `nginx_vhost.conf.master`, `bind_pri.domain.master`, `bind_named.conf.local.master`, `php_fpm_pool.conf.master` nearly verbatim — the templates encode years of edge cases; translating them to text/template would be the riskiest part of the port.
*Alternative considered*: convert templates to Go text/template — rejected for initial port, may happen later.

### D7 — Frontend: Vue 3 + Tailwind v4, Vite outDir → `web/dist`, embed.FS
Exactly the go-cubemail pattern: `//go:embed all:web/dist`, Vite dev proxy `/api → :8080`, Pinia stores, fonts self-hosted under `web/static/fonts`, axios-style API client. i18n: JSON locale files (`en.json` first) loaded by a light i18n composable — replaces `.lng` PHP arrays; every UI string goes through it from day one. Square corners enforced via Tailwind theme (`--radius: 0`).

### D8 — Config: config.toml via Viper + DB-stored runtime config
Static process config (listen address, DB DSN, paths) in `config.toml` (Viper, env prefix `GOISP_`). Runtime server behavior config stays **in the DB** like ISPConfig (`server.config` INI text, `sys_config`/`sys_ini`) parsed into typed structs — port of `getconf`. Rationale: the daemon of a given server must be reconfigurable from the panel, which is exactly why ISPConfig stores it in the DB.

### D12 — asynq task queue (Redis/Valkey) for system jobs and multiserver coordination
System tasks and the job queue run on **asynq** (github.com/hibiken/asynq) backed by Redis or Valkey (`[queue]` section in config.toml, default localhost). Design:
- **Per-server queues**: each server consumes only its queue (`server:<id>`); the API/panel enqueues jobs targeted at the owning server — the multiserver dispatch ISPConfig does via datalog polling, but with retries, priorities, uniqueness and observability built in.
- **Scheduler on asynq**: the internal scheduler's cron jobs (D1b) are registered as asynq periodic tasks; last-run/status still mirrored to `sys_config` for the panel. The daemon embeds the asynq server (worker) alongside the datalog loop.
- **Instant wake**: the datalog writer enqueues a lightweight `datalog:ready` task after commit, so the owning server's daemon processes changes immediately instead of waiting for the poll tick (the ticker remains as fallback).
- **sys_datalog stays the source of truth** for config changes — asynq transports work, never state; a lost Redis never loses configuration (daemon falls back to tick polling). This keeps the migration-first DB-identical architecture intact.
- asynqmon can be pointed at the same Redis for queue inspection during ops/debug.
Rationale (user requirement): manage multiserver like ISPConfig does, but better — typed jobs with retry/backoff instead of cron+polling only. *Alternative considered*: DB-backed queue table — rejected: reinvents asynq poorly and adds schema drift risk.

### D9 — Schema from the embedded original ISPConfig3 DDL (migration-first)
The full `install/sql/ispconfig3.sql` (~80 tables) is embedded in the binary; `migrate` executes it verbatim on an empty database, producing a schema **byte-identical to ISPConfig3** — names, types, indexes, defaults. GORM models with explicit `gorm:"column:..."` tags map onto the tables the initial modules use; the other tables exist untouched. Rationale (user requirement): an existing `dbispconfig` database from a PHP ISPConfig install must be usable by go-ispconfig, making client migration ISPConfig → Go-ISPConfig possible with zero schema conversion. `migrate` detects an existing ISPConfig schema (checks `dbversion` in `server`) and skips DDL, only validating compatibility. **Minimum supported dbversion is pinned to the embedded 3.3.1p1 schema**: older installs (3.1/3.2 — e.g. missing `dns_soa.rendered_zone`) abort with a clear message telling the operator to update PHP ISPConfig to 3.3.x first (its own updater runs `install/sql/incremental/`); go-ispconfig does not port the incremental DDL chain. Future go-ispconfig schema changes use versioned additive migrations tracked in a separate mechanism, never edits to the embedded original DDL.
*Alternative considered*: GORM AutoMigrate — rejected: generated DDL would drift from ISPConfig's exact schema and break drop-in migration. *Alternative considered*: porting the PHP incremental chain — rejected: the PHP updater already exists and is the supported upgrade path to 3.3.x.

### D10 — Legacy password hash compatibility
ISPConfig stores sys_user passwords as crypt hashes (SHA-512 `$6$`, older MD5-crypt `$1$`). Login verification SHALL accept these plus bcrypt. Re-hashing to bcrypt is **deferred and opt-in** (`auth.rehash_legacy = false` by default): PHP ISPConfig cannot verify bcrypt, so an eager rewrite would break the documented cutover rollback. Operators enable rehash once the cutover is final; until then legacy hashes are verified in place. (External review finding: eager rehash made rollback impossible.)

### D11 — API/code documentation as a deliverable
Every exported Go identifier gets a godoc comment (enforced by golangci-lint `revive` exported rules); every Echo handler gets swaggo annotations; `swag init` output is embedded and served as Swagger UI at `/swagger/` so the whole API can be exercised interactively. CI fails if swagger generation is stale.

## Risks / Trade-offs

- [.master engine subtle semantics (op=, nested loops)] → golden-file tests: render each template with fixture data and diff against output produced by the PHP `tpl.inc.php` for the same fixtures.
- [riud permission bugs = cross-client data leaks] → the GORM scope is the only query path (no raw table access in handlers); dedicated permission test suite per access level.
- [Daemon writes root-owned configs; a bug can break nginx/bind] → port ISPConfig's safety nets: `named-checkzone` / `nginx -t` validation before activation, keep previous file, `.err` quarantine on failure (bind_plugin behavior).
- [Echo v5 is newer than most examples] → go-cubemail already runs it in production; copy its server bootstrap.
- [Scope creep toward mail/etc.] → full DDL exists, but GORM models/logic only for sys/server/client/web/dns groups; other tables stay untouched until their own changes.

## Migration Plan

**Fresh install**: `init` writes config.toml, `migrate` creates schema (embedded ispconfig3.sql) + seed, `serve`+`daemon` run. Rollback = drop DB + remove binary.

**Migration from PHP ISPConfig3** (cutover, no coexistence):
1. Let the PHP daemon drain pending `sys_datalog` (until `server.updated` = max datalog_id).
2. Stop PHP ISPConfig services and its cron entries.
3. Point go-ispconfig's config.toml at the existing `dbispconfig`; `migrate` validates schema/dbversion, seeds nothing.
4. Start `serve` + `daemon`. Users log in with existing credentials (D10). New datalog rows are JSON.
5. One-shot post-cutover resync job: re-render all active vhosts/zones so nothing depends on stale PHP-era on-disk state (also covers IP changes when moving hosts).
Rollback checklist: stop go-ispconfig; drain any JSON datalog rows it wrote; verify `auth.rehash_legacy` was never enabled (if it was, affected users need password resets on PHP); re-enable the PHP crons. Schema untouched either way.

## Open Questions

- PHP-FPM pool management belongs to the nginx module change — does the foundation need `server_php` seeded? (Lean: yes, table + model only.)
- Datalog retention/pruning policy (ISPConfig keeps rows; decide a cleanup cron in a later change).
