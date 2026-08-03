# getmail-cron

## ADDED Requirements

### Requirement: getmail_fetch scheduler job
The getmail plugin SHALL register a scheduler job named `getmail_fetch` with the cron spec `*/5 * * * *`, wired from the daemon bootstrap on mail servers only (replacement for the `*/5 * * * * /usr/local/bin/run-getmail.sh` crontab that `installer_base.lib.php` writes for the `getmail` user). No crontab and no systemd timer SHALL be created for getmail.

#### Scenario: Job is registered on a mail server
- **WHEN** the daemon starts on a server with `mail_server = 1` and the mail module enabled
- **THEN** a scheduler job `getmail_fetch` with spec `*/5 * * * *` is registered

#### Scenario: Job is absent on a non-mail server
- **WHEN** the daemon starts on a server without the mail role
- **THEN** no `getmail_fetch` job is registered

#### Scenario: Job state is visible to the panel
- **WHEN** `getmail_fetch` has executed at least once
- **THEN** its last-run time and status are persisted in the scheduler's `sys_config` group and returned by the scheduler job listing

### Requirement: rc file discovery and invocation
Each run SHALL list `*.conf` entries directly in `getmail_config_dir` (non-recursive), sort them for a deterministic argv, and invoke the configured getmail program **once** with `-g <getmail_config_dir>` followed by one `-r <file>` argument per rc file — the argv `run-getmail.sh` assembles. When no rc file exists the job SHALL return without executing anything.

#### Scenario: Multiple accounts fetch in one invocation
- **WHEN** three rc files exist in the config directory
- **THEN** the getmail program is executed exactly once with `-g <config dir>` and three `-r` arguments, in sorted filename order

#### Scenario: Empty config directory skips execution
- **WHEN** the config directory contains no `*.conf` file
- **THEN** no process is started and the job succeeds

#### Scenario: Non-conf files are ignored
- **WHEN** the config directory contains `oldmail-…` state files or subdirectories alongside `*.conf` files
- **THEN** only the `*.conf` files appear as `-r` arguments

### Requirement: Privilege drop to the getmail user
The fetch SHALL run as the configured getmail user, never as root, by resolving that user's uid/gid and dropping privileges for the invocation (parity with the PHP crontab, which is installed under `crontab -u getmail`). Failure to resolve the user or to start the program SHALL be a job error.

#### Scenario: Fetch runs unprivileged
- **WHEN** the job invokes the getmail program
- **THEN** the process runs with the uid and gid of the configured getmail user

#### Scenario: Unresolvable getmail user fails the job
- **WHEN** the configured getmail user does not exist on the system
- **THEN** the job returns an error, no process is started, and the scheduler records the failure status

### Requirement: Remote failures do not fail the run
A non-zero exit status from the getmail program SHALL be logged together with its captured output but SHALL NOT be returned as a job error, so one unreachable remote server cannot mask the successful accounts (parity with `getmail … || true` in `run-getmail.sh`). Failures to start the program remain job errors.

#### Scenario: One dead remote server does not fail the job
- **WHEN** the getmail program exits non-zero because one account's remote server is unreachable
- **THEN** the output is logged and the job reports success

#### Scenario: Missing getmail binary fails the job
- **WHEN** the configured getmail program does not exist
- **THEN** the job returns an error and the scheduler records the failure status

### Requirement: Single-flight guard
A `getmail_fetch` activation that starts while a previous one is still running SHALL log and return immediately instead of starting a second fetch (replacement for the `/tmp/.getmail_lock` sentinel in `run-getmail.sh`, which exists to stop overlapping five-minute runs from double-fetching).

#### Scenario: Overlapping activation is skipped
- **WHEN** the scheduler activates `getmail_fetch` while a previous run has not finished
- **THEN** the second activation starts no process, logs that a fetch is already running, and returns success

#### Scenario: Guard is released after a failed run
- **WHEN** a run ends with an error
- **THEN** the next activation is allowed to start a fetch
