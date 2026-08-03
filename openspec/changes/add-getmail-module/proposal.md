# Proposal: add-getmail-module

## Why

`add-mail-module` shipped the Postfix + Dovecot + Rspamd stack but listed getmail / `mail_get` fetching as an explicit **non-goal** ("getmail / `mail_get` fetching (`getmail_plugin`), Mailman mailing lists, Courier and maildrop — legacy"), and the daemon module deliberately left `mail_get` out of its table hooks (`internal/mail/module.go`: "design D2: mail_get, mail_content_filter and mail_mailinglist are non-goals and simply not registered"). This change closes that gap.

The feature itself is not legacy in practice: "Fetch Email" is how a customer migrates an existing POP3/IMAP mailbox hosted elsewhere into a mailbox on this server, and the client limit that gates it (`limit_fetchmail`) is **already ported and counted** in `internal/clients/usage.go` (`countByGroup("mail_get", "")`) — the panel shows a quota for a feature that cannot be used. Every other piece is already in place too: the `mail_get` table exists byte-identically in the schema, the `.master` renderer, the delayed-service registry, the scheduler and the entity/REST framework all ship.

Reference PHP sources being ported (read-only under `base/ispconfig3_install/`):
- `server/plugins-available/getmail_plugin.inc.php` — writes/removes one getmail rc file per `mail_get` row (no `getmail_plugin/` subdirectory exists; the plugin is a single file)
- `server/conf/getmail.conf.master` — the rc template (`[options]` / `[retriever]` / `[destination]`)
- `server/scripts/run-getmail.sh` — the runner: collect `*.conf` under `/etc/getmail`, one `getmail -v -g /etc/getmail -r …` invocation, guarded by `/tmp/.getmail_lock`
- `install/lib/installer_base.lib.php` — `configure_getmail()` (create the `getmail` system user, `chown -R getmail`, `chmod -R 700` on the config dir), the `run-getmail.sh` install at `/usr/local/bin` and the `*/5 * * * *` getmail crontab
- `server/mods-available/mail_module.inc.php` — the `mail_get` table hook → `mail_get_insert|update|delete`
- `interface/web/mail/mail_get_edit.php`, `mail_get_list.php`, `mail_get_del.php`, `form/mail_get.tform.php`, `list/mail_get.list.php` — the panel form/list, `limit_fetchmail` enforcement, destination ownership check and the `server_id`/`sys_groupid` derivation
- `interface/lib/classes/remote.d/mail.inc.php` — `mail_fetchmail_get|add|update|delete`
- `interface/web/admin/form/server_config.tform.php` — the `getmail` server-config tab (`getmail_config_dir`)
- `install/sql/ispconfig3.sql` — `mail_get` table (no schema change needed)

## What Changes

- **`mail_get` table hook**: the existing mail module registers `mail_get` and announces/raises `mail_get_insert|update|delete` (port of `mail_module.inc.php::process`). This is the only change to already-shipped daemon behaviour.
- **getmail plugin**: a fifth plugin in `internal/mail`, port of `getmail_plugin.inc.php` — render one rc file per active row from an embedded `getmail.conf.master`, remove it on deactivate/delete, skip entirely on mirror servers, with the PHP path guards plus a containment check on the config dir.
- **`[getmail]` getconf section**: typed `GetmailConfig` (`getmail_config_dir`, program path, run-as user) with Debian/Ubuntu defaults, plus the Getmail tab in the server-config UI.
- **Scheduler job, not a crontab**: a `getmail_fetch` job on the daemon scheduler (`*/5 * * * *`) replaces the getmail crontab **and** `run-getmail.sh`. Foundation rule D1b stands — the installer never writes a crontab.
- **REST API**: `/api/mail/fetchmail` CRUD on the entity framework, port of `mail_fetchmail_*`, with `limit_fetchmail` enforcement, destination-ownership check, derived `server_id`/`sys_groupid` and the `source_delete=n` + `source_read_all=y` rejection.
- **UI (Vue 3)**: "Fetch Email" list + form under the Email module, reusing the generic `MailList.vue` table.
- **Installer**: provision the `getmail6` package and the `getmail` system user with a `0700` config dir owned by it (port of `configure_getmail()`); no `/usr/local/bin/run-getmail.sh`, no crontab.
- **Testing**: golden file for the rendered rc, unit tests for the filename sanitiser and path guards, integration test for `mail_get` datalog → rc file, scheduler-job test with a fake command runner.

## Capabilities

### New Capabilities

- `getmail-config`: daemon getmail plugin — `mail_get` events, per-account rc file rendering with ownership/permissions and path guards, `[getmail]` getconf section.
- `getmail-cron`: the `getmail_fetch` scheduler job — rc discovery, single-flight guard, privilege drop to the `getmail` user, per-account failure isolation and status reporting.
- `getmail-ui`: REST surface (`/api/mail/fetchmail`, port of `mail_fetchmail_*`) plus the Vue "Fetch Email" list/form and the Getmail server-config tab.

### Modified Capabilities

- `mail-module-events`: `mail_get` moves from "not hooked" to hooked. The requirement "Mail table hooks raise named events" gains `mail_get` and its three events, and its scenario "Out-of-scope tables are not hooked" narrows to `mail_mailinglist` / `mail_content_filter` only. The replacement requirement text lives in `specs/getmail-config/spec.md` ("mail_get table hook") so the delta stays in one place.

## Impact

- **Depends on `add-mail-module`** (mail module, `mail.Plugin` base, `mastertpl`, mail REST group, Email nav) and on the foundation scheduler (`internal/engine/scheduler.go`).
- Touched Go code: `internal/mail` (new `getmail.go`, one line in `module.go`), `internal/getconf` (new section), `internal/model/mail.go` (`MailGet`), `internal/api` (`mailfetch.go` + registration in `registerMailEntities`), `internal/mastertpl/templates/getmail.conf.master`, `cmd/daemon.go` (plugin + job wiring, same shape as `RegisterPurgeJob`), `internal/installer` (package + user step), `frontend/src/router.ts` + a `FetchmailForm.vue`.
- DB: **no schema change** — the existing `mail_get` table is used as is (`mailget_id`, `server_id`, `type`, `source_server`, `source_username`, `source_password`, `source_delete`, `source_read_all`, `destination`, `active` + the `sys_*` permission columns).
- External: the `getmail6` package and a `getmail` system user on mail servers. Fetched mail re-enters through `sendmail -i -bm <destination>`, so delivery, quota and sieve remain owned by the existing mail plugins — no new maildir code.
- `limit_fetchmail` becomes meaningful: the already-shipped counter now gates a reachable feature.

## Non-goals

- Rewriting the delivery path: no direct maildir writes, no `MDA_lmtp`/`Maildir` destination types. `MDA_external` → `sendmail` (PHP parity) only.
- Per-account fetch intervals or per-account schedules — one global `*/5 * * * *` job, as in ISPConfig.
- A systemd timer or any crontab entry (foundation D1b: the daemon scheduler owns periodic work).
- getmail retriever types beyond the four PHP offers (`pop3`, `imap`, `pop3ssl`, `imapssl`); no OAuth2/XOAUTH2 retrievers, no client-certificate auth.
- Encrypting `mail_get.source_password` at rest — getmail needs the cleartext; the column stays as ISPConfig defines it (see design D7).
- Per-account fetch logs/history in the panel, and multi-server mirror fetching (mirrors are skipped, PHP parity).
- Mailman, Courier/maildrop, `mail_content_filter` — still out of scope.
- Translations beyond English.
