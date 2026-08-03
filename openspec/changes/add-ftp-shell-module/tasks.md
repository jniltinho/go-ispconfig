# Tasks: add-ftp-shell-module

## 1. Models and foundations wiring

- [x] 1.1 Add GORM models for `ftp_user` and `shell_user` with explicit `gorm:"column:..."` tags matching the ISPConfig3 schema (`ftp_user_id`…`user_config`; `shell_user_id`…`ssh_rsa`); unit-test round-trip against MariaDB. Commit.
- [x] 1.2 Embed the shell-user blacklist (port of `interface/lib/shelluser_blacklist`) as a Go data set for username validation; unit-test known denied names (`root`, `www-data`, `mysql`, …). Commit.
- [x] 1.3 Extend `internal/web` Module: register table hooks for `ftp_user` and `shell_user`, announce `ftp_user_insert|update|delete` and `shell_user_insert|update|delete`, map datalog `i`/`u`/`d` to events; update the module unit tests that currently assert unhooked `ftp_user` is ignored. Commit.
- [x] 1.4 Document/load the server getconf `[jailkit]` section defaults (`jailkit_chroot_home`, `jailkit_chroot_app_sections`, `jailkit_chroot_app_programs`, `jailkit_chroot_cron_programs`, `jailkit_hardlinks`, authorized-keys template) matching `server.ini.master`; unit-test defaults. Commit.

## 2. FTP plugin (daemon)

- [x] 2.1 Implement `internal/ftp` plugin port of `ftpuser_base_plugin.inc.php`: subscribe to `ftp_user_insert|update|delete`; ensure dir under parent `document_root` (mkdir `0755` as site system_user/group with web_folder_protection toggle); delete old/new `.ftpquota` on dir change/delete; never call useradd/userdel; unit tests with fake filesystem/runner. Commit.
- [x] 2.2 Wire the FTP plugin into the daemon bootstrap next to the nginx plugin; integration-style test: datalog `ftp_user` insert → event → directory created under a temp docroot. Commit.

## 3. Shell base plugin (daemon)

- [x] 3.1 Implement `internal/shell` plugin port of `shelluser_base_plugin.inc.php` insert path: security kill-switch `allow_shell_user`, path/UID/user guards (`min_uid` 499), home layout (`dir/home`, user home, `.bash_history`/`.profile`/`.bashrc.d`/`.local/bin`, web/log/private symlinks), `useradd -o -u <puser_uid>`, `chpasswd -e`, temporary lock when `chroot=jailkit`; tests with fake runner asserting argv. Commit.
- [x] 3.2 Implement shell update path: homedir rename/move, `usermod`, missing-user fallthrough to insert, inactive → `/bin/false`; tests per branch. Commit.
- [x] 3.3 Implement shell delete path for non-jailkit users: optional PHP-FPM stop/start around `killall -u` + `userdel -f`, owned-dotfile cleanup when `dir` is unused; confirm jailkit users skip `userdel` here; tests. Commit.
- [x] 3.4 Implement `_setup_ssh_rsa` port: merge site shell_user keys + client `ssh_rsa`, write `.ssh/authorized_keys` with correct modes/ownership for non-jailkit and jailkit home layouts; tests. Commit.
- [x] 3.5 Wire the shell plugin into the daemon bootstrap; end-to-end test with fake runner from datalog row → expected commands. Commit.

## 4. Jailkit plugin (daemon)

- [x] 4.1 Implement `internal/jailkit` plugin config merge (server `[jailkit]` + `web_domain` overrides + `php_jk_section`) and hash computation for `last_jailkit_hash`; unit tests. Commit.
- [x] 4.2 Implement jail create/update (`jk_init` / program copy / MOTD / force-update with skip web_folder / stamp `last_jailkit_update`+hash) and `_add_jailkit_user` (jailed home, passwd/shadow, `jk_chrootsh`, unlock); tests with fake runner. Commit.
- [x] 4.3 Implement jailkit delete: `userdel`, jail passwd/shadow line removal, jailed home delete, optional `_delete_jailkit_if_unused` when `delete_unused_jailkit=y`; tests. Commit.
- [x] 4.4 Wire the jailkit plugin into the daemon bootstrap (after shell base on the same events); integration test: jailkit shell_user insert → base creates user → jailkit builds chroot and sets shell. Commit.

## 5. Validation, repositories, REST API

- [x] 5.1 Implement FTP user validation + prepare/after-insert/after-update hooks (prefix, parent vhost scope, CRYPT password, dir under docroot, admin-only advanced fields, client `limit_ftp_user`); table-driven unit tests. Commit.
- [x] 5.2 Implement Shell user validation + prepare/after-insert/after-update hooks (prefix, blacklist, 32-char cap, immutable parent, chroot allow-list, `ssh_authentication` password/key mode, client `limit_shell_user`, admin-only advanced fields); table-driven unit tests. Commit.
- [x] 5.3 Register declarative entities `FTPUser` and `ShellUser` under `/api/sites/ftp-users` and `/api/sites/shell-users` via `RegisterEntity` (list/get/create/update/delete), riud-scoped repositories, datalog `{old,new}`, password redaction on read; handler tests including 403 cross-client. Commit.
- [x] 5.4 Add swaggo annotations for both entities; run `make swagger` / `swag init` and verify Swagger UI lists the new endpoints; CI staleness check green. Commit.

## 6. Panel UI (Vue)

- [x] 6.1 Add Sites sidebar sections for FTP Users and Shell Users in `modules.ts` + routes; FTP Users list (DataTable: username, site, active, server; search) wired to the API; English i18n keys. Commit.
- [x] 6.2 FTP user form (TabbedForm / EntityForm): main tab + Options tab; admin-only advanced fields; client-side validation mirroring API; create/edit/delete flows. Commit.
- [x] 6.3 Shell Users list + form (chroot select, ssh_rsa, admin Options tab, parent domain locked on edit); i18n keys. Commit.
- [x] 6.4 agent-browser E2E against the built binary: create FTP user under a site, edit dir/quota, delete; create shell user (non-jailkit), toggle active, delete; screenshots to `docs/prints/`. Commit.

## 7. Integration and docs

- [ ] 7.1 End-to-end integration test against MariaDB: API FTP create → datalog → daemon run → directory exists; API shell create (non-jailkit) → datalog → daemon run → expected runner commands for useradd/chpasswd/ssh; API shell create with `chroot=jailkit` → jailkit commands sequenced after base. Commit.
- [ ] 7.2 Module docs in `docs/` (FTP PureFTPd MySQL auth model, shell account lifecycle, jailkit ops, security kill-switch, migration notes: existing accounts/jails left until next event; pointer that installer PureFTPd/jailkit steps live in `add-installer-cli`). Commit.
- [ ] 7.3 Note in `add-installer-cli` / installer tracking (or ROADMAP cross-link) that `configure_pureftpd` / `configure_jailkit` / `pureftpd_mysql.conf.master` remain a Modified Capability of the installer change when this module is scheduled — no installer code in this change unless that change is co-scheduled. Commit.
