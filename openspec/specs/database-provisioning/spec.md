# database-provisioning Specification

## Purpose
TBD - created by archiving change add-database-module. Update Purpose after archive.
## Requirements
### Requirement: Client-DB admin connection
The mysql_clientdb plugin SHALL connect to the locally administered MariaDB/MySQL using dedicated credentials (host, user, password, optional port) loaded from a restricted config file or section equivalent to ISPConfig's `mysql_clientdb.conf`, never using the panel root account. Connection failure SHALL log an error and abort the current event without panicking the daemon.

#### Scenario: Missing credentials abort the event
- **WHEN** a `database_insert` event fires and the client-DB config file is missing or unreadable
- **THEN** the plugin logs an error, performs no DDL, and returns

#### Scenario: Successful connect reuses a short-lived connection
- **WHEN** an event handler starts
- **THEN** it opens a connection, performs its work, and closes the connection before returning (no process-wide leaked handle)

### Requirement: Create and drop MySQL databases
On `database_insert` for `type = 'mysql'` the plugin SHALL `CREATE DATABASE` with the optional `DEFAULT CHARACTER SET` from `database_charset` when non-empty. On `database_delete` for `type = 'mysql'` it SHALL `DROP DATABASE`. Names in the denylist (`mysql`, `information_schema`, `performance_schema`, case-insensitive) SHALL be refused with a warning log. Non-`mysql` types SHALL be no-ops.

#### Scenario: Active database is created with charset
- **WHEN** `database_insert` fires for `database_name=c1_app`, `database_charset=utf8mb4`, `type=mysql`
- **THEN** MySQL has a database `c1_app` with default charset utf8mb4

#### Scenario: Denylisted name is refused
- **WHEN** `database_insert` fires for `database_name=mysql`
- **THEN** no `CREATE DATABASE` is executed and a warning is logged

#### Scenario: PostgreSQL type is ignored
- **WHEN** `database_insert` fires with `type=postgresql`
- **THEN** the plugin returns without connecting or running DDL

### Requirement: Host list for grants
For each database record the plugin SHALL build the host list as: always `localhost`; when `remote_access = 'y'`, the valid IPs from comma-separated `remote_ips`, or `%` when that list is empty after filtering; unique and sorted (port of `getHostList`).

#### Scenario: Remote access with empty IPs grants from anywhere
- **WHEN** a database has `remote_access=y` and empty `remote_ips`
- **THEN** grants are applied for hosts `localhost` and `%`

#### Scenario: Remote access with explicit IPs
- **WHEN** a database has `remote_access=y` and `remote_ips=1.2.3.4, 5.6.7.8`
- **THEN** grants are applied for `1.2.3.4`, `5.6.7.8` and `localhost` only

#### Scenario: Remote access disabled is localhost only
- **WHEN** a database has `remote_access=n`
- **THEN** grants are applied for `localhost` only

### Requirement: GRANT modes for rw, ro and quota-exceeded users
For each host in the host list of an **active** database the plugin SHALL:
- grant the rw user (`database_user_id`) `ALL PRIVILEGES` when `quota_exceeded != 'y'`, else `SELECT, DELETE, ALTER, DROP`;
- grant the ro user (`database_ro_user_id`) `SELECT` only when it is set and distinct from the rw user (after `REVOKE ALL` on that DB for the ro user);
- `CREATE USER IF NOT EXISTS` then set password before GRANT;
- `FLUSH PRIVILEGES` once after any successful grant/revoke batch.
Users in the denylist (`root`, `debian-sys-maint`, `mysql.infoschema`) SHALL be refused.

#### Scenario: Full user gets ALL on insert
- **WHEN** `database_insert` fires for an active DB with a rw user and `quota_exceeded=n`
- **THEN** the user has `ALL PRIVILEGES` on that database for each host in the host list

#### Scenario: Quota exceeded restricts rw grants
- **WHEN** `database_update` sets `quota_exceeded=y` on an active DB
- **THEN** the rw user is re-granted only `SELECT, DELETE, ALTER, DROP` on that database

#### Scenario: RO user gets SELECT only
- **WHEN** an active DB has a distinct `database_ro_user_id`
- **THEN** that user has `SELECT` (and not INSERT/UPDATE) on the database

### Requirement: Update reconciles hosts, users and rename
`database_update` for `type=mysql` SHALL: no-op when both old and new are inactive; create the database if the name is unchanged but missing on the server; rename via the rename procedure when `database_name` changes; revoke old hosts/users no longer needed; grant new hosts/users; drop user@host only when `getOtherHostList` shows no other active DB still needs that user on that host.

#### Scenario: Deactivating a database revokes grants
- **WHEN** `database_update` changes `active` from `y` to `n`
- **THEN** privileges on the old database name are revoked for old users/hosts and processing stops without new grants

#### Scenario: Host removed from remote_ips
- **WHEN** `remote_ips` no longer includes `9.9.9.9` and no other active DB of the user lists that host
- **THEN** the user is dropped at `9.9.9.9` after revoke

#### Scenario: User reassignment revokes previous user
- **WHEN** `database_user_id` changes from user A to user B
- **THEN** A is revoked (and dropped when unused elsewhere) and B is granted on the current host list

### Requirement: Database rename with tables, views and triggers
When `database_name` changes the plugin SHALL rename by: refusing denylisted or case-only renames; for empty DBs create-new + drop-old; otherwise dump triggers/routines/events and views with `mysqldump` to a mode-0600 temp file, create the new DB, drop old triggers, `RENAME TABLE` each base table into the new schema, import dumps, drop the old database (port of `renameDatabase`).

#### Scenario: Empty database rename
- **WHEN** a database with no tables/views/triggers is renamed from `a` to `b`
- **THEN** database `b` exists, database `a` does not

#### Scenario: Rename with base tables
- **WHEN** a database containing base tables is renamed
- **THEN** those tables exist under the new name and the old database is dropped

### Requirement: Database user password and rename
On `database_user_update` the plugin SHALL, for every host derived from databases that still reference the user: `RENAME USER` when the username changed; set the password when `database_password` changed and the new value is non-empty; no-op when both username and password are unchanged. Password application SHALL follow server type/version: MariaDB or MySQL < 5.7 use native `SET PASSWORD` with `database_password`; MySQL ≥ 8.0 prefer `caching_sha2_password` with `database_password_sha2` when present, else native via `ALTER USER`.

#### Scenario: Password change updates all hosts
- **WHEN** a database user password changes and the user is granted on `localhost` and `%`
- **THEN** the password is updated for both hosts and `FLUSH PRIVILEGES` runs

#### Scenario: Unchanged user is a no-op
- **WHEN** `database_user_update` fires with identical username and password hashes
- **THEN** no SQL user statements are executed

### Requirement: Database user delete
On `database_user_delete` the plugin SHALL refuse denylisted usernames; select `mysql.user` rows for that User with `Create_user_priv = 'N'`; `DROP USER` each matching User@Host; `FLUSH PRIVILEGES` when any drop succeeded.

#### Scenario: Client user is dropped on all hosts
- **WHEN** `database_user_delete` fires for a non-denylisted user present on `localhost` and `10.0.0.1`
- **THEN** both User@Host pairs are dropped

#### Scenario: System-like user is refused
- **WHEN** `database_user_delete` fires for `root`
- **THEN** no `DROP USER` runs and a warning is logged

### Requirement: database_user_insert is not handled by the plugin
The plugin SHALL announce-compatible subscription omit `database_user_insert` (PHP parity): creating a panel user alone does not create a MySQL account until a database grant path runs `CREATE USER IF NOT EXISTS`.

#### Scenario: User insert alone creates no MySQL account
- **WHEN** only a `database_user_insert` event is processed and no database references the user
- **THEN** no `CREATE USER` is executed

