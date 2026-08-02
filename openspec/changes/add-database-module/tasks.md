# Tasks: add-database-module

## 1. Models and foundations wiring

- [x] 1.1 Add GORM models for `web_database` and `web_database_user` with explicit `gorm:"column:..."` tags matching the ISPConfig3 schema (`database_id`, `sys_*`, `server_id`, `parent_domain_id`, `type`, `database_name`, `database_name_prefix`, `database_quota`, `quota_exceeded`, `last_quota_notification`, `database_user_id`, `database_ro_user_id`, `database_charset`, `remote_access`, `remote_ips`, `backup_interval`, `backup_copies`, `active`; user: `database_user_id`, `sys_*`, `server_id`, `database_user`, `database_user_prefix`, `database_password`, `database_password_sha2`, `database_password_mongo`, `database_password_postgres`); unit-test round-trip against MariaDB. Commit.
- [x] 1.2 Add client-DB admin config loading (path or TOML section for `clientdb_host` / `clientdb_user` / `clientdb_password` / optional port; file mode expectations documented; never log the password); unit tests for missing file and successful parse. Commit.
- [x] 1.3 Wire module enablement flag in `config.toml` defaults and document the `sites` getconf keys consumed (`dbname_prefix`, `dbuser_prefix`, `phpmyadmin_url`, `default_dbserver`, `default_remote_dbserver`, `disable_client_remote_dbserver`). Commit.

## 2. database module (daemon events)

- [x] 2.1 Implement `internal/modules/database` Module: announce the 6 events (`database_insert/update/delete`, `database_user_insert/update/delete`), register table hooks for `web_database` / `web_database_user`, map datalog actions `i`/`u`/`d` to events; gate on `server.db_server=1` + config.toml enablement; unit tests with fake registries. Commit.
- [x] 2.2 Confirm no MySQL service is registered in the services registry; test that module load does not add restart/reload handlers. Commit.

## 3. mysql_clientdb plugin — provisioning

- [x] 3.1 Implement plugin connection lifecycle and denylists (`root`, `debian-sys-maint`, `mysql.infoschema`; `mysql`, `information_schema`, `performance_schema`); skip non-`mysql` types; tests. Commit.
- [x] 3.2 Implement `getHostList` / `getOtherHostList` (localhost always; remote_access + remote_ips → IPs or `%`; union of other active DBs for the user); table-driven unit tests. Commit.
- [x] 3.3 Implement `createDatabase` / `deleteDatabase` with optional charset; integration test against MariaDB. Commit.
- [x] 3.4 Implement password application (`setPassword`) with MariaDB/MySQL version probe and native vs `caching_sha2_password` paths; refuse when `validate_password` plugin active; tests with stubbed server version. Commit.
- [x] 3.5 Implement `grant` / `revokeAndDrop` with modes `rw` / `rd` / `r`, `CREATE USER IF NOT EXISTS`, `FLUSH PRIVILEGES`; integration tests for ALL vs SELECT vs quota-restricted grants. Commit.
- [x] 3.6 Implement `dbInsert` / `dbUpdate` / `dbDelete` handlers (inactive early-out, missing-DB recreate, host/user reconcile, deactivate revoke); integration test datalog-shaped payloads. Commit.
- [x] 3.7 Implement `renameDatabase` (empty path + tables/views/triggers path with mysqldump temp files mode 0600); integration test rename with a base table. Commit.
- [x] 3.8 Implement `dbUserUpdate` / `dbUserDelete` (rename user, set password across hosts, drop via `mysql.user` with `Create_user_priv='N'` guard); do **not** register `database_user_insert`; tests. Commit.

## 4. Domain logic (API-side)

- [x] 4.1 Implement MySQL native and sha2 password hash helpers (port of `getPasswordHash` / MYSQL + MYSQLSHA2) with unit tests (known vectors for native `*SHA1(SHA1())` form). Commit.
- [x] 4.2 Implement name-prefix helpers for `dbname_prefix` / `dbuser_prefix` (placeholder expansion parity with sites tools) and full-name crop to 64; tests. Commit.
- [x] 4.3 Implement database validators on the foundation validation engine (regex, unique per server, charset set, remote_ips IP list, parent vhost required, immutable charset/server_id, non-admin rename guard, blacklist); tests. Commit.
- [x] 4.4 Implement database-user validators (regex, unique, password strength on create); tests. Commit.
- [x] 4.5 Implement client/reseller limit checks (`limit_database`, `limit_database_user`, `limit_database_quota`) and `db_servers` allow-list; permission tests (client/reseller/admin). Commit.
- [x] 4.6 Implement repositories / entity prepare hooks: riud scopes, datalog `{old,new}` writes, parent-site `sys_groupid` inheritance, remote_ips auto-merge when site server differs, user-delete nulling of FK refs, user-update datalog fan-out per distinct database `server_id`; redact password hashes on read. Commit.

## 5. REST API

- [x] 5.1 Database endpoints under `/api/sites/databases` (list/get/create/update/delete) via declarative Entity + swaggo annotations; handler tests incl. 403 cross-client. Commit.
- [x] 5.2 Database-user endpoints under `/api/sites/database-users` (list/get/create/update/delete) with write-only password; swaggo; tests. Commit.
- [x] 5.3 Regenerate swagger (`make swagger` / `swag init`), verify Swagger UI lists database endpoints, CI staleness check green. Commit.

## 6. Panel UI (Vue)

- [ ] 6.1 Sites navigation entries + database list (search, active/remote filters) and database-user list; en locale keys. Commit.
- [ ] 6.2 Database form (server, parent site, name+prefix, quota, charset, rw/ro users, remote access/IPs, active, backup_interval); respect edit locks and `disable_client_remote_dbserver`; API errors inline. Commit.
- [ ] 6.3 Database-user form (server, username+prefix, password); empty password on edit means unchanged. Commit.
- [ ] 6.4 phpMyAdmin external link action with `[SERVERNAME]` / `[DATABASENAME]` substitution when configured. Commit.
- [ ] 6.5 agent-browser E2E: create user, create database for a site, remote access toggle, password change, deletes; screenshots to `docs/prints/`. Commit.

## 7. Integration and docs

- [ ] 7.1 End-to-end integration test against MariaDB: API create user + database → datalog → daemon run → physical `CREATE DATABASE` + GRANTs observable via information_schema / mysql.user; update remote_ips and delete paths covered. Commit.
- [ ] 7.2 Module docs in `docs/` (client-DB admin user setup and required privileges, config keys, prefix rules, phpMyAdmin URL, migration notes: existing MySQL objects left until first event/resync). Commit.
