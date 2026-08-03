# fail2ban-jails

## ADDED Requirements

### Requirement: Module announces fail2ban events and registers the service
The fail2ban module SHALL register a table hook for its config surface, announce `fail2ban_insert`, `fail2ban_update` and `fail2ban_delete`, and register the `fail2ban` system service with the foundation services registry for delayed reload (same shape as `internal/firewall/module.go`). It SHALL be loaded only when the module is enabled in `config.toml` and `server.config` `[fail2ban] enabled = y`.

#### Scenario: Module announces its events on load
- **WHEN** the daemon loads the fail2ban module
- **THEN** `fail2ban_insert`, `fail2ban_update` and `fail2ban_delete` are announced and the `fail2ban` service is registered for delayed reload

#### Scenario: Module stays unloaded when disabled
- **WHEN** `server.config` has no `[fail2ban]` section, or `[fail2ban] enabled` is not `y`
- **THEN** the module is not loaded, no events are announced and no file under `/etc/fail2ban` is written

### Requirement: Plugin applies on server_update and fail2ban events
The plugin SHALL subscribe to `server_update` (the event raised when the `[fail2ban]` getconf section is edited) and to `fail2ban_insert|update|delete`, and SHALL ignore any event whose payload `server_id` is neither zero nor the local server id (port of the `isLocal` gate in `internal/firewall/plugin.go`).

#### Scenario: Config edit re-renders the jail file
- **WHEN** an admin saves fail2ban settings and a `server_update` event fires for the local server
- **THEN** the plugin re-renders `/etc/fail2ban/jail.local` and queues a delayed `fail2ban` reload

#### Scenario: Event for another server is ignored
- **WHEN** a `server_update` event arrives with `server_id` of a different server
- **THEN** the plugin makes no filesystem change and issues no command

### Requirement: Binary probe gates every apply
Before rendering or applying, the plugin SHALL verify `fail2ban-client` is present on PATH. If it is absent, the plugin SHALL log a warning and return without writing files or running commands (PHP-free parity with the UFW probe in `internal/firewall/ufw.go`).

#### Scenario: fail2ban not installed
- **WHEN** an apply is triggered on a host without `fail2ban-client`
- **THEN** no file under `/etc/fail2ban` is written, no command runs, and a warning is logged

### Requirement: Only jail.local and goisp filters are owned
The plugin SHALL write exactly one generated jail file at `/etc/fail2ban/jail.local`, carrying a `# Managed by go-ispconfig` header, and MAY write filter files matching `/etc/fail2ban/filter.d/goisp-*.conf`. It SHALL NOT write, modify or delete `/etc/fail2ban/jail.conf`, `/etc/fail2ban/fail2ban.conf`, any file under `/etc/fail2ban/jail.d/`, or any packaged filter under `/etc/fail2ban/filter.d/` that does not match the `goisp-` prefix.

#### Scenario: Operator drop-ins survive an apply
- **WHEN** `/etc/fail2ban/jail.d/local-extra.conf` exists and an apply runs
- **THEN** that file is byte-identical after the apply

#### Scenario: Packaged sshd filter is referenced, not rewritten
- **WHEN** the `sshd` jail is enabled
- **THEN** `jail.local` contains `filter = sshd` and `/etc/fail2ban/filter.d/sshd.conf` is not written

#### Scenario: Panel filter is written under the owned prefix
- **WHEN** the `goisp-panel` jail is enabled
- **THEN** `/etc/fail2ban/filter.d/goisp-panel.conf` is written and referenced as `filter = goisp-panel`

### Requirement: Unmanaged jail.local is backed up before first managed write
When `/etc/fail2ban/jail.local` exists and does not carry the managed-file header, the plugin SHALL copy it to `/etc/fail2ban/jail.local~` before writing the generated file.

#### Scenario: Hand-written jail.local is preserved
- **WHEN** an apply runs on a migrated host whose `jail.local` was written by hand
- **THEN** `/etc/fail2ban/jail.local~` contains the original content and `/etc/fail2ban/jail.local` contains the generated content

#### Scenario: Second apply does not re-backup
- **WHEN** an apply runs against an already-managed `jail.local`
- **THEN** no new backup is created and the previous `jail.local~` is left untouched

### Requirement: Config test precedes activation
The plugin SHALL write the generated file to a temporary path, run `fail2ban-client -t` against the resulting configuration, and only move it into place when the test exits zero. On a non-zero exit the plugin SHALL leave the existing `/etc/fail2ban/jail.local` unchanged, SHALL NOT queue a reload, and SHALL log the test output.

#### Scenario: Failing config test aborts the apply
- **WHEN** the rendered configuration fails `fail2ban-client -t`
- **THEN** the previous `jail.local` content is intact, no reload is queued, and the test output is logged

#### Scenario: Passing config test activates and reloads
- **WHEN** the rendered configuration passes `fail2ban-client -t` and differs from the current file
- **THEN** the file is moved into place and a delayed `fail2ban` reload is queued

#### Scenario: Unchanged render skips the reload
- **WHEN** the rendered configuration is byte-identical to the current `jail.local`
- **THEN** the file is not rewritten and no reload is queued (idempotent re-apply)

### Requirement: Commands run through the foundation CommandRunner with argv
Every `fail2ban-client` invocation SHALL go through the foundation `CommandRunner` with an argv slice. The plugin SHALL NOT build shell command strings or interpolate configuration values into a shell.

#### Scenario: Reload uses argv
- **WHEN** a reload is applied
- **THEN** the recorded invocation is the program `fail2ban-client` with argument `reload`, not a shell string

### Requirement: Firewall reset re-installs bans
The plugin SHALL subscribe to `firewall_insert` and `firewall_delete` and queue a `fail2ban` reload after them, because those paths run `ufw --force reset`, which flushes the `f2b-*` chains fail2ban owns (design D12).

#### Scenario: UFW reset is followed by a fail2ban reload
- **WHEN** a `firewall_insert` event is applied on a server with fail2ban enabled
- **THEN** a delayed `fail2ban` reload is queued after the UFW commands

### Requirement: Installer provisions fail2ban on fresh installs
The installer SHALL add `fail2ban` and `python3-systemd` to the profile package set, and a `fail2ban` step SHALL seed the `[fail2ban]` defaults into `server.config` and write the first `jail.local` through the same renderer the daemon plugin uses. The step SHALL be skipped when the answers opt out, and SHALL be idempotent on re-run.

#### Scenario: Fresh install is protected by default
- **WHEN** the installer pipeline runs with fail2ban enabled on a clean host
- **THEN** the `fail2ban` package is installed, the unit is enabled and started, `[fail2ban]` defaults exist in `server.config`, and `/etc/fail2ban/jail.local` is generated

#### Scenario: Opting out skips the step
- **WHEN** the answers disable fail2ban
- **THEN** the step reports skipped, no package is installed and no `/etc/fail2ban` file is written

#### Scenario: Re-running the installer changes nothing
- **WHEN** the pipeline is re-run on an already-provisioned host with unchanged settings
- **THEN** the step reports no change and queues no reload

### Requirement: Monitor collects the fail2ban log and service state
The monitor module's deferred `log_fail2ban` collector SHALL be implemented by tailing `/var/log/fail2ban.log` through the existing `monitor.TailFile` helper and storing it as `monitor_data` type `log_fail2ban` (parity with `cron.d/100-monitor_fail2ban.inc.php` and `monitor_tools.inc.php`), and a `fail2ban` service probe SHALL be added to the monitor services check.

#### Scenario: Log is collected on schedule
- **WHEN** the monitor job runs on a host where fail2ban is installed
- **THEN** a `monitor_data` row of type `log_fail2ban` contains the tail of `/var/log/fail2ban.log`

#### Scenario: Absent fail2ban yields empty data, not an error
- **WHEN** the monitor job runs on a host without fail2ban
- **THEN** the stored data is empty with state `no_state` and the job does not fail
