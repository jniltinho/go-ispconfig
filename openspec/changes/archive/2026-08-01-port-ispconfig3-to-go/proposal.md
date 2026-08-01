# Proposal: Port ISPConfig3 to Golang (go-ispconfig)

## Why

ISPConfig3 is a mature PHP hosting control panel, but it depends on a PHP runtime, cron-based daemons and a legacy codebase (PHP 5.4 compatibility, own template engine, serialized-PHP data). Porting it to Go delivers a single static binary (panel + API + daemon + installer), modern tooling, and a maintainable base — starting with the two modules that matter first: **nginx (web/HTTP)** and **Bind (DNS)**.

## What Changes

This is the **foundation change**. It creates the go-ispconfig application skeleton and the core services every module depends on. Each ISPConfig module (nginx, bind, installer, panel UI) is proposed in its **own separate OpenSpec change**.

- New Go module `go-ispconfig`: Cobra CLI (`serve`, `daemon`, `migrate`, `init`, `version`) + `config.toml` (Viper), following the go-cubemail layout (`cmd/`, `internal/`, `web/dist` embed).
- **Database 100% identical to ISPConfig3**: the schema is created from the embedded original DDL (`install/sql/ispconfig3.sql`, all ~80 tables, same names/types/indexes) so an existing `dbispconfig` database can be pointed at go-ispconfig — enabling migration of existing ISPConfig3 clients. GORM models map onto this schema for the tables the initial modules use (sys, server, client, web, dns groups).
- **sys_datalog engine** (the architectural core): API writes `{old,new}` diffs to `sys_datalog` (JSON instead of PHP `serialize`); the daemon polls `datalog_id > server.updated`, raises table-hooks → named events → module plugins (Go port of `server/server.php`, `server/lib/classes/modules.inc.php`, `plugins.inc.php`).
- **Modern daemon replaces all ISPConfig cron jobs**: one persistent systemd service with an internal scheduler — datalog processing loop (replaces the every-minute `server.sh` cron) plus scheduled jobs (replaces `cron_daily.sh`: traffic accounting, backups, cert renewal hooks). No system crontab entries required.
- **asynq task queue (Redis/Valkey)** for system jobs and multiserver coordination: per-server queues (`server:<id>`), scheduler jobs as asynq periodic tasks, retries/priorities/uniqueness, instant daemon wake on datalog writes (`datalog:ready`), asynqmon-compatible. sys_datalog remains the source of truth for config changes; the queue transports work, never state.
- **Auth + permission model**: sys_user/sys_group with Unix-style `riud` permission strings (`sys_perm_user/group/other`) on every record — ported faithfully from `interface/lib/classes/tform_base.inc.php` semantics, including the reseller graph (`sys_user.groups`, `default_group`, `client.parent_client_id`). Access levels: admin, reseller, client (mail login later). CSRF protection on all mutating endpoints. Security policy flags ported from `security/security_settings.ini` (stored in `sys_config`, enforced by the API).
- **Client limits hook**: the CRUD framework ships a limit-check hook point from day one (no-op until `add-client-module` provides `limit_web_domain`/`limit_dns_zone` enforcement) so phase-1 sites/DNS endpoints don't need retrofitting.
- **REST API** (Echo v5) replacing `interface/lib/classes/remoting.inc.php` + `remote.d/*`: session auth, JSON, same function surface for dns/sites/admin domains (mail later). **Fully documented**: swaggo annotations on every endpoint, embedded Swagger UI to exercise/test the whole API, and godoc comments on all exported code.
- **Template renderer** compatible with ISPConfig `.master` templates (`<tmpl_var>`, `<tmpl_if>`, `<tmpl_loop>`) so existing nginx/bind templates can be reused with minimal translation.
- **Vue 3 + Tailwind v4 panel skeleton** embedded via `embed.FS`: login, layout (topbar modules + sidebar), i18n structure (English first, translation-ready — replaces `.lng` files with JSON locales), fonts vendored locally, square corners.
- Docs in English (`README.md`, `docs/`), `AGENTS.md` runbook, GitHub repo `go-ispconfig`, GitHub Actions release workflow.

## Capabilities

### New Capabilities

- `core-cli`: Cobra CLI, config.toml loading, version injection, single-binary layout with embedded frontend.
- `core-database`: GORM models, migrations and seed for the ISPConfig core schema (sys, server, client, web, dns groups) on MariaDB.
- `sys-datalog`: transactional datalog writer (API side) + daemon consumer with table-hook/event registries and delayed service restarts.
- `auth-permissions`: panel authentication, sessions, and the riud user/group/other record permission model with access levels admin/reseller/client.
- `rest-api-core`: Echo v5 REST API core — auth endpoints, CRUD framework with validation (REGEX, UNIQUE, NOTEMPTY, ISEMAIL, ISINT, ISIPV4, ISIPV6…) mirroring tform validators, datalog integration.
- `master-templates`: renderer for ISPConfig `.master` template syntax used by config generation.
- `panel-skeleton`: embedded Vue 3 + Tailwind v4 SPA shell — login, module navigation, listview/form primitives, i18n framework (en first).

### Modified Capabilities

(none — greenfield project)

### Follow-up changes (separate proposals, one per module)

- `add-web-nginx-module` — port of `server/plugins-available/nginx_plugin.inc.php` + `web_module` + sites UI.
- `add-dns-bind-module` — port of `server/plugins-available/bind_plugin.inc.php` + `dns_module` + DNS UI.
- `add-installer-cli` — port of `install/` for Debian 11–13 / Ubuntu 22.04–24.04 + Vagrant test rig (Ubuntu 24.04).
- `add-panel-ui-theme` — ISPConfig-based theme recreated (nicer, modern, square corners) in Tailwind.
- `add-legacy-migration` — migration assistant: connect to a running PHP ISPConfig3 via its remote API (`remoting`), import existing data (clients, sites, DNS zones, users), driven from the web UI (wizard) or CLI (`go-ispconfig migrate-from`). Complements the drop-in shared-DB path: works when the legacy panel runs on another host/database.

## Impact

- New repository `go-ispconfig` on GitHub (created with `gh`).
- Dependencies: `labstack/echo/v5`, `gorm.io/gorm` + mysql driver, `spf13/cobra`, `spf13/viper`, `swaggo/swag`, a crypt(3) verification lib for legacy `$6$`/`$1$` hashes (e.g. `github.com/GehirnInc/crypt`), `golang.org/x/crypto` (bcrypt), `github.com/hibiken/asynq` (+ Redis/Valkey server on the host — installed by the installer); frontend: Vue 3, Vite, Tailwind v4, Pinia.
- Reference PHP sources (read-only, under `base/ispconfig3_install/`): `server/server.php`, `server/lib/classes/{modules,plugins,services,getconf,tpl}.inc.php`, `interface/lib/classes/{db_mysql,tform,tform_actions,remoting}.inc.php`, `install/sql/ispconfig3.sql`.
- Testing: testify unit tests, integration tests against MariaDB, agent-browser E2E for the panel, Vagrant for install testing.

## Non-goals

- Mail, FTP, firewall, jailkit, OpenVZ/VM, XMPP, APS, monitor, mailman, webdav modules — out of scope for the initial port (schema tables may exist, but no logic).
- Apache2 support (nginx only initially); PowerDNS (Bind only initially).
- Multi-server / mirror replication — schema keeps `server_id` semantics, but the first release targets single-server.
- Running go-ispconfig and PHP ISPConfig **simultaneously** against the same DB (migration is a cutover: drain pending datalog, stop PHP, start Go). New datalog rows use JSON, not PHP serialize.
- Translations beyond English (structure ready, content later).
- CentOS/RHEL/openSUSE/Gentoo distro support.
- The ISPConfig `remote_user`/`remote_functions` granular remote-API grant model — **BREAKING** for migrated remote automations (scripts using `/remote/json.php` stop working at cutover; a compatibility adapter may come as a later change; documented in MIGRATION.md).
- Existing installs older than ISPConfig 3.3.x: `migrate` aborts and requires updating PHP ISPConfig to 3.3.x first (its updater applies the incremental DDL).
