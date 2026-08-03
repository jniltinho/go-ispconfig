# Proposal: FTP and Shell Users Module

> Roadmap phase 2 — proposal only for now; design/specs/tasks will be authored when the module is scheduled. Depends on `port-ispconfig3-to-go` (foundation) and `add-web-nginx-module` (users are always attached to a website).

## Why

Sites without upload access are of limited use: customers need FTP accounts and (optionally chrooted) shell accounts tied to their websites. This ports the ISPConfig3 FTP (PureFTPd) and shell-user (incl. jailkit) plugins so go-ispconfig covers the standard site-access workflow.

## What Changes

- **FTP users**: CRUD on `ftp_user` (linked to a `web_domain`, uid/gid/dir derived from the site), served by **PureFTPd with MySQL auth** — the daemon does not write per-user system accounts; PureFTPd reads `ftp_user` directly, as in ISPConfig3. Daemon plugin ports `server/plugins-available/ftpuser_base_plugin.inc.php` (events `ftp_user_insert/update/delete`): ensures the ftp dir exists inside the web root, applies quota fields (`quota_size`, upload/download rate and bandwidth fields), and handles expiration/active flags.
- **Shell users**: CRUD on `shell_user` (linked to a `web_domain`); daemon plugin ports `server/plugins-available/shelluser_base_plugin.inc.php` (events `shell_user_insert/update/delete`): creates/updates/deletes the real system account (useradd/usermod/userdel), sets password or SSH key (`_setup_ssh_rsa` port: authorized_keys under the site), shell selection, and cleanup on delete.
- **Jailkit chroot**: port of `server/plugins-available/shelluser_jailkit_plugin.inc.php` — when `chroot = 'jailkit'`, build/update the jail under the website root (jk_init sections from server config, jk_cp apps), relocate the user into the jail, and tear it down on delete. **Jailkit ownership lives entirely in this module**: the web module only persists the jailkit fields on `web_domain` (stored-only there); all jail logic, paths/config (`server.config` `[jailkit]` section) and lifecycle belong here.
- **Quotas**: honor `ftp_user.quota_size` and shell users inherit the site's hd_quota; no separate quota subsystem (site quota is owned by the web module).
- **UI**: new entries under the existing **Sites** module (FTP-User and Shell-User lists/forms, as in `interface/web/sites/ftp_user_*.php`, `shell_user_*.php`), including client limit checks (`limit_ftp_user`, `limit_shell_user`).
- **REST API** mirroring `remote.d/sites.inc.php` ftp/shell functions: sites_ftp_user_add/get/update/delete, sites_shell_user_*, with Swagger docs.
- **Installer**: PureFTPd + jailkit configuration steps (port of `configure_pureftpd`/`configure_jailkit` from `install/lib/installer_base.lib.php`) will land as a **Modified Capability of `add-installer-cli`** (`installer-cli`) when this module is scheduled — tracked there, listed here for visibility.

## Capabilities

### New Capabilities

- `ftp-users`: ftp_user CRUD, PureFTPd MySQL-auth integration, directory/quota handling, API + Sites UI.
- `shell-users`: shell_user CRUD, system account lifecycle, SSH key setup, API + Sites UI.
- `jailkit-chroot`: jail creation/update/teardown for chrooted shell users.

### Modified Capabilities

- `web-module-events`: the web module gains the `ftp_user` and `shell_user` table hooks and announces the six matching events (PHP parity with `web_module.inc.php`, which already registers them). No other module behaviour changes; the Sites UI gains screens but its existing requirements are unchanged.

## Impact

- Reference PHP sources: `server/plugins-available/ftpuser_base_plugin.inc.php`, `shelluser_base_plugin.inc.php`, `shelluser_jailkit_plugin.inc.php`, `interface/web/sites/{ftp_user,shell_user}_*.php`, `install/tpl/pureftpd_mysql.conf.master` (or distro equivalent), `remote.d/sites.inc.php`.
- Tables: `ftp_user`, `shell_user`, `ftp_traffic` (read-only stats later); FKs to `web_domain` and `sys_group`.
- System packages: pure-ftpd-mysql, jailkit, openssh (Debian/Ubuntu targets).
- Depends on client limits hook from `add-client-module` for limit_ftp_user/limit_shell_user (soft dependency: enforced once that change lands).

## Non-goals

- SFTP-only server or ProFTPd/vsftpd support (PureFTPd only, as ISPConfig default on Debian).
- WebDAV users (`webdav_user`) — table stays, no logic.
- FTP traffic accounting dashboards (`ftp_traffic` collection is a monitor/statistics concern, later).
- TLS certificate management for PureFTPd beyond what the installer configures.
