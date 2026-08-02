# Design: database module (MySQL/MariaDB client DBs)

## Context

ISPConfig3's database stack is three pieces glued by `sys_datalog`:

1. `interface/web/sites/` (`database_edit.php`, `database_user_edit.php` + tform/list defs) and `remote.d/sites.inc.php` (`sites_database_*`, `sites_database_user_*`) — panel and remote API write `web_database` / `web_database_user` with `{old,new}` datalog diffs, enforce name prefixes, client limits (`limit_database`, `limit_database_user`, `limit_database_quota`), and parent-site ownership.
2. `server/mods-available/database_module.inc.php` — registers table hooks for the two tables, announces six named events (`database_insert/update/delete`, `database_user_insert/update/delete`), maps datalog actions `i`/`u`/`d`.
3. `server/plugins-available/mysql_clientdb_plugin.inc.php` (~880 lines) — connects with a dedicated privileged client-DB admin user (from `mysql_clientdb.conf`), then creates/renames/drops databases, creates MySQL users, sets passwords (native + sha2 hashes), GRANT/REVOKE per host list (`localhost`, `%`, or explicit remote IPs), and drops orphan users. `database_user_insert` is announced but deliberately not subscribed (stale users without grants are useless).

The foundation change already provides everything this module plugs into: datalog consumer with table-hook/event registries, getconf, riud GORM scopes, validation engine, REST core, panel skeleton (Sites module). The DB tables exist (byte-identical ISPConfig3 schema); only GORM models and the module/plugin/API/UI are missing. This change depends on `add-web-nginx-module` for the Sites panel shell and `web_domain` parent association.

## Goals / Non-Goals

**Goals:**
- Behavior-faithful port of `database_module` + `mysql_clientdb_plugin`: same events, same MySQL DDL/GRANT semantics, same denylists, same host-list and password-hash rules.
- API/UI parity with the ISPConfig Sites → Databases surface for MySQL databases and database users (charset, remote access, RO user, active flag, prefixes, limits).
- Integration tests against MariaDB for the datalog → event → CREATE/GRANT pipeline.

**Non-Goals:**
- PostgreSQL / MongoDB engines (schema columns and `type` field exist; plugin skips non-`mysql`; API accepts only `mysql`).
- Bundled phpMyAdmin installation (external URL / link only).
- Remote multi-host DB servers separate from the managed host (single-server, per foundation).
- Database backups (`web_backup` integration) — later change; `backup_interval`/`backup_copies` fields are stored for schema parity but not enforced by a backup job here.
- Quota accounting daemon job that flips `quota_exceeded` from disk usage — store and honor the flag when set; the measurement job is out of scope (same deferral pattern as web HD quota).
- Translations beyond English.

## Decisions

### D1 — One Go package, two registrations (module + plugin)
`internal/modules/database` contains both the `Module` (table hooks → events, port of `database_module.inc.php`) and the `Plugin` (`mysqlClientDBPlugin`, port of `mysql_clientdb_plugin.inc.php`), wired explicitly in the daemon bootstrap. Keeping the two-level dispatch (hook → named event → plugin) preserves the foundation's registry architecture and matches the nginx/dns module pattern.
*Alternative*: collapse hooks into the plugin — rejected: breaks the announced-events contract shared with any future alternative engine plugins.

### D2 — No service registration
Unlike DNS/web, the database module does not register a systemd service for restart/reload. MySQL privilege changes take effect after `FLUSH PRIVILEGES` inside the plugin; no daemon bounce is required. PHP does not register a service either (`//$app->services->registerService(...)` is commented out).

### D3 — Dedicated client-DB admin credentials (never root in config)
The plugin connects with a dedicated privileged MariaDB/MySQL user whose credentials live in a config file equivalent to ISPConfig's `server/lib/mysql_clientdb.conf` (`clientdb_host`, `clientdb_user`, `clientdb_password`, optional port), mode `0600`, owned by root — path resolved via getconf / `config.toml` (e.g. `database.clientdb_conf` or an embedded TOML section). That user needs only the privileges required for client-DB administration (`CREATE`/`DROP` DATABASE, `CREATE USER`, `GRANT OPTION` on client DBs, read of `mysql.user` / `information_schema`). Root credentials are never stored. Connection failures are logged and the event aborts without panicking the daemon.
*Alternative*: reuse the panel's GORM DSN — rejected: the panel DB user is intentionally unprivileged for client-DB DDL; ISPConfig separates the two.

### D4 — Host list semantics (getHostList)
Port of `mysql_clientdb_plugin::getHostList`:
- Always includes `localhost`.
- When `remote_access = 'y'`: parse `remote_ips` as comma-separated values, keep only valid IPs; if the list is empty after filtering, use `%`.
- Unique + sorted.
`getOtherHostList` unions host lists of every other **active** database that still references the same user (rw or ro), so `DROP USER` only happens when no other DB still needs that user@host.

### D5 — GRANT modes and quota_exceeded
Port of `grant()` access modes:
| mode | when | privileges |
|---|---|---|
| `rw` | active DB, full user, `quota_exceeded != 'y'` | `ALL PRIVILEGES` |
| `rd` | active DB, full user, `quota_exceeded = 'y'` | `SELECT, DELETE, ALTER, DROP` |
| `r` | RO user (`database_ro_user_id`, distinct from rw user) | `SELECT` |

RO grants first `REVOKE ALL` then `GRANT SELECT` (PHP parity). After any batch of grant/revoke changes, `FLUSH PRIVILEGES`. When a DB becomes inactive (`active` n→n early-out; y→n revokes), grants are revoked and the user@host is dropped only if not still needed elsewhere.

### D6 — Password hashing dual-store (API-side)
On create/update of `web_database_user`, when a plaintext password is submitted the API stores:
- `database_password` = MySQL native hash `*` + uppercase hex of `SHA1(SHA1(password, binary))` (encryption `MYSQL`).
- `database_password_sha2` = caching_sha2-compatible hash (encryption `MYSQLSHA2`; same plaintext input; empty when password not changed).
- `database_password_postgres` / `database_password_mongo` columns are written only for schema parity if the existing PHP form does so, but are unused by the MySQL plugin; Go may leave them empty unless a trivial port is free.
Empty password on update means "leave unchanged" (PHP: password field empty → no hash rewrite; plugin `dbUserUpdate` no-ops when both name and password are unchanged).
The plugin's `setPassword` picks the auth plugin from server type/version: MariaDB or MySQL < 5.7 → `SET PASSWORD ... = native hash`; MySQL ≥ 8.0 with sha2 hash present → `ALTER USER ... IDENTIFIED WITH caching_sha2_password AS ...`; else native via `ALTER USER`. Refuse when the `validate_password` plugin is active (log error, skip). Never log plaintext or hash values beyond debug of the auth plugin name.

### D7 — database_user_insert announced, not handled
The module announces all six events (PHP parity). The plugin registers only `database_insert/update/delete`, `database_user_update`, `database_user_delete` — **not** `database_user_insert` (PHP comment: "stale user accounts are useless"). Users are materialised on the first `grant()` that does `CREATE USER IF NOT EXISTS` when a database is assigned to them.

### D8 — Denylists
Refuse create/drop/rename/grant/password/drop for:
- users: `root`, `debian-sys-maint`, `mysql.infoschema` (case-insensitive).
- databases: `mysql`, `information_schema`, `performance_schema`.
API-side additionally rejects the panel database name (from config) and `mysql` as database names (port of `database_edit.php` blacklist). Plugin logs WARN and returns without erroring the whole daemon run.

### D9 — Database rename
Port of `renameDatabase`:
- Denylist + case-insensitive same-name guards.
- Empty DB (no base tables/views/triggers): `CREATE` new + `DROP` old.
- Otherwise: dump triggers/routines/events and views via `mysqldump` to a mode-0600 temp file under the configured temp path; create new DB; drop old triggers; `RENAME TABLE` each base table; import triggers/views; drop old DB. Failures are logged; partial states are best-effort cleaned (temp files unlinked on success).
Privileges are re-applied by the surrounding `dbUpdate` host/user grant loop after rename.

### D10 — Sites global config for prefixes and phpMyAdmin
Consumed via getconf global `sites` section (already present for the web module):
- `dbname_prefix` / `dbuser_prefix` — applied on create (and preserved on update) via the existing tools-sites prefix helpers pattern (`[CLIENTNAME]`, etc.); full name = prefix + user-supplied suffix, cropped to 64 (DB) / 64 (user, schema max; MySQL user effective length still validated by regex `^[a-zA-Z0-9_]{2,64}$`).
- `phpmyadmin_url` — optional external URL template with `[SERVERNAME]` / `[DATABASENAME]` placeholders; when empty, UI may fall back to a documented conventional URL (`http(s)://<server>/phpmyadmin` or nginx `:8081/phpmyadmin`) without installing anything.
- `default_dbserver`, `default_remote_dbserver`, `disable_client_remote_dbserver` — used by API/UI for defaults and client remote-access restrictions (admin may always set remote access).

### D11 — Parent site ownership and backup field inheritance
Port of `sites_database_plugin`: on database insert/update, when `parent_domain_id > 0`, set `sys_groupid` from the parent `web_domain` row (so the DB is owned by the same client group as the site). `backup_interval` stays as submitted; `backup_copies` may be aligned from the parent site for parity. `parent_domain_id` is required and must be an accessible `web_domain` of type `vhost`. When the parent site's `server_id` differs from the DB's `server_id`, auto-enable `remote_access` and merge the web server's IP (plus `default_remote_dbserver` list) into `remote_ips` so the site can reach a DB on another role of the same single-server deploy.

### D12 — Client limits enforced on the API layer
On create (and quota on create/update):
- `client.limit_database` (−1 unlimited): count of `web_database` rows with `type='mysql'` and the client's `sys_groupid`.
- `client.limit_database_user` (−1 unlimited): count of `web_database_user` for the group.
- `client.limit_database_quota` (−1 unlimited): sum of `database_quota` (MB); reject or clamp when the new total would exceed; `database_quota = 0` rejected when the client has a positive quota limit; `database_quota = -1` means unlimited for that DB and is rejected when the client has a finite quota.
- Reseller limits checked the same way when the client has `parent_client_id > 0`.
- Client may only place DBs on servers listed in `client.db_servers`.
Admin bypasses client limits but still validates that assigned DB users share the parent site's `sys_groupid`.

### D13 — REST surface under `/api/sites`
Port of `sites_database_*` / `sites_database_user_*` onto the existing Sites API group (same pattern as `web-domains`, `web-folders`):
- `/api/sites/databases` — CRUD + list (filterable), get-all-by-user semantics via list scoped by client.
- `/api/sites/database-users` — CRUD + list.
Declarative `Entity` descriptors (fields from `database.tform.php` / `database_user.tform.php`), foundation validators, swaggo annotations, riud scopes, datalog `{old,new}` writes targeted at the row's `server_id`. Password hashes are never returned on GET (redact `database_password*`); write-only password field on create/update. Database-user update that changes name/password also fans datalog UPDATE rows per distinct `server_id` of databases still referencing that user (PHP remote API parity in `sites_database_user_update`). User delete nulls `database_user_id` / `database_ro_user_id` on referencing databases via datalog updates (PHP `sites_database_user_delete` / `database_user_del.php`).

### D14 — UI under Sites → Databases
Vue views living next to existing `frontend/src/views/sites/`:
- Database list (active, remote_access, server, site, user, name; search on name) + form (server, parent site, name+prefix, quota, charset, rw/ro user selects, remote access + IPs, active, backup_interval stored).
- Database-user list + form (server, username+prefix, password).
- Optional "Open phpMyAdmin" action when URL config is set.
Reuse DataTable, TabbedForm/EntityForm patterns, en.json i18n. No new top-level module — navigation entries inside Sites (matching ISPConfig's sites module menu).

### D15 — Module enablement
Load when `server.db_server = 1` and the module is enabled in `config.toml`. Non-DB servers ignore database datalog rows.

## Risks / Trade-offs

- [Plugin runs privileged MySQL DDL as the daemon] → dedicated low-scope admin user (D3); denylists (D8); all SQL uses parameterized/escaped identifiers; integration tests assert denylist refusals.
- [Rename with views/triggers depends on `mysqldump`/`mysql` CLI] → same as PHP; failures log and leave the old DB intact when create-new fails; temp dumps mode 0600 and unlinked.
- [Password hash algorithms differ across MariaDB/MySQL 5.7/8] → version probe (`SELECT VERSION()`, server_info) ports the PHP decision tree; dual columns keep both hashes available.
- [Stale MySQL users if panel rows deleted out of band] → user delete path queries `mysql.user` for matching User with `Create_user_priv = 'N'` and drops those hosts only (PHP guard against deleting system accounts).
- [Client limit races under concurrent creates] → count + insert in one transaction with group-scoped lock or re-check; acceptable rare race returns a limit error on the losing request.
- [Remote access `%` is powerful] → only when `remote_access=y` and `remote_ips` empty (PHP parity); UI warns; `disable_client_remote_dbserver` hides the control for non-admins.

## Migration Plan

- Ships as code only — no schema change. Existing `web_database` / `web_database_user` rows from ISPConfig3 work as-is.
- Fresh installs: installer (`add-installer-cli`) creates the dedicated client-DB admin user and writes `mysql_clientdb.conf` (or TOML section); until then operators create the user manually and point config at it.
- After cutover: first datalog event (or a resync touch) re-applies GRANTs from DB state (self-healing). Physical MySQL databases/users already present are left untouched until an event runs.
- Rollback: disable the database module in `config.toml`; MySQL objects remain as last applied.

## Open Questions

- Should `quota_exceeded` flipping get a small scheduler job in this change once disk usage can be measured, or stay strictly out of scope until a monitor/quota change? (Leaning out of scope; flag is honored when set by any writer.)
- Exact on-disk path for client-DB credentials vs pure `config.toml` section — prefer a separate `0600` file like PHP for least surprise on migrated servers; confirm with installer change.
