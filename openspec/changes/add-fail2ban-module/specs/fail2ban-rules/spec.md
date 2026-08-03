# fail2ban-rules

## ADDED Requirements

### Requirement: Jail catalogue
The system SHALL ship a fixed catalogue of six jails — `sshd`, `postfix`, `postfix-sasl`, `dovecot`, `pure-ftpd` and `goisp-panel` — each with a name, a filter reference, a service gate, a default action and default `maxretry`/`findtime`/`bantime`. The catalogue SHALL be a pure data structure with no filesystem or command access, so it is table-testable. Jails absent from the catalogue SHALL NOT be rendered, and operator jails in `/etc/fail2ban/jail.d/` SHALL remain outside its scope.

#### Scenario: Catalogue exposes exactly the six jails
- **WHEN** the catalogue is enumerated
- **THEN** it yields `sshd`, `postfix`, `postfix-sasl`, `dovecot`, `pure-ftpd` and `goisp-panel` and no others

#### Scenario: Unknown jail name is not rendered
- **WHEN** `server.config` `[fail2ban]` contains a `jail_something_else = y` key with no catalogue entry
- **THEN** the key is ignored and no `[something_else]` stanza appears in the rendered `jail.local`

### Requirement: Global defaults in the DEFAULT stanza
The rendered `jail.local` SHALL open with a `[DEFAULT]` stanza carrying `ignoreip`, `bantime`, `findtime`, `maxretry` and `backend` from `server.config` `[fail2ban]`. When a key is absent, the defaults SHALL be `bantime = 1200`, `findtime = 1200`, `maxretry = 5`, `backend = auto` (the `bantime`/`findtime` values match ISPConfig3's only fail2ban template, `install/tpl/dovecot_fail2ban_jail.local.master`).

#### Scenario: Defaults applied when the section is sparse
- **WHEN** `[fail2ban]` contains only `enabled = y`
- **THEN** the rendered `[DEFAULT]` stanza contains `bantime = 1200`, `findtime = 1200`, `maxretry = 5` and `backend = auto`

#### Scenario: Configured values override the defaults
- **WHEN** `[fail2ban]` sets `bantime = 86400` and `maxretry = 3`
- **THEN** `[DEFAULT]` renders `bantime = 86400` and `maxretry = 3`

### Requirement: Per-jail overrides
Each catalogue jail SHALL be enabled by a `jail_<name> = y` key and MAY override the global `maxretry`, `findtime`, `bantime` and `action` via `jail_<name>_<option>` keys. Unset per-jail options SHALL be omitted from the stanza so the `[DEFAULT]` value applies.

#### Scenario: Per-jail maxretry overrides the default
- **WHEN** `[fail2ban]` sets `maxretry = 5` and `jail_sshd_maxretry = 3`
- **THEN** the `[sshd]` stanza contains `maxretry = 3` and other jails inherit `5` from `[DEFAULT]`

#### Scenario: Unset options are omitted
- **WHEN** no `jail_dovecot_bantime` key is present
- **THEN** the `[dovecot]` stanza contains no `bantime` line

#### Scenario: Disabled jail renders as disabled
- **WHEN** `jail_pureftpd` is absent or not `y`
- **THEN** either no `[pure-ftpd]` stanza is rendered or it is rendered with `enabled = false`

### Requirement: Service gating
Each jail SHALL be enabled only when its service gate passes: `sshd` and `goisp-panel` always; `postfix`, `postfix-sasl` and `dovecot` only when the local `server` row has `mail_server = 1`; `pure-ftpd` only when `web_server = 1` (the same gate `internal/installer/ftpstep.go` uses for the `pure-ftpd-mysql` package).

#### Scenario: DNS-only node gets no mail jails
- **WHEN** the local server has `mail_server = 0` and `web_server = 0` and every jail key is `y`
- **THEN** only `sshd` and `goisp-panel` are enabled in the rendered file

#### Scenario: Mail server gets the mail jails
- **WHEN** the local server has `mail_server = 1` and `jail_dovecot = y`
- **THEN** the `[dovecot]` stanza is rendered with `enabled = true`

### Requirement: Log path and backend resolution
For each enabled jail the system SHALL resolve a log source: `sshd` from `/var/log/auth.log`; `postfix`, `postfix-sasl` and `dovecot` from `/var/log/mail.log`; `pure-ftpd` from `/var/log/syslog`; `goisp-panel` from the panel log path already configured for the monitor module. When the resolved path does not exist, the jail SHALL be rendered with `backend = systemd` and no `logpath`. When neither the path exists nor a systemd backend is usable, the jail SHALL be rendered disabled with a logged warning rather than aborting the whole render. Resolution SHALL be a pure function taking a filesystem probe, so it is testable without touching the real filesystem.

#### Scenario: Classic rsyslog host uses file paths
- **WHEN** `/var/log/auth.log` and `/var/log/mail.log` exist
- **THEN** `[sshd]` renders `logpath = /var/log/auth.log` and `[dovecot]` renders `logpath = /var/log/mail.log`

#### Scenario: rsyslog-less Debian 12 falls back to systemd
- **WHEN** `/var/log/auth.log` does not exist and the systemd backend is usable
- **THEN** `[sshd]` renders `backend = systemd` and contains no `logpath` line

#### Scenario: No usable source disables just that jail
- **WHEN** the dovecot log is absent and the systemd backend is unusable
- **THEN** `[dovecot]` renders disabled, a warning is logged, and the other jails render normally

### Requirement: ignoreip whitelist always contains loopback
The effective `ignoreip` written to `jail.local` SHALL be the configured value unioned with `127.0.0.1/8` and `::1`, deduplicated and space-separated. Every configured entry SHALL be a valid IP or CIDR; invalid entries SHALL be rejected at the API boundary and dropped with a warning at render time.

#### Scenario: Loopback is present even when not configured
- **WHEN** `[fail2ban] ignoreip` is empty
- **THEN** the rendered `[DEFAULT]` contains `ignoreip = 127.0.0.1/8 ::1`

#### Scenario: Configured entries are merged without duplication
- **WHEN** `ignoreip` is configured as `127.0.0.1/8 203.0.113.0/24`
- **THEN** the rendered value contains `127.0.0.1/8`, `::1` and `203.0.113.0/24` with `127.0.0.1/8` appearing once

#### Scenario: Invalid entry is dropped, not fatal
- **WHEN** `ignoreip` contains `not-an-ip` alongside valid entries
- **THEN** the invalid token is omitted from the rendered value, a warning is logged, and the render succeeds

#### Scenario: Loopback removal is impossible
- **WHEN** any rendered `jail.local` fixture is checked by the lock-out test suite
- **THEN** every fixture's `ignoreip` includes both `127.0.0.1/8` and `::1`, and a fixture missing either fails the test

### Requirement: Default ban actions
Each jail SHALL default to a multiport ban action naming the jail and its protected ports, following the shape of ISPConfig3's only fail2ban template (`action = iptables-multiport[name=<jail>, port="<ports>", protocol=tcp]`): `sshd` → the resolved SSH port; `postfix`/`postfix-sasl` → `smtp,submission,submissions`; `dovecot` → `pop3,pop3s,imap,imaps`; `pure-ftpd` → `ftp,ftp-data,ftps`; `goisp-panel` → the panel listen port from `config.toml` `server.port`. The action SHALL be overridable per jail.

#### Scenario: Dovecot jail keeps the ISPConfig port set
- **WHEN** the `[dovecot]` stanza is rendered with default action
- **THEN** the action ports are `pop3,pop3s,imap,imaps`

#### Scenario: Panel jail follows the configured listen port
- **WHEN** `config.toml` sets `server.port = 9443`
- **THEN** the `[goisp-panel]` action protects port `9443`

#### Scenario: SSH jail follows the configured SSH port
- **WHEN** `server.config` `[server] ssh_port` is `2222`
- **THEN** the `[sshd]` action protects port `2222`

### Requirement: Panel jail filter
The system SHALL ship `/etc/fail2ban/filter.d/goisp-panel.conf` whose `failregex` matches the panel's dedicated failed-login log line and nothing else, and the panel login handler SHALL emit that line on every authentication failure with the client IP resolved through the existing trusted-proxy logic. The filter SHALL be pinned by golden tests against captured sample log lines, including negative samples that MUST NOT match.

#### Scenario: Failed login produces a matching line
- **WHEN** a login attempt fails
- **THEN** the panel log contains one failed-login line carrying the resolved client IP, and the filter's `failregex` matches it with the IP captured as `<HOST>`

#### Scenario: Successful login does not match
- **WHEN** a login attempt succeeds
- **THEN** no line in the panel log matches the filter's `failregex`

#### Scenario: Proxied client IP is used, not the proxy IP
- **WHEN** the request arrives through a configured trusted proxy
- **THEN** the logged IP is the forwarded client address, not the proxy address

### Requirement: Rendered file carries a managed header
The generated `jail.local` SHALL begin with a comment identifying it as generated by go-ispconfig and warning that manual edits are overwritten, and SHALL point operators at `/etc/fail2ban/jail.d/` for their own drop-ins.

#### Scenario: Generated file is self-identifying
- **WHEN** `jail.local` is rendered
- **THEN** its first lines are a comment naming go-ispconfig as the owner and referencing `/etc/fail2ban/jail.d/` for local additions
