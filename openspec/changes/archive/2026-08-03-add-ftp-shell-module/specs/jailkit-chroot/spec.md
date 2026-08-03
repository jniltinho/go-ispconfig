# jailkit-chroot

## ADDED Requirements

### Requirement: Jailkit plugin subscribes to shell_user events
The jailkit plugin SHALL register handlers for `shell_user_insert`, `shell_user_update`, and `shell_user_delete` (port of `shelluser_jailkit_plugin.inc.php`) and SHALL only perform work when `chroot = 'jailkit'` (insert/update use `new.chroot`; delete uses `old.chroot`). It SHALL honor the same security kill-switch as the shell base plugin (`permissions.allow_shell_user`).

#### Scenario: Non-jailkit insert is a no-op for jailkit
- **WHEN** `shell_user_insert` fires with `chroot` empty or `no`
- **THEN** the jailkit plugin performs no jail filesystem changes

#### Scenario: Policy disables jailkit as well
- **WHEN** `allow_shell_user` is not `yes` and a jailkit shell user is inserted
- **THEN** the jailkit plugin logs a warning and exits without changes

### Requirement: Jail configuration from server section and site overrides
Jail setup SHALL load the server getconf section `[jailkit]` (`jailkit_chroot_home` default `/home/[username]`, `jailkit_chroot_app_sections`, `jailkit_chroot_app_programs`, `jailkit_chroot_cron_programs`, `jailkit_hardlinks`, authorized-keys template, and related keys from `server.ini.master`) and overlay non-empty `web_domain.jailkit_chroot_app_sections` / `jailkit_chroot_app_programs`. When the site has a `server_php.php_jk_section`, that section name SHALL be appended to the app sections list (deduplicated, sorted). Hardlink options follow `jailkit_hardlinks` (`yes` → hardlink; otherwise allow_hardlink).

#### Scenario: Site-level sections override server defaults
- **WHEN** a jailkit shell user is inserted for a site with custom `jailkit_chroot_app_sections`
- **THEN** `jk_init` is invoked with the site sections (plus PHP jk section when set), not only the server defaults

#### Scenario: PHP jk section is merged
- **WHEN** the site's selected PHP version has `php_jk_section = 'php8_3'`
- **THEN** the effective sections list includes `php8_3`

### Requirement: Jail create and update under the website root
On insert/update for jailkit users, after the OS user exists, the plugin SHALL (port of `_setup_jailkit_chroot` / `_add_jailkit_user`):
1. Toggle web_folder_protection off around jail mutations.
2. If `<dir>/etc/jailkit` is missing, create the chroot with the configured app sections, copy additional programs, and write a MOTD under `<dir>/var/run/motd` from the domain name.
3. If the jail already exists, compute an md5 hash of the sorted sections+programs+cron programs; when it equals `web_domain.last_jailkit_hash`, skip rebuild; otherwise force-update the chroot (skipping site `web_folder` subpaths) including PHP CLI binary when provided.
4. Stamp `web_domain.last_jailkit_update = NOW()` and `last_jailkit_hash` for all sites sharing that `document_root`.
5. Ensure the jailed home at `dir + jailkit_chroot_home` (with `[username]` substituted), add the user to the jail passwd/shadow, set the OS user shell to `/usr/sbin/jk_chrootsh`, unlock the account (`usermod -U`), and run SSH key + shell PHP setup inside the jail context.

#### Scenario: First jailkit user builds the chroot
- **WHEN** `shell_user_insert` fires with `chroot = 'jailkit'` and `<dir>/etc/jailkit` does not exist
- **THEN** a jailkit chroot is created under `dir`, the user is added to the jail, and the OS shell becomes `/usr/sbin/jk_chrootsh`

#### Scenario: Unchanged hash skips rebuild
- **WHEN** a second jailkit shell user is added to a site whose `last_jailkit_hash` already matches the current sections/programs set
- **THEN** the chroot is not rebuilt from scratch and only the user is added/updated

#### Scenario: Hash change rebuilds the jail
- **WHEN** site jailkit sections change and a shell user update triggers the plugin with a different hash
- **THEN** the chroot is force-updated and `last_jailkit_hash` is rewritten

### Requirement: Jailkit delete removes the jailed account
On `shell_user_delete` when `old.chroot = 'jailkit'`, the plugin SHALL: kill processes and `userdel -f` the username; remove the user lines from `<dir>/etc/passwd` and `<dir>/etc/shadow`; delete the jailed home directory when present (port of `_delete_homedir`); when `web_domain.delete_unused_jailkit = 'y'` and no remaining jailkit shell users reference the site, tear down unused jail contents (port of `_delete_jailkit_if_unused`, respecting do-not-remove paths). Web folder protection is restored afterward.

#### Scenario: Jailkit user delete removes OS user and jail passwd line
- **WHEN** `shell_user_delete` fires for a jailkit user
- **THEN** the OS account is removed and the username no longer appears in `<dir>/etc/passwd`

#### Scenario: Unused jail torn down when configured
- **WHEN** the last jailkit shell user of a site with `delete_unused_jailkit = 'y'` is deleted
- **THEN** the unused jail tree under the site document root is removed per the plugin rules

#### Scenario: Jail kept when delete_unused_jailkit is n
- **WHEN** the last jailkit shell user is deleted but `delete_unused_jailkit = 'n'`
- **THEN** the chroot directories remain on disk

### Requirement: Jailkit ownership is not in the web/nginx module
The web module and nginx plugin SHALL continue to treat `web_domain` jailkit columns as stored data only. All `jk_init` / `jk_cp` / jail passwd mutations and `last_jailkit_*` updates from jail lifecycle belong to this module's jailkit plugin.

#### Scenario: Web domain save does not build a jail
- **WHEN** a site is updated only changing `jailkit_chroot_app_sections` with no shell_user event
- **THEN** no jailkit chroot tools are invoked until a subsequent shell_user jailkit event runs

### Requirement: Safety guards shared with shell base
Jailkit operations SHALL refuse usernames/puser/pgroup that are root, refuse paths that are files/symlinks or not allowed, and require parent system user UID `> 499`. Commands run via the foundation runner with argv slices only.

#### Scenario: Root parent user aborts jailkit setup
- **WHEN** a jailkit insert is attempted with `puser = root`
- **THEN** the plugin logs a warning and does not modify the jail
