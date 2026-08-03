# FTP and Shell Users module

Port of the ISPConfig3 site-access stack (`ftpuser_base_plugin.inc.php`,
`shelluser_base_plugin.inc.php`, `shelluser_jailkit_plugin.inc.php` + the
Sites → FTP/Shell User interface): virtual FTP accounts (PureFTPd MySQL
auth) and real shell accounts (optional jailkit chroot), driven from the
panel database through `sys_datalog`.

REST API under `/api/sites/ftp-users` and `/api/sites/shell-users`, panel
UI under **Sites → FTP Users / Shell Users**, daemon plugins wired next
to nginx (`cmd/daemon.go`) on servers with `web_server = 1`.

## Packages

| Package | Role |
|---------|------|
| `internal/ftp` | PureFTPd side-effects only: ensure login dir, clean `.ftpquota` |
| `internal/shell` | OS account lifecycle (`useradd`/`usermod`/`userdel`/`chpasswd`) + SSH keys |
| `internal/jailkit` | Chroot build/update/teardown when `chroot = jailkit` |
| `internal/web` | Table hooks for `ftp_user` / `shell_user` → six named events |
| `internal/api` | Validation, `RegisterEntity`, swaggo |
| `frontend/` | Lists + metadata-driven forms |

## FTP: PureFTPd MySQL auth model

FTP users are **virtual**. PureFTPd (`pure-ftpd-mysql`) authenticates
against the `ftp_user` table on every login — the daemon **never**
creates `/etc/passwd` entries for them.

Typical PureFTPd MySQL config (installer template
`pureftpd_mysql.conf.master`, still owned by `add-installer-cli`):

- Match `active = 'y'`, `server_id`, optional `expires > NOW()`
- Read `password` (CRYPT: `$1$` / `$5$` / `$6$`), `uid`, `gid`, `dir`,
  quotas and ratios/bandwidths

Daemon plugin (`internal/ftp`) on insert/update:

1. Load parent `web_domain` by `parent_domain_id`
2. Reject paths outside `document_root`
3. Create `dir` at `0755` owned by `system_user`:`system_group` when
   missing (with web_folder_protection toggle around mkdir)
4. On dir change, delete the old `.ftpquota`; on delete, delete
   `.ftpquota` only (never the user's files)

No PureFTPd reload is required for user CRUD (design D9).

## Shell account lifecycle

Shell users are **real system accounts** sharing the site system user's
UID (`useradd -o -u <puser_uid>`):

| Event | Behaviour |
|-------|-----------|
| Insert | Guards (`allow_shell_user`, path, min UID 499+), home layout under `dir/home/<username>`, `useradd`, `chpasswd -e`, `.profile` / `.bashrc.d` / symlinks, `_setup_ssh_rsa`; if `chroot=jailkit`, temporarily lock shell to `/bin/false` |
| Update | `usermod` for rename/home/shell/password; missing OS user falls through to insert; `active=n` forces `/bin/false` |
| Delete (non-jailkit) | Optional PHP-FPM stop/start around `killall -u` + `userdel -f`; owned-dotfile cleanup when `dir` unused |

### Security kill-switch

`permissions.allow_shell_user` (security policy, default `yes`) aborts
every shell and jailkit action when not enabled. Path and identity
guards (`system.IsAllowedPath`, min UID/GID, never root) apply on every
event. All shell-outs use `engine.Runner` with argv slices only.

### SSH keys (`_setup_ssh_rsa`)

On shell insert/update the base (and jailkit) plugin rebuilds
`authorized_keys` under the effective home:

- Merge all non-empty `shell_user.ssh_rsa` for the site + client `ssh_rsa`
- Write `.ssh/authorized_keys` mode `0600` under `.ssh` mode `0700`
- Global `ssh_authentication` mode (password / key / both) clears the
  unused secret field on save

## Jailkit ops

When `chroot = 'jailkit'`, the jailkit plugin runs **after** the shell
base plugin on the same events:

1. Merge server `[jailkit]` getconf with per-site
   `web_domain.jailkit_chroot_app_*` overrides and `php_jk_section`
2. `jk_init` / program copy when `last_jailkit_hash` changed; stamp
   `last_jailkit_update` + hash
3. Add user into jail passwd/shadow, home under `jailkit_chroot_home`,
   shell `/usr/sbin/jk_chrootsh`, unlock
4. Delete: `userdel`, jail line removal, optional full jail teardown when
   `delete_unused_jailkit = y` and no remaining jailkit users share the site

Default `[jailkit]` keys match `server.ini.master`
(`jailkit_chroot_home`, `jailkit_chroot_app_sections`,
`jailkit_chroot_app_programs`, `jailkit_chroot_cron_programs`,
`jailkit_hardlinks`, authorized-keys template).

## REST API

| Entity | Path | Notes |
|--------|------|-------|
| FTPUser | `/api/sites/ftp-users` | CRYPT password, redacted on read; parent derives `server_id` / `uid` / `gid` / default `dir` |
| ShellUser | `/api/sites/shell-users` | Blacklist + 32-char cap; parent immutable after create; `chroot` allow-list |

Client limits: `client.limit_ftp_user` / `limit_shell_user` (`-1`
unlimited, `0` none). Admin-only advanced fields match the ISPConfig
tforms (FTP ratios/bandwidth; shell puser/pgroup/shell/dir).

## Panel UI

- Sidebar: **Sites → FTP Users**, **Sites → Shell Users**
- DataTable lists (username, site, active, server; shell adds chroot)
- Metadata-driven forms (`EntityForm` + `/api/meta/forms/{entity}`)

## Migration notes

- **No schema changes** — tables are byte-identical to ISPConfig3.
- Existing `ftp_user` / `shell_user` rows and on-disk accounts/jails are
  left untouched until the next insert/update/delete event (self-healing
  on the next change).
- Fresh installs still need PureFTPd MySQL and jailkit packages + config
  from the installer — see below.

## Installer dependency (out of scope here)

`configure_pureftpd`, `configure_jailkit`, and the
`pureftpd_mysql.conf.master` template remain a **Modified Capability of
`add-installer-cli`** (archived). This module ships plugins and panel
only; until the installer change is extended, operators must provision
PureFTPd MySQL auth and jailkit manually for daemon side-effects to
matter in production. Cross-link: [install.md](install.md),
[ROADMAP.md](ROADMAP.md).

## Tests

| Suite | What |
|-------|------|
| `go test ./internal/ftp/...` | Fake FS/runner unit + docker MariaDB pipeline |
| `go test ./internal/shell/...` | Insert/update/delete/ssh argv + pipeline |
| `go test ./internal/jailkit/...` | Config hash, chroot lifecycle, sequencing |
| `go test -tags=integration ./internal/api/ -run FTPShell` | API → datalog → daemon (task 7.1) |
| `e2e/panel-ftp-shell.sh` | agent-browser UI create/edit/delete (task 6.4) |
