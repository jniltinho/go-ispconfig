# Proposal: add-database-module

> Roadmap phase 2 — proposal only. Design/specs/tasks will be written when this module is scheduled.

## Why

Hosting clients need self-service MySQL/MariaDB databases tied to their sites. This change ports the ISPConfig3 database module so go-ispconfig can provision client databases and database users with correct privileges and per-client limits, managed from the Sites panel and the API.

Reference PHP sources being ported (read-only under `base/ispconfig3_install/`):
- `server/mods-available/database_module.inc.php` — table hooks for `web_database` and `web_database_user` → named events (`database_insert/update/delete`, `database_user_insert/update/delete`)
- `server/plugins-available/mysql_clientdb_plugin.inc.php` — creates/renames/drops databases, manages MySQL users and GRANTs (incl. remote-access host lists), quota-related revocation
- `interface/web/sites/` (`database_edit.php`, `database_user_edit.php` + forms/lists) — panel UI
- `interface/lib/classes/remote.d/sites.inc.php` — remote API surface (`sites_database_add/update/delete/get`, `sites_database_user_*`)

## What Changes

- **database module (daemon side)**: Go `Module` registering table hooks for `web_database` and `web_database_user` and raising the six named events.
- **mysql_clientdb plugin**: Go `Plugin` consuming those events — `CREATE DATABASE`/`DROP DATABASE`, MySQL user create/update/delete (password handling incl. pre-hashed passwords), GRANT/REVOKE per database, remote-access host list (`%`, specific IPs), rename handling — port of `mysql_clientdb_plugin.inc.php` against a locally administered MariaDB/MySQL.
- **Per-client limits**: enforce `client.limit_database` (and quota fields) on create through the API layer.
- **REST API**: port of the `sites_database_*` / `sites_database_user_*` surface with swaggo annotations, riud scopes and datalog writes.
- **UI (Vue 3)**: Sites → Databases — database list/form (charset, remote access, database user assignment) and database-user list/form, following the ISPConfig3 layout.
- **phpMyAdmin access**: optional external link only (configured URL), no bundled installation.
- **Testing**: integration tests against MariaDB for the datalog→event→GRANT pipeline.

## Capabilities

### New Capabilities

- `database-module-events`: daemon database module — table hooks for web_database/web_database_user, named event dispatch.
- `database-provisioning`: MySQL/MariaDB database and user lifecycle with GRANT management and remote-access control (mysql_clientdb_plugin port).
- `database-rest-api`: REST endpoints porting the `sites_database_*` functions with swagger docs and client limit enforcement.
- `database-panel-ui`: Vue Sites → Databases UI — databases and database users.

### Modified Capabilities

(none — foundation capabilities are consumed, not changed)

## Impact

- **Depends on `port-ispconfig3-to-go`** (datalog registries, rest-api-core, auth-permissions, panel-skeleton) **and on `add-web-nginx-module`** (Sites panel module, `web_domain` association, client limits context).
- New Go packages: `internal/modules/database` (module + plugin), REST handlers, Vue additions to the `sites` module.
- DB: no schema changes — uses existing `web_database` and `web_database_user` tables; needs a **dedicated privileged DB user** on the client-facing MariaDB/MySQL instance, created with exactly the privileges required for client-DB administration (`CREATE`/`DROP`/`GRANT OPTION` scope — the standard ISPConfig pattern of a dedicated admin user), never stored root credentials.

## Non-goals

- PostgreSQL support — MySQL/MariaDB only.
- Bundled phpMyAdmin (symlink/external link only).
- Remote database servers separate from the managed host (single-server, per foundation).
- Database backups (`web_backup` integration) — later change.
- Translations beyond English.
