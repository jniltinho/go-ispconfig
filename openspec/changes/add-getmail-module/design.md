# Design: Getmail module (external POP3/IMAP fetching)

## Context

getmail in ISPConfig3 is the thinnest module in the whole product — four moving parts, no daemon of its own:

1. **`interface/web/mail/mail_get_*.php` + `form/mail_get.tform.php`** — one form writing one `mail_get` row per external account (`type`, `source_server`, `source_username`, `source_password`, `source_delete`, `source_read_all`, `destination`, `active`). `mail_get_edit.php::onSubmit` derives `server_id` from the destination mailbox (`SELECT server_id FROM mail_user WHERE email = ?`), checks that the destination belongs to the caller, enforces `limit_fetchmail`, and rejects `source_delete=n` + `source_read_all=y`; `onAfterInsert` copies `sys_groupid` from the destination `mail_user`.
2. **`server/mods-available/mail_module.inc.php`** — hooks `mail_get` and raises `mail_get_insert|update|delete`.
3. **`server/plugins-available/getmail_plugin.inc.php`** — on every event, delete the old rc file then (if `active=y`) render `server/conf/getmail.conf.master` into `<getmail_config_dir>/<clean(source_server)>_<clean(source_username)>.conf`, `chmod 0400`, `chown getmail`. `_clean_path` maps `[^A-Za-z0-9\-_]` to `_`. Mirror servers return early ("Do not write getmail config files on mirror servers to avoid double fetching of emails"). The template placeholders are `{DELETE}`, `{READ_ALL}`, `{TYPE}`, `{SERVER}`, `{USERNAME}`, `{PASSWORD}`, `{DESTINATION}`; `{TYPE}` maps `pop3|imap|pop3ssl|imapssl` → `SimplePOP3Retriever|SimpleIMAPRetriever|SimplePOP3SSLRetriever|SimpleIMAPSSLRetriever`.
4. **`server/scripts/run-getmail.sh` + a crontab** — `installer_base.lib.php` creates the `getmail` system user with `-d /etc/getmail`, `chown -R getmail`, `chmod -R 700`, installs the script at `/usr/local/bin/run-getmail.sh` (0744, owned by `getmail`) and writes `*/5 * * * * /usr/local/bin/run-getmail.sh` into the **getmail user's** crontab. The script globs `*.conf`, builds one `-r <file>` list and runs `getmail -v -g /etc/getmail $rcfiles || true`, serialised by a `/tmp/.getmail_lock` sentinel.

Delivery is not getmail's problem: the rc `[destination]` is `MDA_external` to `/usr/sbin/sendmail -i -bm <destination>`, so a fetched message re-enters the local MTA as an ordinary mail for that address and lands in the mailbox through the normal Postfix → Dovecot LMTP path. The maildir, its quota and its sieve rules stay owned by `mailPlugin` / `maildeliverPlugin`. That is why this change adds **no** filesystem code beyond the rc files.

On the Go side everything this needs already exists: `internal/mail` has a `Plugin` base plus three satellite plugins (`MaildeliverPlugin`, `DkimPlugin`, `RspamdPlugin`) that all take `*Plugin` and register their own events; `mastertpl.Load`/`New` renders `.master` files with a custom-dir override; `engine.Scheduler.Register` runs cron-spec jobs as asynq periodic tasks with last-run/status in `sys_config`; `internal/api` has the entity framework the other mail resources use; `internal/clients/usage.go` already counts `mail_get` rows for `limit_fetchmail`.

## Goals / Non-Goals

**Goals:**
- Behaviour-faithful port of `getmail_plugin.inc.php` + `run-getmail.sh` + the `mail_get` hook + `mail_fetchmail_*`, including the mirror-server skip and every path guard.
- Replace the getmail crontab with a scheduler job without changing what actually runs (`getmail -g <dir> -r <rc>…` as the `getmail` user).
- Panel parity for the "Fetch Email" list/form, `limit_fetchmail` enforcement and the Getmail server-config tab.
- Golden file for the rendered rc; unit tests for sanitising, guards and the rc discovery; integration test for datalog → rc file.

**Non-Goals:**
- Any change to how fetched mail is delivered (see proposal Non-goals).
- Per-account intervals, extra retriever types, OAuth2, password encryption at rest.
- Writing a crontab or a systemd timer.
- Schema changes; translations beyond English.

## Decisions

### D1 — A fifth plugin in `internal/mail`, not a new package
`internal/mail/getmail.go` holds `GetmailPlugin`, constructed as `mail.NewGetmailPlugin(mailPlugin, cfg.Templates.CustomDir)` and appended in `cmd/daemon.go` next to `NewMaildeliverPlugin` / `NewDkimPlugin` / `NewRspamdPlugin`. It reuses the base `Plugin`'s db, runner, serverID and logger.

*Alternative*: a standalone `internal/getmail` package — rejected. It would duplicate the base plugin plumbing for ~150 lines of logic, and getmail is only ever loaded on a server that already loads the mail module (`server.mail_server = 1`).

### D2 — `mail_get` joins the existing module hook list
One entry added to `hookedTables` in `internal/mail/module.go`, which automatically announces `mail_get_insert|update|delete` and registers the table hook — the module's generic `process` needs no change. This is the `mail-module-events` modification called out in the proposal, and it retires that spec's "`mail_get` is not hooked" scenario.

### D3 — rc rendering through `mastertpl`, guards kept and tightened
`getmail.conf.master` is copied verbatim into `internal/mastertpl/templates/` (embedded, `mastertpl.Load` honours the custom-template dir like every other mail template). Placeholders are substituted exactly as PHP does, including the boolean mapping (`source_delete`/`source_read_all` `y` → `true`, anything else → `false`) and the retriever map of D-context item 3. An unknown `type` leaves `{TYPE}` unsubstituted in PHP; here it is a rejected row (logged, no file written) — a malformed rc would fail the whole batch run in `getmail`.

Filename: `<clean(source_server)>_<clean(source_username)>.conf` with `clean` = `[^A-Za-z0-9\-_]` → `_`, byte-identical to `_clean_path`, so an upgrade from PHP finds its existing files. Guards, in order:

1. PHP's metacharacter check on the assembled path (`..`, `|`, `;`, `$`) — kept for parity and log-message compatibility.
2. Additionally, `filepath.Clean` of the result MUST still have `getmail_config_dir` as its parent. `clean()` already makes traversal impossible; the containment check is the assertion that survives a future change to the naming scheme.
3. The config dir must exist and be a directory (PHP logs an error and does nothing otherwise) — the daemon must never `mkdir` it, because creating it without the `getmail` user/ownership would silently produce rc files nothing can read.

Ordering follows PHP: **delete first, then write**, so a rename of `source_server`/`source_username` never leaves an orphan rc (the delete uses `data.old`, the write uses `data.new`). `active=n` deletes and writes nothing.

### D4 — Permissions: 0600 owned by `getmail`, not PHP's 0400
The rc file holds a third-party password in cleartext, and getmail refuses to run on an rc file that is group- or world-readable. PHP writes `0400` + `chown getmail`. This port writes `0600` + `chown getmail:getmail`:

- `0400` and `0600` are equally private; `0600` lets the owning user rewrite the file, which matters because a future `getmail --idle`/state write in the same directory is done as that user.
- Order is write → `chown` → `chmod` (never the reverse: a `0600` file owned by root momentarily readable by nobody is safe, the inverse is not).
- The parent directory stays `0700` owned by `getmail`, created by the installer (D8), so even a botched file mode is not world-reachable.

`chown` goes through the command runner (`chown getmail:getmail <file>`), the same mechanism the mail plugin uses for sieve artifacts, so tests fake it and every argv is logged.

### D5 — `getmail_fetch` scheduler job replaces the crontab and `run-getmail.sh`
`(*GetmailPlugin).RegisterFetchJob(sched)` registers `getmail_fetch` at `*/5 * * * *`, wired in `cmd/daemon.go` beside `mailPlugin.RegisterPurgeJob(sched)`. The job body is `run-getmail.sh` in Go:

1. Read `getmail_config_dir`; list `*.conf` (non-recursive, sorted for deterministic argv). No files → return nil, no exec.
2. Run the configured program once with `-g <dir>` and one `-r <file>` per rc, dropping to the `getmail` user via `setpriv --reuid <uid> --regid <gid> --clear-groups` — one argv prefix rather than new privilege-dropping plumbing in `engine.CommandRunner`, which today has no user field. `internal/cron/privdrop.go` already proves the uid/gid lookup pattern if a native `SysProcAttr` runner is added later.
3. A non-zero exit is logged with the captured output and **not** returned as a job error when at least one account is configured — matching `|| true` in the shell script, whose intent is that one dead remote server must not stop the others. A failure to *start* the program (missing binary, unresolvable user) is a real job error and surfaces in the scheduler's `sys_config` status.

The `/tmp/.getmail_lock` sentinel becomes a `sync.Mutex` `TryLock` in the plugin: a still-running fetch makes the next activation log and return immediately. `// ponytail: in-process single-flight; a file lock under /run is only needed if a second daemon can share the config dir.`

*Alternative — a systemd timer* (`go-ispconfig-getmail.timer` + `.service` with `User=getmail`): rejected. It would be the only periodic work outside the scheduler, invisible to `/api/monitor` job status, and it duplicates the installer's unit handling. The scheduler already gives cron specs, last-run and status for free.

*Alternative — keep `run-getmail.sh`*: rejected, it exists only to build an argv.

### D6 — `[getmail]` getconf section
`internal/getconf` gains `GetmailConfig` decoded from the `[getmail]` server-config section, alongside `MailConfig`:

| Key | Default | Used for |
|---|---|---|
| `getmail_config_dir` | `/etc/getmail` | rc file directory and `-g` state directory |
| `getmail_program` | `/usr/bin/getmail` | binary invoked by the job (`getmail6` installs here on Debian/Ubuntu) |
| `getmail_user` | `getmail` | rc file owner and the identity the job drops to |

Only `getmail_config_dir` exists in PHP (`server_config.tform.php` Getmail tab, regex `^\/[a-zA-Z0-9\.\-\_\/]{5,128}$`); the other two are PHP install-time constants (`$conf['getmail']['program']`) promoted to config so a distro variant does not need a code change. The UI exposes `getmail_config_dir` only, same as ISPConfig.

### D7 — `source_password` stays cleartext, and is never returned or logged
getmail authenticates to the remote server with the password, so it must be recoverable — no hashing, and no encryption at rest either (the daemon would need the key on the same box, which buys nothing against a root compromise and adds a key-rotation problem). The column is `varchar(64)`, so the API validates length ≤ 64 rather than truncating silently at write time (PHP's tform declares `maxlength 255` against a 64-char column — a latent silent truncation this port fixes).

Compensating controls, all of which are requirements, not intentions: list and detail responses omit `source_password`; the plugin never logs the rendered rc body or the password; rc files are `0600` in a `0700` directory owned by a dedicated user; the panel form submits a write-only field.

### D8 — Installer: package + user + directory, no script, no crontab
A step in `internal/installer` on mail servers, port of `configure_getmail()`:

- package `getmail6` (Debian 11 ships `getmail`; the package name comes from the distro table next to the other mail packages);
- system user `getmail` with home `= getmail_config_dir`, no shell, created only when absent;
- directory `getmail_config_dir` `0700` owned `getmail:getmail`.

Not ported: `/usr/local/bin/run-getmail.sh` (D5) and the `crontab -u getmail` block. On upgrade from PHP ISPConfig, an existing getmail crontab is out of scope to remove — documented in `docs/` as a manual cutover step, consistent with how the other cron cutovers are handled.

### D9 — `model.MailGet` and the REST entity
`model.MailGet` maps the existing `mail_get` table (PK `mailget_id`) with the `sys_*` permission columns the riud scopes need. `/api/mail/fetchmail` is a standard `RegisterEntity[model.MailGet]` mounted from `registerMailEntities`, `Decorate: datalogStateDecorator("mail_get", "mailget_id")`, with a `Prepare` hook carrying the four PHP rules from `mail_get_edit.php`:

1. `destination` must resolve to a `mail_user` row visible to the caller (`no_destination_perm` parity);
2. `server_id` is **derived** from that row and ignored if supplied by the client;
3. `sys_groupid` is copied from that row after insert (PHP `onAfterInsert`);
4. `source_delete=n` together with `source_read_all=y` is rejected (`error_delete_read_all_combination`) — that combination re-fetches the entire remote mailbox every five minutes forever.

`limit_fetchmail` is enforced on create for non-admins via the existing client-limits helper, which already counts this table.

### D10 — UI
A `mail-fetchmail` route pair in `frontend/src/router.ts` reusing the generic `MailList.vue` (`apiBase: /api/mail/fetchmail`, `idField: mailget_id`, columns active / type / source_server / source_username / destination — the columns `mail_get.list.php` shows) plus a `FetchmailForm.vue` mirroring the tform fields: server-side-derived `server_id` is not shown, `type` is a four-option select, `destination` is a select over the caller's mailboxes, `source_password` is a write-only password input, `source_delete` / `source_read_all` / `active` are checkboxes. The Getmail tab in the server-config view exposes `getmail_config_dir`.

## Risks / Trade-offs

- [Cleartext third-party passwords in the DB and on disk] → D7's controls; unchanged from ISPConfig, and documented in `docs/`.
- [One `getmail` invocation for all accounts, `*/5`] → a slow remote server delays the batch; the single-flight guard means the next tick is skipped rather than piling up. Splitting into per-account runs is the upgrade path if throughput ever matters.
- [Renaming `source_server`/`source_username` orphans the old rc] → D3's delete-then-write on `{old,new}`; covered by a scenario.
- [Two mail servers sharing one `getmail_config_dir` over NFS would double-fetch] → out of scope; the mirror guard covers the case ISPConfig actually supports.
- [`setpriv` availability] → present in `util-linux` on every target distro (Debian 11+/Ubuntu 22.04+); a missing binary is a hard job error, not a silent root-run.
- [Stale rc files from a PHP install with rows since deleted] → the plugin only ever touches files it can name from a row; a documented `--resync`-style sweep is out of scope. Files nothing references keep fetching until removed by hand — called out in `docs/`.

## Migration Plan

- Code + one embedded template only; no schema change, existing `mail_get` rows work as they are.
- Fresh install: the installer step (D8) provisions package/user/dir; the first `mail_get` datalog row writes the rc.
- Cutover from PHP ISPConfig: rc filenames are byte-identical, so existing files under `/etc/getmail` are simply re-used and overwritten in place on the next event. The operator must remove the old `crontab -u getmail` entry, otherwise both the crontab and the scheduler job fetch (the lock file and the mutex do not know about each other) — documented as a required manual step.
- Rollback: no `getmail` rows → no rc files → the job is a no-op; the plugin can be dropped from `cmd/daemon.go` without touching anything else.

## Open Questions

- Should the fetch interval be configurable (`[getmail] getmail_interval` → the job's cron spec) instead of hard-coded `*/5 * * * *`? Leaning: hard-coded for parity now; the scheduler takes any spec, so promoting it later is a one-line change.
- Should a failed account surface in the panel (last error per `mail_get` row) rather than only in the daemon log? PHP shows nothing at all. Leaning: defer — it needs a column, which this change refuses to add.
