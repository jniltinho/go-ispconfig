# Database module

Port of the ISPConfig3 database stack (`database_module.inc.php` +
`mysql_clientdb_plugin.inc.php` + the Sites → Databases interface):
self-service MySQL/MariaDB client databases and database users with
GRANT management, driven from the panel database through `sys_datalog`.
REST API under `/api/sites/databases` and `/api/sites/database-users`,
panel UI under **Sites → Databases / Database Users**, daemon module +
`mysql_clientdb` plugin on servers with `db_server = 1`.

Scope is **MySQL/MariaDB only**. PostgreSQL/MongoDB (the schema columns
exist for compatibility), bundled phpMyAdmin and database backups are
non-goals of this change.

## Data model

`web_database` (one client database) and `web_database_user` (a MySQL
login shared by one or more databases), byte-identical to the ISPConfig3
schema. Key columns:

| Column | Meaning |
|--------|---------|
| `database_name` / `database_name_prefix` | full name (prefix + user part, cropped to 64) and the prefix it was created with |
| `parent_domain_id` | owning website (vhost) — required; the database inherits its `sys_groupid` and `backup_copies` |
| `database_user_id` / `database_ro_user_id` | read-write and optional read-only login |
| `database_charset` | `''` (server default), `latin1`, `utf8`, `utf8mb4` — immutable after create |
| `remote_access` / `remote_ips` | `y` + comma-separated IPs (empty list = `%` wildcard) |
| `database_quota` | MB, `-1` unlimited; counted against `limit_database_quota` |
| `quota_exceeded` | honored when set: grants downgrade to `SELECT, DELETE, ALTER, DROP` |
| user: `database_password` / `database_password_sha2` | MySQL native (`*SHA1(SHA1())`) and caching_sha2 (`$A$005$…`) hashes — write-only, never returned by the API |

## Client-DB admin credentials

The daemon plugin connects with a **dedicated privileged user** — never
root — whose credentials live in a TOML file (the go-ispconfig
equivalent of ISPConfig's `server/lib/mysql_clientdb.conf`), mode
`0600`, owned by root:

```toml
# /etc/go-ispconfig/mysql_clientdb.conf
clientdb_host = "localhost"
clientdb_port = 3306          # optional, default 3306
clientdb_user = "goisp_clientdb"
clientdb_password = "secret"
```

`config.toml` points at it via `database.clientdb_conf` (default
`/etc/go-ispconfig/mysql_clientdb.conf`). Create the user with exactly
the privileges client-DB administration needs:

```sql
CREATE USER 'goisp_clientdb'@'localhost' IDENTIFIED BY '...';
GRANT ALL PRIVILEGES ON *.* TO 'goisp_clientdb'@'localhost' WITH GRANT OPTION;
```

(As in ISPConfig, the admin user needs global scope to create/drop
arbitrary client schemas, `CREATE USER`, grant on client DBs and read
`mysql.user` / `information_schema` — `WITH GRANT OPTION` included.
What it must never be is the panel's own DB user or root credentials
stored in `config.toml`.)

A failed connection aborts the event with an error log; the daemon keeps
running and the row is retried on the next resync/change.

## Panel-wide configuration (getconf `sites` section)

Stored in `sys_ini` (editable from the panel), consumed by API and UI:

| Key | Meaning |
|-----|---------|
| `dbname_prefix` / `dbuser_prefix` | name templates with `[CLIENTNAME]`, `[CLIENTID]`, `[DOMAINID]` placeholders; expanded on create, preserved on update, full name cropped to 64 (DB) / 32 (user) |
| `phpmyadmin_url` | optional external URL template with `[SERVERNAME]` / `[DATABASENAME]`; when set, the databases list shows an "Open phpMyAdmin" action (no bundled installation) |
| `default_dbserver` | server preselected for admin creates |
| `default_remote_dbserver` | comma-separated IPs merged into `remote_ips` |
| `disable_client_remote_dbserver` | `y` hides/ignores the remote-access controls for non-admin users |

The password policy comes from the getconf `misc` section
(`min_password_length`, default 8, and `min_password_strength`).

## REST API

Declarative entities under `/api/sites` with riud permission scopes and
transactional `sys_datalog` `{old,new}` journaling:

- `/api/sites/databases` — list/get/create/update/delete. Prepare
  enforces: MySQL only, parent vhost readable by the caller,
  prefix + crop, `mysql`/panel-DB blacklist, per-server uniqueness,
  immutable `server_id`/`database_charset`, admin-only rename and the
  remote-IP auto-merge when the website lives on another server.
  Client creates are gated by `limit_database`, `limit_database_quota`
  (client and reseller) and the `db_servers` allow-list.
- `/api/sites/database-users` — list/get/create/update/delete.
  Plaintext `database_password` is write-only: the API stores the
  native + caching_sha2 hashes (a submitted `*HEX40` native hash is
  kept as-is) and every read redacts the hash columns. An empty
  password on update means unchanged. Renames/password changes fan the
  datalog update out per distinct `server_id` of referencing databases;
  deleting a user nulls `database_user_id`/`database_ro_user_id` on its
  databases with journaled updates.

## Provisioning pipeline (daemon)

Servers with `server.db_server = 1` (and without
`daemon.disable_database_module` in `config.toml`) load the database
module and the `mysql_clientdb` plugin. The module hooks `web_database`
and `web_database_user` and raises `database_*` / `database_user_*`
events; `database_user_insert` is announced but deliberately unhandled —
accounts materialise on the first grant (`CREATE USER IF NOT EXISTS`).

- **Host lists**: always `localhost`; with `remote_access = 'y'` the
  valid IPs of `remote_ips`, or `%` when none. `DROP USER` only happens
  for hosts no other active database of that user needs.
- **Grant modes**: `rw` = `ALL PRIVILEGES`; `rd` (quota exceeded) =
  `SELECT, DELETE, ALTER, DROP`; `r` (read-only user) = `SELECT` after a
  `REVOKE ALL`. Batches end with `FLUSH PRIVILEGES`.
- **Passwords**: MariaDB / MySQL < 5.7 use `SET PASSWORD` with the
  native hash; MySQL ≥ 8 uses `caching_sha2_password` when the sha2
  hash exists. Refused while a `validate_password` plugin is active.
- **Rename**: empty databases are re-created; otherwise
  triggers/routines/events and views are dumped via `mysqldump` to
  mode-0600 temp files, base tables move with `RENAME TABLE` and the
  dumps are replayed.
- **Denylists**: `root`, `debian-sys-maint`, `mysql.infoschema` (users)
  and `mysql`, `information_schema`, `performance_schema` (schemas) are
  never touched; user drops are additionally guarded by
  `Create_user_priv = 'N'` in `mysql.user`.
- **No service registration**: privilege changes apply via
  `FLUSH PRIVILEGES`; the module never restarts MySQL.

## Migration notes

- Code only — no schema change; `web_database` / `web_database_user`
  rows adopted from ISPConfig3 work as-is.
- Physical MySQL databases and accounts already present are left
  untouched until the first datalog event (or a resync touch) for their
  row re-applies state — the pipeline is self-healing from DB state.
- Until the installer writes `mysql_clientdb.conf`, create the admin
  user manually (SQL above) and point `database.clientdb_conf` at the
  file.
- Rollback: set `daemon.disable_database_module = true`; MySQL objects
  stay as last applied.

## Testing

- Unit: module events, host lists, password statement decision tree,
  hash vectors, prefix helpers, validators, limits.
- Integration (`go test -tags=integration ./internal/...`, Docker):
  provisioning against MariaDB 11 (create/drop/rename/grants/users),
  the caching_sha2 hash against a real MySQL 8 login, REST handler
  suites and the end-to-end API → datalog → daemon → `information_schema`
  / `mysql.user` flow.
- UI: `make e2e-database PANEL_URL=… ADMIN_PASSWORD=…` (agent-browser).
