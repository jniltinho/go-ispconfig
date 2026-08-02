# database-rest-api Specification

## Purpose
TBD - created by archiving change add-database-module. Update Purpose after archive.
## Requirements
### Requirement: Database endpoints
The REST API SHALL expose database operations porting `remote.d/sites.inc.php` `sites_database_add/get/update/delete` and `sites_database_get_all_by_user` under `/api/sites/databases`, session/token authenticated, permission-scoped, returning JSON. Writes SHALL persist `web_database` and a `{old,new}` `sys_datalog` row targeted at the record's `server_id`. Type SHALL be `mysql` only.

#### Scenario: Create database
- **WHEN** `POST /api/sites/databases` is called with valid fields by an authorized user
- **THEN** a `web_database` row is created with the caller's ownership fields (adjusted per parent site), a datalog insert is written and the new `database_id` is returned

#### Scenario: List databases is riud-scoped
- **WHEN** a client lists databases
- **THEN** only rows readable under the caller's riud scope are returned

#### Scenario: Delete writes datalog and removes the row
- **WHEN** `DELETE /api/sites/databases/{id}` is called by an authorized user
- **THEN** the row is deleted and a `web_database` delete datalog row is written for its `server_id`

### Requirement: Database user endpoints
The REST API SHALL expose `sites_database_user_add/get/update/delete` semantics under `/api/sites/database-users`. Password fields are write-only: GET responses SHALL omit `database_password`, `database_password_sha2`, `database_password_mongo` and `database_password_postgres`. Empty password on update means leave unchanged.

#### Scenario: Create database user hashes password
- **WHEN** `POST /api/sites/database-users` is called with a plaintext password
- **THEN** the stored `database_password` is the MySQL native hash form and `database_password_sha2` is populated; the response does not include the plaintext

#### Scenario: Update without password keeps hashes
- **WHEN** a user is updated with an empty password field
- **THEN** existing hash columns are unchanged

#### Scenario: User delete clears references
- **WHEN** a database user still referenced as `database_user_id` or `database_ro_user_id` is deleted
- **THEN** those database columns are set to null via datalog updates and a `web_database_user` delete datalog row is written

### Requirement: Database validation (tform parity)
Database create/update SHALL enforce `database.tform.php` and `database_edit.php` rules: `server_id` required and a DB-capable server (`db_server=1`); `parent_domain_id` required, referencing an accessible `web_domain` with `type=vhost`; `database_name` NOTEMPTY matching `^[a-zA-Z0-9_]{2,64}$` after prefix application, unique per `server_id`, not in the blacklist (panel DB name, `mysql`); `database_charset` one of empty / `latin1` / `utf8` / `utf8mb4`; `remote_access` and `active` are `y`/`n`; `remote_ips` empty or a comma-separated list of valid IPs; `database_quota` integer (default `-1`); `database_user_id` required; `type` must be `mysql`; `server_id` immutable after create; `database_charset` immutable after create; non-admin cannot rename `database_name` after create; name length after prefix ≤ 64.

#### Scenario: Duplicate name on same server rejected
- **WHEN** a database is created with a `database_name` already used on that `server_id`
- **THEN** the API returns a validation error naming the uniqueness rule and no rows are written

#### Scenario: Missing parent site rejected
- **WHEN** a database is created with `parent_domain_id = 0`
- **THEN** the API returns a validation error and no datalog row is written

#### Scenario: Invalid remote IP list rejected
- **WHEN** `remote_ips` contains a non-IP token
- **THEN** the API returns a field error for `remote_ips`

#### Scenario: Non-admin cannot rename database
- **WHEN** a client updates `database_name` to a different value
- **THEN** the change is rejected and the stored name is unchanged

### Requirement: Database user validation
Database-user create/update SHALL enforce `database_user.tform.php` rules: `database_user` NOTEMPTY, UNIQUE, matching `^[a-zA-Z0-9_]{2,64}$` after prefix; password required on create and checked by the shared password-strength validator; `server_id` a DB-capable server.

#### Scenario: Weak password rejected
- **WHEN** a database user is created with a password failing the strength policy
- **THEN** the API returns a validation error and no row is written

#### Scenario: Invalid username regex rejected
- **WHEN** a username contains `-` or spaces
- **THEN** the API returns a regex validation error

### Requirement: Name prefixes
On create the API SHALL apply global sites config prefixes `dbname_prefix` / `dbuser_prefix` (with client placeholder expansion), store the applied prefix in `database_name_prefix` / `database_user_prefix`, and persist the full prefixed name in `database_name` / `database_user`.

#### Scenario: Prefix applied on database create
- **WHEN** a client creates a database with suffix `app` and the configured prefix expands to `c1_`
- **THEN** the stored `database_name` is `c1_app` and `database_name_prefix` is `c1_`

### Requirement: Client and reseller limits
On create the API SHALL enforce for non-admin callers: `client.limit_database` (count of mysql `web_database` for the group), `client.limit_database_user` (count of `web_database_user`), and `client.limit_database_quota` (sum of `database_quota` in MB, including the new value; reject zero quota when the client limit is positive; reject unlimited `-1` per-DB when the client limit is finite). Reseller limits apply when `parent_client_id > 0`. Clients may only use `server_id` values listed in `client.db_servers`. Limit `-1` means unlimited.

#### Scenario: Database count limit blocks create
- **WHEN** a client with `limit_database = 1` already has one mysql database and attempts another
- **THEN** the API returns a limit error and no row is written

#### Scenario: Quota sum exceeded
- **WHEN** a client with `limit_database_quota = 100` has used 80 MB and submits `database_quota = 50`
- **THEN** the API returns a quota error (or clamps per product choice documented as PHP-parity free-quota message) and does not exceed the limit

#### Scenario: Disallowed server rejected
- **WHEN** a client submits a `server_id` not in `db_servers`
- **THEN** the API returns an not-allowed-server error

### Requirement: Parent site ownership inheritance
When `parent_domain_id > 0` the API SHALL set the database's `sys_groupid` from the parent `web_domain` (port of `sites_database_plugin`). Assigned `database_user_id` / `database_ro_user_id` MUST belong to the same `sys_groupid` as the parent site (admin path check).

#### Scenario: Database inherits site group
- **WHEN** a database is created under a site owned by group 42
- **THEN** the stored `sys_groupid` is 42

#### Scenario: Cross-client user assignment rejected
- **WHEN** an admin assigns a database user from another client group than the parent site
- **THEN** the API returns an error and no row is written

### Requirement: User update fans datalog to database servers
When a database user is updated (name or password), the API SHALL write additional `web_database_user` UPDATE datalog rows for each distinct `server_id` of databases that reference the user as rw or ro (port of `sites_database_user_update`), so every DB server daemon applies the change.

#### Scenario: Multi-server fan-out
- **WHEN** a user referenced by databases on server_id 1 and 2 has a password change
- **THEN** datalog contains UPDATE entries covering those server ids for that user

### Requirement: riud permissions on all mutations
All database and database-user operations SHALL go through the foundation permission scope (`sys_userid` / `sys_groupid` / `sys_perm_*`). Clients SHALL NOT read or mutate another client's rows when `sys_perm_other` is empty.

#### Scenario: Client cannot update another client's database
- **WHEN** client A updates a database owned by client B's group with empty `sys_perm_other`
- **THEN** the API returns 403 and no datalog row is written

### Requirement: Swagger documentation for database endpoints
Every database and database-user endpoint SHALL carry swaggo annotations (summary, params, request/response models, security, error codes) and appear in the embedded Swagger UI; CI SHALL fail when generated swagger output is stale.

#### Scenario: Endpoints visible in Swagger UI
- **WHEN** `/swagger/` is opened
- **THEN** the Sites database and database-user endpoints are listed with typed request/response schemas (password fields write-only where applicable)

