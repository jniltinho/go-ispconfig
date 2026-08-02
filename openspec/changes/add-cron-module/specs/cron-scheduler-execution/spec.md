# cron-scheduler-execution

## ADDED Requirements

### Requirement: Client jobs run in the daemon, never via system crontab
The cron plugin SHALL register, update and remove client jobs on an in-process client-job runner driven by `cron_insert` / `cron_update` / `cron_delete` events. It SHALL NOT write, update or create any file under the getconf `cron.crontab_dir` (deliberate divergence from `cron_plugin.inc.php::_write_crontab`).

#### Scenario: Active job is registered on insert
- **WHEN** `cron_insert` fires for a row with `active='y'` and a valid parent `web_domain` on this server
- **THEN** the client-job runner holds a scheduled entry for that `cron.id` and no file is created under `crontab_dir`

#### Scenario: Inactive job is not scheduled
- **WHEN** `cron_insert` or `cron_update` fires for a row with `active='n'`
- **THEN** no scheduled entry exists for that `cron.id`

#### Scenario: Delete removes the scheduled entry
- **WHEN** `cron_delete` fires for a previously active job
- **THEN** the client-job runner no longer has an entry for that `cron.id`

#### Scenario: Daemon start reloads active jobs from the database
- **WHEN** the daemon starts with existing `cron` rows where `active='y'` and `server_id` is this server
- **THEN** each such row is registered in the client-job runner without requiring a new datalog event

### Requirement: Schedule composition from cron table fields
The runner SHALL compose schedules from the five columns `run_min`, `run_hour`, `run_mday`, `run_month`, `run_wday` (spaces stripped per field, port of `_write_crontab`). When `run_month` equals `@reboot`, the job SHALL run once at daemon start instead of as a recurring expression.

#### Scenario: Five-field expression is evaluated in-process
- **WHEN** a job has `run_min='*/5'`, `run_hour='*'`, `run_mday='*'`, `run_month='*'`, `run_wday='*'`
- **THEN** the runner schedules it on a standard 5-field cron expression `*/5 * * * *` and fires at the matching minutes

#### Scenario: @reboot runs on daemon start
- **WHEN** the daemon starts and an active job has `run_month='@reboot'`
- **THEN** that job is executed once during startup (not as a recurring entry)

### Requirement: URL job execution
For `type='url'` the plugin SHALL perform an HTTP GET of the command after substituting `{DOMAIN}` with the parent site's domain, with a timeout (default 7200 seconds). Commands containing backslash, CR, LF or NUL SHALL be refused and logged (PHP parity).

#### Scenario: URL job fetches the configured endpoint
- **WHEN** a due URL job has `command='https://{DOMAIN}/cron.php'` and parent domain `example.com`
- **THEN** the plugin issues `GET https://example.com/cron.php` and records the outcome

#### Scenario: Insecure URL command is skipped
- **WHEN** a URL job's command contains a backslash or a newline
- **THEN** the job is not executed and a warning is logged

### Requirement: Full and chrooted job execution as the site user
For `type='full'` and `type='chrooted'` the plugin SHALL execute the command as the parent site's `web_domain.system_user` / `system_group` via direct exec (argv slices, no shell), with working directory `{document_root}/web` for `full` (and the equivalent path rules for `chrooted`, including stripping a leading `document_root` prefix from the command when present). Placeholders `{DOMAIN}`, `{DOCROOT_CLIENT}`, `[web_root]` and `{SITE_PHP}` SHALL be expanded before exec (`{SITE_PHP}` from `server_php.php_cli_binary` joined via `web_domain.server_php_id`, default `/usr/bin/php`). Jailkit chroot SHALL NOT be required in this change; `chrooted` runs as the site user without a jail.

#### Scenario: Full job runs as the site system user
- **WHEN** a due `full` job's parent site has `system_user=web1` with a non-root uid
- **THEN** the process runs under that uid/gid with cwd at `{document_root}/web` and the expanded command argv

#### Scenario: Placeholder expansion for PHP CLI
- **WHEN** a job command contains `{SITE_PHP}` and the site's PHP version has `php_cli_binary=/usr/bin/php8.3`
- **THEN** the executed argv uses `/usr/bin/php8.3` in place of `{SITE_PHP}`

#### Scenario: Chrooted command path is stripped of document_root
- **WHEN** a `chrooted` job command starts with the parent `document_root`
- **THEN** that prefix is removed before execution (PHP parity)

### Requirement: Fail-safe privilege drop
Non-URL jobs SHALL set `NoNewPrivileges` and drop to the site uid/gid before exec. If the site user/group is root, missing, or the privilege drop fails for any reason, the job SHALL be aborted and logged — a job is never executed as root. An execution timeout SHALL kill the process group.

#### Scenario: Root-owned site aborts the job
- **WHEN** a `full` job's parent site has `system_user` resolving to uid 0
- **THEN** the job is not started and an error is logged

#### Scenario: Privilege drop failure aborts the job
- **WHEN** the site user cannot be resolved or Credential setup fails
- **THEN** the job is aborted without starting a child process and the failure is logged

#### Scenario: Timeout kills the process
- **WHEN** a `full` job exceeds the configured execution timeout
- **THEN** the process group is killed and the run is recorded as `status=timeout`

### Requirement: Execution log in sys_log
When `cron.log='y'`, each finished run SHALL insert a `sys_log` row with `datalog_id=0`, this `server_id`, a timestamp, and a `message` matching the convention `cron_run id=<id> parent_domain_id=<pd> type=<type> status=<ok|exit|timeout|error> exit=<code> start=<unix> end=<unix> output=<tail>` with a bounded output tail. When `log='n'`, successful runs produce no row; privilege-drop aborts and similar security failures SHALL still be logged at error level.

#### Scenario: Logged job writes a sys_log row
- **WHEN** a job with `log='y'` completes
- **THEN** a `sys_log` row exists whose message starts with `cron_run id=<that id>` and includes status and timestamps

#### Scenario: Unlogged successful run writes nothing
- **WHEN** a job with `log='n'` completes with status ok
- **THEN** no `sys_log` row with that cron id is inserted for the run

### Requirement: Legacy OS crontab cutover
On plugin load the runner SHALL list and remove legacy ISPConfig crontab files under getconf `cron.crontab_dir` matching `ispc_*` and `ispc_chrooted_*` (including Gentoo-style `*.cron` suffix), log each removal, then register active DB jobs — so no job runs both under vixie-cron and under the daemon after cutover.

#### Scenario: Legacy ispc crontab files are removed at startup
- **WHEN** the daemon starts and `/etc/cron.d/ispc_web1` exists
- **THEN** that file is deleted and the removal is logged before client jobs are registered

#### Scenario: No double execution after cutover
- **WHEN** cutover has removed legacy files and active DB jobs are registered in-process
- **THEN** only the daemon client-job runner executes those jobs
