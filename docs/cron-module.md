# Cron module

Port of the ISPConfig3 cron stack (`cron_module.inc.php` +
`cron_plugin.inc.php` + the Sites → Cron interface): client cron jobs
stored in the panel database, executed by the **go-ispconfig daemon
itself** instead of being written out as crontab drop-ins. REST API
under `/api/sites/crons`, panel UI under **Sites → Cron**, daemon module
+ cron plugin on servers with `web_server = 1`.

The one deliberate divergence from PHP: nothing is ever written under
`crontab_dir`. Jobs live in the daemon's in-process scheduler
(`robfig/cron`), so there is no system cron, no `ispc_*` files and no
`crontab` reload to keep in sync.

## Data model

Table `cron`, byte-identical to the ISPConfig3 schema
(`internal/model/cron.go`). Key columns:

| Column | Meaning |
|--------|---------|
| `parent_domain_id` | owning website (vhost) — required, immutable after create; the job inherits its `sys_groupid` and `server_id` |
| `type` | `url`, `chrooted` or `full` — auto-derived, never chosen by the user |
| `command` | URL to fetch (`url`) or command line to execute (`chrooted`/`full`) |
| `run_min` / `run_hour` / `run_mday` / `run_month` / `run_wday` | the five schedule fields |
| `log` | `y` writes one `sys_log` row per run |
| `active` | `n` removes the job from the runner without deleting the row |

## Architecture

```
panel/API ──▶ cron row + sys_datalog ──▶ daemon
                                          ├── cron module   (datalog → events)
                                          ├── cron plugin   (events → runner)
                                          └── ClientJobRunner (robfig/cron)
                                                 ├── URL executor
                                                 └── process executor (full/chrooted)
```

- **Module** (`internal/cron/module.go`) — registers a table hook on
  `cron` and announces `cron_insert` / `cron_update` / `cron_delete`,
  mapping datalog actions `i`/`u`/`d`. Gated by
  `cron.Enabled(web_server, disable_cron_module)`: a server that is not
  a web server registers no hooks at all.
- **Plugin** (`internal/cron/plugin.go`) — subscribes to the three
  events, resolves the parent `web_domain` (joined with `server_php` for
  the PHP CLI binary), skips jobs whose parent is missing or whose
  site user/group is not `web<N>`/`client<N>` (PHP `is_allowed_user` /
  `is_allowed_group` parity), and drives the runner. `active != 'y'`
  and delete both mean *remove from the runner*.
- **ClientJobRunner** (`internal/cron/runner.go`) — mutex-guarded
  `map[cron.id] → robfig entry`; `Add` replaces by id so an update never
  leaves a stale schedule behind. The five fields are joined with spaces
  stripped; `run_wday = 7` is normalized to `0` (vixie accepts both,
  robfig only `0`), otherwise the job would be accepted and never fire.
  `run_month = @reboot` is not a cron expression — it is kept aside and
  fired once when the daemon starts.
- **Boot load** (`internal/cron/load.go`) — on daemon start every
  `cron WHERE active='y' AND server_id=<this>` is re-armed; a bad row is
  logged and skipped, it never aborts the load of the rest.

## Job types

`type` is derived from the command and the owner's `limit_cron_type`
(`internal/cron/type.go`, port of `cron_edit.php::onSubmit`) — the form
shows it read-only:

| Type | Derived when | Execution |
|------|--------------|-----------|
| `url` | the command is an `http(s)://` URL | HTTP GET with the site domain substituted for `{DOMAIN}`, 2 h timeout, **TLS verification on** (PHP used `wget --no-check-certificate`), first 4 KiB of the body kept for the log |
| `chrooted` | non-URL command, owner limited to `chrooted` | same as `full`, plus `document_root` stripped from the command paths |
| `full` | non-URL command, owner allowed `full` (admin-owned sites always) | argv split **without a shell**, cwd `<document_root>/web`, placeholders `{DOMAIN}`, `{DOCROOT_CLIENT}`, `[web_root]`, `{SITE_PHP}` expanded |

`{SITE_PHP}` resolves to the site's `server_php.php_cli_binary`, falling
back to `/usr/bin/php`. Commands containing CR, LF, NUL or a backslash
are rejected at validation time and again before execution.

## Privilege drop (full / chrooted)

Fail-closed, in `internal/cron/privdrop.go`:

- the site's `system_user`/`system_group` are resolved to uid/gid; **uid
  or gid 0 is refused** — the job aborts and the abort is logged even
  when `log = 'n'`;
- if the resolution itself fails, the job aborts (never falls back to
  running as the daemon user);
- the child gets `SysProcAttr.Credential` + `Setpgid`, so the context
  timeout kills the whole process group (`kill(-pid, SIGKILL)`), not
  just the direct child;
- no shell is involved at any point — the command is split into argv, so
  `;`, backticks and pipes are inert arguments.

Go does not expose `PR_SET_NO_NEW_PRIVS` on `SysProcAttr`; the enforced
guarantees are the credential drop, the root refusal and the process
group kill.

## Run log convention (`sys_log`)

One row per run when `log = 'y'` (and always for security aborts),
written by `internal/cron/log.go`:

```
cron_run id=%d parent_domain_id=%d type=%s status=%s exit=%d start=%d end=%d output=%s
```

- `status`: `ok`, `exit`, `timeout`, `error` (`security` for an aborted
  privilege drop);
- `exit`: process exit code, or the HTTP status for `url` jobs;
- `start`/`end`: unix timestamps;
- `output`: last 4 KiB of stdout+stderr (or the response body), cut on a
  rune boundary, `-` when empty;
- `loglevel`: `0` info, `1` warn (`exit`/`timeout`), `2` error.

`GET /api/sites/crons/:id/runs` reads this back with
`message LIKE 'cron_run id=<id> %'`, newest first, and parses the fields
for the run-history table in the UI.

## Legacy crontab cutover

On daemon start, before arming any job, `internal/cron/cutover.go`
deletes leftover PHP drop-ins under the server's getconf
`[cron] crontab_dir` (default `/etc/cron.d`): files named `ispc_*` or
`ispc_chrooted_*`. Every removal is logged. From then on the jobs exist
only inside the daemon — a machine migrated from ISPConfig PHP stops
running the old system-cron copies on the first daemon start, so jobs
are never executed twice.

## Client limits

Enforced on create/update for non-admins (`internal/api/sitescron.go`),
admins bypass all three:

| Limit | Effect |
|-------|--------|
| `limit_cron` | max number of cron rows for the client's group (checked on create) |
| `limit_cron_type` | `url` forbids anything but URL jobs; `chrooted` forbids `full` |
| `limit_cron_frequency` | minimum minutes between two runs, computed from the five fields by `MinFrequencyMinutes` (wrap-around aware) |

Defaults for a new client: `limit_cron = 0`, `limit_cron_type = url`,
`limit_cron_frequency = 5`.

## Schedule validation

`internal/cron/schedule.go` ports `validate_cron.inc.php`: allowed
charset, per-field ranges (`run_min` 0-59, `run_hour` 0-23, `run_mday`
1-31, `run_month` 1-12, `run_wday` 0-7), `a-b` ranges, `*/n` steps and
comma lists; `run_month` additionally accepts `@reboot`. The panel form
mirrors the same rules client-side, so an obviously broken field is
rejected before the POST.

## Configuration

Only one config.toml key belongs to this module:

```toml
[daemon]
disable_cron_module = false   # true turns the module off on this server
```

`crontab_dir` comes from the server's getconf (`[cron] crontab_dir`).
Timeouts are constants: 2 h for both URL fetches and processes, 4 KiB of
captured output.

## UI

**Sites → Cron** (`frontend/src/views/sites/`): `CronList.vue`
(active, website, schedule, type, command, log), `CronForm.vue` (parent
vhost select — disabled on edit, five schedule fields, command with
placeholder help, read-only derived type, log/active toggles) and
`CronRuns.vue` embedded in the edit screen (start, status, exit,
duration, output tail, empty state).

## Testing

```bash
go test ./internal/cron/... ./internal/api/...
go test -tags=integration ./internal/cron/...    # datalog → daemon → runner
make e2e-cron PANEL_URL=http://127.0.0.1:8096 ADMIN_PASSWORD=...
```

`make e2e-cron` accepts `CRON_SQL="docker exec -i <container> mariadb -uroot -proot <db>"`
to seed one `cron_run` row so the history table is asserted with real
data instead of the empty state.

## Migration notes

- The `cron` table is imported as-is by `migrate-from`; no shape change.
- Jobs migrated from a PHP panel start being executed by the daemon on
  its first start, and the corresponding `/etc/cron.d/ispc_*` files are
  removed in the same step.
- `type` is re-derived on every save, so a client whose
  `limit_cron_type` is tightened later gets the stricter type on the
  next edit.
- URL jobs that relied on PHP's `--no-check-certificate` need a valid
  certificate now, or must be converted to a `full`/`chrooted` command.
