# Design: FTP and Shell Users module

## Context

ISPConfig3's site-access stack is four pieces glued by `sys_datalog`:

1. `interface/web/sites/{ftp_user,shell_user}_{list,edit,del}.php` + tforms — write `ftp_user` and `shell_user` with `{old,new}` datalog diffs; `remote.d/sites.inc.php` exposes the same operations (`sites_ftp_user_*`, `sites_shell_user_*`).
2. `server/mods-available/web_module.inc.php` — already owns the table hooks for `ftp_user` and `shell_user` (alongside `web_domain` / folders), translating datalog actions into six named events: `ftp_user_insert|update|delete`, `shell_user_insert|update|delete`.
3. `server/plugins-available/ftpuser_base_plugin.inc.php` (141 lines) — on FTP events ensures the home directory exists inside the site document root and cleans `.ftpquota` on dir change/delete. **No system accounts**: PureFTPd authenticates against the `ftp_user` table via MySQL (`pureftpd_mysql.conf.master`).
4. `server/plugins-available/shelluser_base_plugin.inc.php` (696 lines) + `shelluser_jailkit_plugin.inc.php` (854 lines) — create/update/delete real system accounts (`useradd`/`usermod`/`userdel`/`chpasswd`), set up home dirs and SSH keys; when `chroot = 'jailkit'`, build/update the jail under the website root and relocate the user into it.

The foundation and the web/nginx module already provide everything this change plugs into: datalog consumer with table-hook/event registries, getconf, services registry, riud GORM scopes, validation engine, declarative entity REST framework (`RegisterEntity`), panel skeleton under Sites. The DB tables exist (byte-identical ISPConfig3 schema); only GORM models and the plugins/API/UI are missing.

Jailkit fields on `web_domain` (`jailkit_chroot_app_sections`, `jailkit_chroot_app_programs`, `delete_unused_jailkit`, `last_jailkit_update`, `last_jailkit_hash`) are already persisted by the web module as **stored-only**; this change owns all jail lifecycle logic.

## Goals / Non-Goals

**Goals:**
- Behavior-faithful port of `ftpuser_base_plugin`, `shelluser_base_plugin`, and `shelluser_jailkit_plugin`: same events, same filesystem side-effects, same PureFTPd MySQL-auth model.
- API/UI parity with ISPConfig Sites FTP-User and Shell-User screens: CRUD, username prefixes, parent-site derivation of uid/gid/dir, admin advanced options, client limits (`limit_ftp_user`, `limit_shell_user`), riud isolation.
- PureFTPd reads `ftp_user` directly; the daemon never creates OS accounts for FTP users.
- Jailkit ownership lives entirely here (config from server `[jailkit]` section + per-site overrides on `web_domain`).

**Non-Goals:**
- ProFTPd/vsftpd, SFTP-only servers, WebDAV users (`webdav_user`), FTP traffic dashboards (`ftp_traffic` is read-only/later), PureFTPd TLS cert management beyond installer wiring (see proposal).
- Installer steps (`configure_pureftpd` / `configure_jailkit`) — tracked as a **Modified Capability of `add-installer-cli`**, listed here for visibility only.
- No schema changes of any kind.

## Decisions

### D1 — Extend the existing web module for table hooks; plugins in dedicated packages
PHP registers `ftp_user` / `shell_user` hooks inside `web_module.inc.php` and raises the six events from `process()`. The Go `internal/web` module already documents that unhooked tables are additive later. This change **extends** `internal/web` to register table hooks for `ftp_user` and `shell_user` and announce the six events (`ftp_user_insert|update|delete`, `shell_user_insert|update|delete`), gated on `server.web_server = 1` + module enablement (same gate as the rest of the web module).

Plugins live in dedicated packages wired in the daemon bootstrap:
- `internal/ftp` — port of `ftpuser_base_plugin`
- `internal/shell` — port of `shelluser_base_plugin`
- `internal/jailkit` — port of `shelluser_jailkit_plugin` (separate package so the capability stays isolatable)

Keeping the two-level dispatch (hook → named event → plugin) preserves the foundation registry contract and matches the nginx/dns pattern.
*Alternative*: a brand-new `sites_access` module — rejected: breaks event-name and ownership parity with PHP's `web_module`, and forces two modules to share the web-server role gate.

### D2 — PureFTPd MySQL auth; daemon only ensures directory + quota side-effects
FTP users are **virtual**: PureFTPd (package `pure-ftpd-mysql`) authenticates via the SQL queries in `install/tpl/pureftpd_mysql.conf.master` against `ftp_user` (`active = 'y'`, matching `server_id`, optional `expires > NOW()`), reading `password`, `uid`, `gid`, `dir`, `quota_size`/`quota_files`, `ul_ratio`/`dl_ratio`, `ul_bandwidth`/`dl_bandwidth`. The daemon plugin on insert/update:
1. Loads the parent `web_domain` by `parent_domain_id`.
2. Rejects paths outside `document_root` (prefix check).
3. Creates `dir` at `0755` owned by `web.system_user`:`web.system_group` when missing (with web_folder_protection toggle around the mkdir).
4. On dir change, deletes the old `.ftpquota` file; on delete, deletes `.ftpquota` only (never the user's files or a system account).

Rationale: ISPConfig never maps FTP users to `/etc/passwd`; inventing system accounts would break PureFTPd MySQL auth and migration parity.

### D3 — Shell users are real system accounts (non-unique UID of the site)
Port of `shelluser_base_plugin`:
- Guards: security config `permissions.allow_shell_user == 'yes'` (default yes, already in `internal/auth/policy.go`); dir must be under site `document_root` and an allowed path; username/puser/pgroup must not be root; UID of parent system user must be `> 499` (`min_uid`).
- Insert: create `dir/home` (root:root 0755) and `dir/home/<username>` (puser:pgroup 0750); `useradd -d <homedir> -g <pgroup> -o -s <shell> -u <uid_of_puser> <username>` (non-unique UID shared with the site system user); set password via `chpasswd -e` when the stored hash is non-empty; write `.bash_history`, `.profile`, `.bashrc.d`, `.local/bin`; symlink `web`/`log`/`private` into the home; call `_setup_ssh_rsa`; if `chroot == 'jailkit'`, temporarily lock the user (`usermod -s /bin/false -L`) so the jailkit plugin can finish setup.
- Update: `usermod` for rename/homedir/shell/password; recreate missing home layout; if the OS user is missing, fall through to insert (PHP parity).
- Delete: only for non-jailkit users (jailkit delete is owned by the jailkit plugin); stop PHP-FPM briefly when the site uses php-fpm so the user is not busy, `killall -u` + `userdel -f`, restart PHP-FPM; remove owned dotfiles under the home when no other shell_user still references the same `dir`.
- Inactive users force `shell = /bin/false`; jailkit inserts also force `/bin/false` until the jailkit plugin sets `/usr/sbin/jk_chrootsh`.

All shell-outs go through the foundation `engine.Runner` (argv slices only, no shell interpolation).

### D4 — Jailkit chroot lifecycle as a second subscriber on shell_user events
When `chroot = 'jailkit'` the jailkit plugin (after the base plugin has created the OS user):
1. Loads server config section `[jailkit]` (`jailkit_chroot_home`, `jailkit_chroot_app_sections`, `jailkit_chroot_app_programs`, `jailkit_chroot_cron_programs`, `jailkit_hardlinks`, …) and overlays non-empty `web_domain.jailkit_chroot_app_sections` / `jailkit_chroot_app_programs`, appending `server_php.php_jk_section` when present.
2. Builds or updates the jail under `dir` (`jk_init` sections + `jk_cp` programs); stamps `web_domain.last_jailkit_update` / `last_jailkit_hash` (md5 of sorted sections+programs+cron programs) and skips rebuild when the hash is unchanged.
3. Adds the user into the jail passwd/shadow and relocates home to `dir + jailkit_chroot_home` (`/home/[username]` by default); sets shell to `/usr/sbin/jk_chrootsh` and unlocks the account.
4. On delete: `killall -u` + `userdel -f`, remove jail passwd/shadow lines, delete jailed home; if `web_domain.delete_unused_jailkit = 'y'` and no remaining jailkit shell users share the site, tear down the jail tree (respecting do-not-remove paths).

The web module continues to store the jailkit columns only; it never runs jail tools.

### D5 — Interface-side field derivation (server_id, uid/gid/dir, ownership)
Port of `ftp_user_edit.php` / `shell_user_edit.php` `onSubmit` / `onAfterInsert` / `onAfterUpdate` into the API entity hooks (same pattern as `webDomainAfterInsert` in `internal/api/sites.go`):

| Field | Source on create (and when parent site changes) |
|---|---|
| `server_id` | parent `web_domain.server_id` |
| `sys_groupid` | parent `web_domain.sys_groupid` |
| FTP `uid` / `gid` | parent `system_user` / `system_group` |
| FTP `dir` (default) | parent `document_root` (admin may override under docroot; clients may set a subpath still under docroot) |
| Shell `puser` / `pgroup` | parent `system_user` / `system_group` |
| Shell `dir` (default) | parent `document_root` |
| Shell `shell` (default) | `/bin/bash` |
| Shell `chroot` (default) | empty / `no` |

Username prefixes come from the global sites config (`ftpuser_prefix`, `shelluser_prefix` with `{client_id}`-style placeholders), stored in `username_prefix` and prepended to the user-supplied name. Shell usernames are limited to 32 chars after prefix, matched against `^[\w\.\-]{0,32}$`, checked against an embedded shell-user blacklist (port of `interface/lib/shelluser_blacklist`), and rejected when not an allowed system username. FTP usernames match `^[\w\.\-@\+]{0,64}$` and are UNIQUE.

Passwords use the same CRYPT path as folder users (`auth.CryptPassword` / legacy-hash passthrough) so PureFTPd `MYSQLCrypt crypt` and `chpasswd -e` both accept the stored hash. Responses MUST never return the password hash (strip or redact on read).

Shell `quota_size` defaults to `-1`; site `hd_quota` remains owned by the web module — this module does not introduce a separate quota subsystem (proposal: honor `ftp_user.quota_size`; shell quota inherits site hd_quota semantics already applied at the site level).

### D6 — Declarative entity API under `/api/sites`
Register two entities on the existing Sites group via the foundation `RegisterEntity` framework (same shape as web domains/folders in `internal/api/sites.go`):

- `FTPUser` → `/api/sites/ftp-users` (table `ftp_user`, pk `ftp_user_id`)
- `ShellUser` → `/api/sites/shell-users` (table `shell_user`, pk `shell_user_id`)

Semantics port `sites_ftp_user_{get,add,update,delete}` and `sites_shell_user_{get,add,update,delete}` plus list. An additional FTP helper endpoint mirrors `sites_ftp_user_server_get` (lookup server by FTP username) if useful for clients; otherwise it can be derived from the get payload's `server_id`.

Tabs mirror the tforms:
- FTP: main tab (parent site, username, password, quota_size, active) + Options tab (`dir`; admin also `uid`, `gid`, `quota_files`, `ul_ratio`, `dl_ratio`, `ul_bandwidth`, `dl_bandwidth`; both see `expires`).
- Shell: main tab (parent site, username, password, chroot no/jailkit, quota_size, active, ssh_rsa) + admin Options tab (`puser`, `pgroup`, `shell`, `dir`). Parent domain is immutable after create for shell users (PHP `edit_disabled`).

Client limit checks: before create, enforce `client.limit_ftp_user` / `limit_shell_user` when the caller is a client/reseller (soft dependency on `add-client-module` — if limits are present on the client row, enforce; `-1` means unlimited, `0` means none allowed). Swaggo annotations on every endpoint; regenerate swagger after handlers land.

### D7 — SSH key management (`_setup_ssh_rsa`)
On shell insert/update the base (and jailkit) plugin rebuilds `authorized_keys` under the effective home (non-jailkit: `dir/home/<username>/.ssh`; jailkit: `dir + jailkit_chroot_home/.ssh`):
- Collect all non-empty `shell_user.ssh_rsa` for the same `parent_domain_id`, plus the owning client's `client.ssh_rsa` when present.
- Merge with any existing keys, dedupe, write `authorized_keys` mode `0600` under `.ssh` mode `0700`, owned by the shell user / pgroup.
- Global `ssh_authentication` mode (password / key / both) from system config: password-only clears `ssh_rsa` on save; key-only clears password on save (PHP `shell_user_edit.php` parity).

### D8 — UI under the existing Sites module
Add sidebar sections **FTP Users** and **Shell Users** under Sites (`frontend/src/modules.ts`), with DataTable lists and TabbedForm editors driven by the entity metadata endpoint (same pattern as `WebDomainList` / `EntityForm`). English i18n keys in `en.json`. Client-side validation mirrors API rules; field errors from the API surface inline. Admin-only fields hidden for non-admin form metadata (entity `AdminOnly` already supported).

### D9 — No PureFTPd reload required for user CRUD
Because PureFTPd queries MySQL on every auth, FTP CRUD needs no service reload. Shell/jailkit changes apply immediately via user tools. Optional PureFTPd service registration is reserved for installer/config changes, not user events.

### D10 — Schema immutability and models
GORM models map 1:1 onto the existing tables with explicit `gorm:"column:..."` tags — **no migrations, no column renames**:

`ftp_user`: `ftp_user_id`, `sys_userid`, `sys_groupid`, `sys_perm_user|group|other`, `server_id`, `parent_domain_id`, `username`, `username_prefix`, `password`, `quota_size`, `active`, `uid`, `gid`, `dir`, `quota_files`, `ul_ratio`, `dl_ratio`, `ul_bandwidth`, `dl_bandwidth`, `expires`, `user_type`, `user_config`.

`shell_user`: `shell_user_id`, `sys_*`, `server_id`, `parent_domain_id`, `username`, `username_prefix`, `password`, `quota_size`, `active`, `puser`, `pgroup`, `shell`, `dir`, `chroot`, `ssh_rsa`.

`ftp_traffic` is **not** modeled for writes in this change (non-goal: traffic dashboards).

## Risks / Trade-offs

- [Shell plugin runs as root and can destroy accounts] → hard guards ported from PHP: `min_uid > 499`, allowed-user/group checks, allowed-path checks, security config kill-switch `allow_shell_user`; all exec via `engine.Runner` with argv only; integration tests with a fake runner.
- [Jailkit rebuild is expensive and can race concurrent shell_user events on the same site] → hash short-circuit (`last_jailkit_hash`) skips unchanged jails; sequential datalog processing per server already serializes events in one daemon.
- [PHP-FPM stop around shell userdel can briefly interrupt the site] → same as ISPConfig; scoped to php-fpm sites only; restart is best-effort and logged.
- [Client limits depend on `add-client-module`] → enforce when `limit_*` columns are populated; defaults in schema (`limit_ftp_user = -1`, `limit_shell_user = 0`) already match ISPConfig so a missing client UI does not block this module.
- [Path escape if dir validation is weaker than PHP] → API validators + daemon re-check that `dir` is under `document_root` and rejects `..` / `./` segments; non-admin path overrides that fail validation are reset to document_root (PHP `onAfterUpdate` safety net).
- [Password hash format mismatch with PureFTPd] → store only crypt-style hashes (`$1$`/`$5$`/`$6$`) that `MYSQLCrypt crypt` accepts; never bcrypt for `ftp_user.password`.

## Migration Plan

- Ships as code only — no schema change. Migrated ISPConfig3 databases keep existing `ftp_user` / `shell_user` rows as-is.
- Fresh installs: installer change (`add-installer-cli`) installs `pure-ftpd-mysql` + `jailkit`, writes `pureftpd_mysql.conf` from the master template, and seeds the `[jailkit]` server config section; until then operators must provision PureFTPd/jailkit manually for daemon side-effects to matter.
- After cutover, the first datalog event per FTP/shell row re-applies directory/account/jail state (self-healing). Existing system accounts and jails on disk are left untouched until an update/delete event.
- Rollback: disable the plugins in daemon wiring / `config.toml`; DB rows remain; PureFTPd continues to read `ftp_user` if still configured.

## Open Questions

- Should `sites_ftp_user_server_get` be a dedicated REST route, or is `GET /api/sites/ftp-users?username=` sufficient for clients that previously used the remote API helper?
- Periodic jailkit refresh (PHP has cron-driven jail updates for some distros) — out of scope unless a real production need appears; hash-based update on shell_user events covers the common path.
- `user_type` / `user_config` on `ftp_user` (system vs user) — kept on the model for schema fidelity; panel exposes only `user` accounts unless a later admin tool needs system FTP users.
