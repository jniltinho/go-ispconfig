# shell-users

## ADDED Requirements

### Requirement: Shell user model on the existing shell_user table
The system SHALL map the ISPConfig3 `shell_user` table with a GORM model using explicit column tags and SHALL NOT alter the table schema. Columns in scope: `shell_user_id`, `sys_userid`, `sys_groupid`, `sys_perm_user`, `sys_perm_group`, `sys_perm_other`, `server_id`, `parent_domain_id`, `username`, `username_prefix`, `password`, `quota_size`, `active`, `puser`, `pgroup`, `shell`, `dir`, `chroot`, `ssh_rsa`.

#### Scenario: Model round-trips against MariaDB
- **WHEN** a `shell_user` row is inserted and reloaded through the GORM model
- **THEN** every column value matches the stored row and no auto-migration is attempted

### Requirement: Shell user validation and field derivation
Create/update of shell users SHALL enforce the `shell_user.tform.php` and `shell_user_edit.php` rules: `parent_domain_id` references a readable `web_domain` of `type = 'vhost'` and is immutable after create; `username` is UNIQUE, matches `^[\w\.\-]{0,32}$` after prefix, is checked against the shell-user blacklist and allowed-username rules, and MUST NOT exceed 32 characters after the configured `shelluser_prefix` is applied; `password` is CRYPT-hashed (legacy hashes accepted as-is) subject to the global `ssh_authentication` mode; `chroot` is one of `no`/empty or `jailkit` (and constrained by the client's `ssh_chroot` allow-list when present); `quota_size` is required integer default `-1`; `active` is `y`/`n`; `ssh_rsa` is trimmed; `dir` is an absolute path under the parent document root without `..` / `./`. On create the API SHALL set `server_id`, `puser`, `pgroup`, `sys_groupid` from the parent site and default `dir` to `document_root`, `shell` to `/bin/bash`. Admin-only advanced fields (`puser`, `pgroup`, `shell`, free-form `dir`) are writable by admin only.

#### Scenario: Blacklisted username rejected
- **WHEN** a shell user is created with username `root` (or another blacklist entry) after prefixing
- **THEN** the API returns a validation error and no row is written

#### Scenario: Username length after prefix exceeds 32
- **WHEN** the configured prefix plus the supplied name exceeds 32 characters
- **THEN** validation fails with the length error

#### Scenario: Parent domain locked after create
- **WHEN** an update attempts to change `parent_domain_id` of an existing shell user
- **THEN** the field is rejected or ignored and the stored parent remains unchanged

#### Scenario: Key-only auth clears password
- **WHEN** global `ssh_authentication` is `key` and a shell user is saved with both password and ssh_rsa
- **THEN** the stored password is empty/null and `ssh_rsa` is kept

### Requirement: Client limit on shell users
When the caller is a client or reseller user, creating a shell user SHALL enforce `client.limit_shell_user` (schema default `0`): `-1` unlimited; at/above the limit or `0` rejects create. Admins are not limited by these counters.

#### Scenario: Default limit blocks client create
- **WHEN** a client with `limit_shell_user = 0` attempts to create a shell user
- **THEN** the API returns an error and no row is written

### Requirement: riud permissions and datalog on shell mutations
All shell user operations SHALL go through the foundation permission scope and SHALL write `{old,new}` JSON datalog rows targeted at the user's `server_id` with `dbtable = shell_user`. Password hashes and full private key material MUST NOT be returned on list/get (password redacted; `ssh_rsa` public keys may be shown as stored for edit).

#### Scenario: Cross-client update denied
- **WHEN** client A updates a shell user owned by client B with empty `sys_perm_other`
- **THEN** the API returns 403 and no datalog row is written

### Requirement: REST API for shell users
The REST API SHALL expose shell user CRUD under `/api/sites/shell-users` (list, get by id, create, update, delete), session/token authenticated, permission-scoped, with swaggo annotations — porting `sites_shell_user_get/add/update/delete`.

#### Scenario: Create shell user via API
- **WHEN** `POST /api/sites/shell-users` is called with a valid parent site, username and credentials by an authorized admin or entitled client
- **THEN** the user is created with derived fields, a datalog row is written and the new `shell_user_id` is returned

#### Scenario: Shell endpoints visible in Swagger UI
- **WHEN** `/swagger/` is opened after swagger regeneration
- **THEN** the shell user endpoints are listed with typed request/response schemas

### Requirement: Web module raises shell events from datalog
The web daemon module SHALL register a table hook for `shell_user` and raise `shell_user_insert`, `shell_user_update`, and `shell_user_delete` from datalog actions `i`/`u`/`d` respectively (port of `web_module.inc.php`).

#### Scenario: Shell insert datalog dispatches shell_user_insert
- **WHEN** the daemon processes a `sys_datalog` row with `dbtable=shell_user` and `action=i`
- **THEN** the `shell_user_insert` event is raised with the `{old,new}` payload

### Requirement: Security kill-switch for shell plugins
Shell base (and jailkit) plugins SHALL no-op with a warning when security config `permissions.allow_shell_user` is not `yes`.

#### Scenario: Shell users disabled by policy
- **WHEN** `allow_shell_user` is set to `no` and a `shell_user_insert` event fires
- **THEN** no system account is created and a warning is logged

### Requirement: System account lifecycle on shell_user events
The shell base plugin SHALL create, update and delete real system accounts (port of `shelluser_base_plugin.inc.php`), using the foundation command runner:

- **Guards**: `dir` under site `document_root` and allowed path; username/puser/pgroup not root; parent system user UID `> 499`.
- **Insert**: create `dir/home` (root:root `0755`) and home `dir/home/<username>` (puser:pgroup `0750`); `useradd -d <home> -g <pgroup> -o -s <shell> -u <uid_of_puser> <username>`; set password with `chpasswd -e` when hash non-empty; write `.bash_history`, `.profile`, `.bashrc.d`, `.local/bin`; symlink `web`/`log`/`private`; run SSH key setup; if `chroot = 'jailkit'`, temporarily set shell `/bin/false` and lock the account for the jailkit plugin.
- **Update**: if the OS user exists, `usermod` for group/home/shell/password/rename and reconcile home layout; if missing, fall through to insert.
- **Delete** (non-jailkit only): optionally stop PHP-FPM when the site uses it, `killall -u` + `userdel -f`, restart PHP-FPM; remove owned dotfiles under the home when no other `shell_user` row still uses the same `dir`. Jailkit-chrooted users are deleted by the jailkit plugin instead.
- Inactive users force `shell = /bin/false`.

#### Scenario: Shell insert creates a non-unique UID account
- **WHEN** `shell_user_insert` fires for an active non-jailkit user whose parent system user has UID 5001
- **THEN** `useradd` is invoked with `-o -u 5001` and the home under `dir/home/<username>`

#### Scenario: Dir outside docroot aborts insert
- **WHEN** `shell_user_insert` fires with `dir` not under the site document root
- **THEN** the plugin logs a warning and does not create an account

#### Scenario: Missing OS user on update is re-created
- **WHEN** `shell_user_update` fires and the old username is not present in `/etc/passwd`
- **THEN** the insert path runs and creates the account

#### Scenario: Non-jailkit delete removes the OS user
- **WHEN** `shell_user_delete` fires for a user with `chroot != 'jailkit'` and UID > 499
- **THEN** `userdel` is executed for that username

#### Scenario: Jailkit delete is left to the jailkit plugin
- **WHEN** `shell_user_delete` fires for a user with `chroot = 'jailkit'`
- **THEN** the base plugin does not call `userdel` (jailkit plugin owns removal)

### Requirement: SSH authorized_keys setup
On shell insert/update the plugin SHALL rebuild `authorized_keys` under the effective home (non-jailkit: `dir/home/<username>/.ssh`; jailkit homes use the jail home path): merge all non-empty `ssh_rsa` values of shell users on the same parent site plus the client `ssh_rsa` when present, dedupe, ensure `.ssh` mode `0700` and `authorized_keys` mode `0600` with correct ownership (port of `_setup_ssh_rsa`).

#### Scenario: Multiple shell users share site keys
- **WHEN** two shell users on the same site each have an `ssh_rsa` public key and either is updated
- **THEN** the resulting `authorized_keys` contains both keys (deduplicated)

### Requirement: Shell Users panel UI under Sites
The panel SHALL show a **Shell Users** section under the Sites module with a searchable list (username, parent site, chroot, active, server) and a form matching `shell_user.tform.php` (main tab including chroot and ssh_rsa; admin Options tab for puser/pgroup/shell/dir). Parent site is not editable after create. All strings through i18n (`en.json`).

#### Scenario: Chroot selector limited by client policy
- **WHEN** a client whose `ssh_chroot` allow-list is `no` opens the shell user form
- **THEN** the jailkit option is not offered

#### Scenario: Admin Options tab for advanced fields
- **WHEN** an admin opens an existing shell user
- **THEN** the Options tab exposes `puser`, `pgroup`, `shell`, and `dir`
