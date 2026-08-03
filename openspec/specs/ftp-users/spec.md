# ftp-users

## ADDED Requirements

### Requirement: FTP user model on the existing ftp_user table
The system SHALL map the ISPConfig3 `ftp_user` table with a GORM model using explicit column tags and SHALL NOT alter the table schema. Columns in scope: `ftp_user_id`, `sys_userid`, `sys_groupid`, `sys_perm_user`, `sys_perm_group`, `sys_perm_other`, `server_id`, `parent_domain_id`, `username`, `username_prefix`, `password`, `quota_size`, `active`, `uid`, `gid`, `dir`, `quota_files`, `ul_ratio`, `dl_ratio`, `ul_bandwidth`, `dl_bandwidth`, `expires`, `user_type`, `user_config`.

#### Scenario: Model round-trips against MariaDB
- **WHEN** an `ftp_user` row is inserted and reloaded through the GORM model
- **THEN** every column value matches the stored row and no auto-migration is attempted

### Requirement: FTP user validation and field derivation
Create/update of FTP users SHALL enforce the `ftp_user.tform.php` and `ftp_user_edit.php` rules: `parent_domain_id` references a readable `web_domain` of `type = 'vhost'`; `username` is UNIQUE, matches `^[\w\.\-@\+]{0,64}$`, and is stored with the configured `ftpuser_prefix` prepended; `password` is CRYPT-hashed (legacy `$1$`/`$5$`/`$6$` hashes accepted as-is); `quota_size` matches `^(-1|[0-9]{1,10})$` (default `-1`); `active` is `y`/`n` (default `y`); `dir` is non-empty, matches a safe absolute-path regex, contains neither `..` nor `./`, and MUST remain under the parent site's `document_root`. On create (and when the parent site changes) the API SHALL set `server_id`, `uid`, `gid`, `sys_groupid` from the parent `web_domain` (`system_user`/`system_group`/`sys_groupid`) and default `dir` to `document_root` when not supplied. Admin-only advanced fields (`uid`, `gid`, `quota_files`, `ul_ratio`, `dl_ratio`, `ul_bandwidth`, `dl_bandwidth`) are writable by admin only; clients may set `dir` (still under docroot) and `expires`.

#### Scenario: Username prefix applied on create
- **WHEN** a client creates an FTP user with username `alice` and the sites config prefix is `c1_`
- **THEN** the stored `username` is `c1_alice` and `username_prefix` is `c1_`

#### Scenario: Duplicate username rejected
- **WHEN** an FTP user is created with a username that already exists
- **THEN** the API returns a validation error naming the uniqueness rule and no datalog row is written

#### Scenario: Directory outside document root rejected
- **WHEN** a non-admin sets `dir` to a path that is not under the parent site's `document_root`
- **THEN** validation fails and the row is not saved

#### Scenario: Parent site derives server and credentials
- **WHEN** an FTP user is created for a vhost with `system_user=web12`, `system_group=client3`, `document_root=/var/www/clients/client3/web12`
- **THEN** the stored row has matching `uid`/`gid`/`dir`/`server_id`/`sys_groupid`

### Requirement: Client limit on FTP users
When the caller is a client or reseller user, creating an FTP user SHALL enforce `client.limit_ftp_user` (and the reseller's corresponding limit when applicable): `-1` means unlimited; `0` or a count already at/above the limit SHALL reject the create with a clear error. Admins are not limited by these counters.

#### Scenario: Limit zero blocks create
- **WHEN** a client with `limit_ftp_user = 0` attempts to create an FTP user
- **THEN** the API returns an error and no row is written

#### Scenario: Unlimited client may create
- **WHEN** a client with `limit_ftp_user = -1` creates a valid FTP user
- **THEN** the user is created and a datalog insert row is written

### Requirement: riud permissions and datalog on FTP mutations
All FTP user operations SHALL go through the foundation permission scope (`sys_userid`/`sys_groupid`/`sys_perm_*`) and SHALL write `{old,new}` JSON datalog rows targeted at the user's `server_id` with `dbtable = ftp_user`. Password hashes MUST NOT appear in API list/get responses (redacted or omitted).

#### Scenario: Client cannot touch another client's FTP user
- **WHEN** client A updates an FTP user owned by client B's group with empty `sys_perm_other`
- **THEN** the API returns 403 and no datalog row is written

#### Scenario: Create writes datalog for the site's server
- **WHEN** an authorized user creates an FTP user on a site hosted on server 2
- **THEN** a `sys_datalog` insert row is written with `server_id = 2` and `dbtable = ftp_user`

### Requirement: REST API for FTP users
The REST API SHALL expose FTP user CRUD under `/api/sites/ftp-users` (list, get by id, create, update, delete), session/token authenticated, permission-scoped, with swaggo annotations — porting `sites_ftp_user_get/add/update/delete` from `remote.d/sites.inc.php`.

#### Scenario: Create FTP user via API
- **WHEN** `POST /api/sites/ftp-users` is called with a valid parent site, username and password by an authorized user
- **THEN** the user is created with derived fields, a datalog row is written and the new `ftp_user_id` is returned

#### Scenario: List is permission-scoped
- **WHEN** a client lists FTP users
- **THEN** only rows readable under the riud scope are returned

#### Scenario: FTP endpoints visible in Swagger UI
- **WHEN** `/swagger/` is opened after swagger regeneration
- **THEN** the FTP user endpoints are listed with typed request/response schemas

### Requirement: PureFTPd virtual auth (no system accounts)
FTP authentication SHALL rely on PureFTPd MySQL lookups against `ftp_user` (queries equivalent to `pureftpd_mysql.conf.master`: password, uid, gid, dir, quotas, ratios, bandwidth, filtered by `active = 'y'`, matching `server_id`, and `expires IS NULL OR expires > NOW()`). The daemon MUST NOT create, modify, or delete OS accounts for FTP users.

#### Scenario: FTP insert does not call useradd
- **WHEN** the daemon handles `ftp_user_insert`
- **THEN** no `useradd`/`usermod`/`userdel` command is executed

### Requirement: FTP daemon plugin ensures directory and quota file cleanup
On `ftp_user_insert` and `ftp_user_update` the ftp plugin SHALL load the parent `web_domain`, refuse dirs outside `document_root`, and create the directory at mode `0755` owned by the site's `system_user`:`system_group` when it does not exist (toggling web_folder_protection around the mkdir). On dir change it SHALL delete the previous path's `.ftpquota` file when present. On `ftp_user_delete` it SHALL delete `.ftpquota` under the old dir when present and MUST NOT delete the directory tree or site content.

#### Scenario: Missing FTP directory is created under docroot
- **WHEN** `ftp_user_insert` fires with `dir` under the site document root and the path does not exist
- **THEN** the directory is created with ownership of the site system user/group

#### Scenario: Directory outside docroot is refused
- **WHEN** `ftp_user_insert` fires with a `dir` not prefixed by the site's `document_root`
- **THEN** the plugin logs a warning and performs no filesystem change

#### Scenario: Dir change removes old .ftpquota
- **WHEN** `ftp_user_update` changes `dir` from `/var/www/.../web` to `/var/www/.../web/uploads` and the old path has `.ftpquota`
- **THEN** the old `.ftpquota` file is removed

#### Scenario: Delete removes only .ftpquota
- **WHEN** `ftp_user_delete` fires for a user whose dir contains files and `.ftpquota`
- **THEN** only `.ftpquota` is removed and the other files remain

### Requirement: Web module raises FTP events from datalog
The web daemon module SHALL register a table hook for `ftp_user` and raise `ftp_user_insert`, `ftp_user_update`, and `ftp_user_delete` from datalog actions `i`/`u`/`d` respectively (port of `web_module.inc.php` cases for `ftp_user`).

#### Scenario: FTP update datalog dispatches ftp_user_update
- **WHEN** the daemon processes a `sys_datalog` row with `dbtable=ftp_user` and `action=u`
- **THEN** the `ftp_user_update` event is raised with the `{old,new}` payload

### Requirement: FTP Users panel UI under Sites
The panel SHALL show an **FTP Users** section under the Sites module with a searchable list (username, parent site, active, server) and a form matching `ftp_user.tform.php` (main tab + Options tab; admin-only advanced fields hidden for non-admins). All strings SHALL go through i18n (`en.json`).

#### Scenario: Client sees only own FTP users
- **WHEN** a client opens the FTP Users list
- **THEN** only FTP users readable under the riud scope are listed

#### Scenario: Admin Options fields hidden from clients
- **WHEN** a non-admin opens the FTP user form Options tab
- **THEN** `uid`, `gid`, and ratio/bandwidth fields are not rendered
