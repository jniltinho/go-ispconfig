# client-management

## ADDED Requirements

### Requirement: Client and reseller domain models on immutable schema
The system SHALL map the existing ISPConfig3 tables `client`, `sys_user`, `sys_group` (and, for related capabilities, `client_template`, `client_template_assigned`, `client_message_template`, `country`) via GORM models with explicit column tags identical to `internal/database/ispconfig3.sql`. Column names, types and defaults MUST NOT be altered. Numeric `limit_*` fields use ISPConfig semantics: `-1` unlimited, `0` none/disabled (except `y`/`n` enum limits).

#### Scenario: Client model round-trips all limit columns
- **WHEN** a `client` row is inserted and reloaded including `limit_web_domain`, `limit_dns_zone`, `limit_client`, `parent_client_id`, `template_master` and contact fields
- **THEN** every column value matches what was stored and the table name is `client`

#### Scenario: No schema migration is required
- **WHEN** this module is deployed against a database created by the foundation migrate
- **THEN** no ALTER TABLE runs for client-related tables

### Requirement: Reseller vs client distinction via limit_client
A record with `limit_client = 0` is a **client**; a record with `limit_client != 0` is a **reseller**. Only one reseller nesting level is allowed: a child with `limit_client != 0` MUST NOT have `parent_client_id` pointing at another reseller. `parent_client_id = 0` means owned by admin.

#### Scenario: Reseller cannot own another reseller
- **WHEN** a create/update sets `parent_client_id` to a client whose `limit_client != 0` and also sets the new record's `limit_client != 0`
- **THEN** validation fails and no row is written

#### Scenario: Client under reseller is accepted
- **WHEN** a create sets `parent_client_id` to a reseller and `limit_client = 0`
- **THEN** the client is stored with that parent

#### Scenario: Non-reseller parent is rejected or cleared
- **WHEN** `parent_client_id` points at a client with `limit_client = 0`
- **THEN** the API rejects the parent (or clears it per remote-API parity) and does not create a broken hierarchy

### Requirement: Automatic sys_group and sys_user provisioning on create
Creating a client or reseller SHALL, in one database transaction, also create: (1) a `sys_group` with `name = client.username` and `sys_group.client_id = client.client_id`; (2) a `sys_user` with matching `username`, hashed `passwort`, `modules` (interface modules, plus `client` when `limit_client > 0`), `startmodule`, `typ = 'user'`, `active = 1`, `language`, `groups` and `default_group` set to the new groupid, and `client_id` set. When `parent_client_id > 0`, the parent reseller's `sys_user.groups` CSV SHALL include the new groupid. Ownership fields on the `client` row SHALL reflect the creating admin or the parent reseller.

#### Scenario: New client gets login and group
- **WHEN** an admin creates a client with username `acme` and a password
- **THEN** one `client`, one `sys_group` (name `acme`, client_id set) and one `sys_user` (username `acme`, default_group = that group, client_id set) exist and the password is stored hashed (never plaintext)

#### Scenario: Reseller modules include client
- **WHEN** a reseller (`limit_client = -1`) is created
- **THEN** the linked `sys_user.modules` contains the `client` module token

#### Scenario: Parent reseller gains group membership
- **WHEN** a client is created under `parent_client_id = R`
- **THEN** the reseller R's `sys_user.groups` list includes the new client's groupid

### Requirement: sys_user and sys_group stay in sync on update
Updates SHALL propagate username changes to `sys_user.username` and `sys_group.name`, password changes to `sys_user.passwort` and `last_password_change`, language to `sys_user.language`, and `limit_client` changes to `sys_user.modules` (add/remove `client`). Parent reassignment (admin only) SHALL move group membership between resellers as in `client_edit.php`.

#### Scenario: Username rename updates group and login
- **WHEN** a client's username changes from `acme` to `acme2`
- **THEN** `sys_user.username` and `sys_group.name` become `acme2` and the client row username is `acme2`

#### Scenario: Password change updates sys_user only when provided
- **WHEN** a client update omits password
- **THEN** `sys_user.passwort` is unchanged; when a new password is provided it is rehashed and stored

### Requirement: Client field validation
Create/update SHALL enforce rules ported from `client.tform.php` / `reseller.tform.php`: `username` NOTEMPTY, UNIQUE among clients and non-colliding with existing `sys_user.username`, matching `^[\w.\-_]{0,64}$`; `password` strength via the foundation password policy on create (and on update when set); `email` valid when non-empty; `country` empty or a `country.iso` code; integer `limit_*` fields; `locked`/`canceled`/`can_use_api` in `{n,y}`; `language` default `en`; `usertheme` default `default`. Contact fields (`contact_name`, company, address, bank fields, notes) accept the same lengths as the schema.

#### Scenario: Duplicate username rejected
- **WHEN** a client is created with a username that already exists on another client or sys_user
- **THEN** the API returns a validation error for `username` and no rows are written

#### Scenario: Weak password rejected
- **WHEN** a client is created with a password that fails the configured password policy
- **THEN** the API returns a validation error and no client/sys_user is created

### Requirement: Locked and canceled status
Setting `locked = y` SHALL deactivate the linked `sys_user` (`active = 0`); unlocking reactivates it. Setting `canceled = y` SHALL follow the PHP cancel helper semantics (deactivate login; resource soft-disable where the foundation already supports it). Both fields are admin/reseller controllable within scope.

#### Scenario: Locking a client blocks panel login
- **WHEN** `locked` is set to `y` on a client
- **THEN** the linked `sys_user.active` becomes `0` and subsequent panel login for that username fails

### Requirement: Delete removes identity rows and optionally cascades resources
`client_delete` SHALL remove `client_template_assigned` rows, the client's `sys_user` row(s), the client's `sys_group`, detach that group from any parent reseller `sys_user.groups`, and delete the `client` row, writing datalog entries so `client_delete` events fire. `client_delete_everything` SHALL additionally datalog-delete owned resource rows for the client's group across the ISPConfig table list used by PHP (`web_domain`, `web_folder`, `web_folder_user`, `dns_soa`, `dns_rr`, `dns_slave`, mail/ftp/shell/db/cron tables when present, child clients, …) before removing identity rows.

#### Scenario: Simple delete removes login
- **WHEN** `client_delete` runs for a client with no owned sites
- **THEN** the `client`, `sys_user` and `sys_group` rows are gone and a `client` datalog action `d` exists

#### Scenario: Delete everything removes owned zones and sites
- **WHEN** `client_delete_everything` runs for a client that owns a `web_domain` and a `dns_soa`
- **THEN** those resource rows are datalog-deleted (plugins will tear down configs) and then the client identity rows are removed

### Requirement: riud permissions and datalog on all client mutations
All client/reseller mutations SHALL go through foundation permission scopes and SHALL write `{old,new}` JSON datalog rows. Because `client` has no `server_id` column, journal rows SHALL use `server_id = 0` (broadcast). Resellers SHALL only read/update/delete clients whose `sys_groupid` is in their effective group list (including children resolved via `parent_client_id`). Admins SHALL access all clients. Password and private key fields MUST NEVER appear in list/get JSON responses.

#### Scenario: Reseller cannot update another reseller's client
- **WHEN** reseller A updates a client owned by reseller B
- **THEN** the API returns 403 and no datalog row is written

#### Scenario: Password omitted from get response
- **WHEN** a client is fetched by id
- **THEN** the JSON body has no `password` field (and no `passwort` / key material)
