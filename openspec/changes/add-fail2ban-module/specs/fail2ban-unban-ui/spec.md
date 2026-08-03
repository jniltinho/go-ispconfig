# fail2ban-unban-ui

## ADDED Requirements

### Requirement: Live state comes from fail2ban-client, not the log file
Jail and ban state SHALL be read exclusively by invoking `fail2ban-client` through the foundation `CommandRunner` with argv slices: `fail2ban-client status` for the jail list and `fail2ban-client status <jail>` for per-jail detail. The system SHALL NOT derive current ban state by parsing `/var/log/fail2ban.log` (that log is collected separately for the monitor view only).

#### Scenario: Jail list is read from the daemon
- **WHEN** `GET /api/fail2ban/jails` is called
- **THEN** the response is built from the `Jail list:` line of `fail2ban-client status` output

#### Scenario: Banned IPs are read per jail
- **WHEN** `GET /api/fail2ban/jails/sshd` is called
- **THEN** the response contains currently-failed, total-failed, currently-banned and total-banned counters and the `Banned IP list:` entries parsed from `fail2ban-client status sshd`

#### Scenario: Output parse failure degrades gracefully
- **WHEN** `fail2ban-client` returns output the parser does not recognise
- **THEN** the endpoint returns the jail with unknown counters rather than a 500, and the parse failure is logged

### Requirement: fail2ban-client absent is a clean, non-error state
When `fail2ban-client` is not on PATH or the daemon socket is unreachable, live-state endpoints SHALL return a success response flagged as unavailable (empty jail list plus an availability field) rather than a 5xx, so the panel can render an explanatory empty state.

#### Scenario: Host without fail2ban
- **WHEN** `GET /api/fail2ban/jails` is called on a host with no `fail2ban-client`
- **THEN** the response is 200 with an empty jail list and an availability flag indicating fail2ban is not installed

#### Scenario: Daemon stopped
- **WHEN** `fail2ban-client status` fails because the daemon is not running
- **THEN** the response is 200 with an availability flag indicating the daemon is down, and the UI shows the explanatory panel

### Requirement: Unban endpoint
`POST /api/fail2ban/jails/{jail}/unban` with body `{"ip":"<addr>"}` SHALL run `fail2ban-client set <jail> unbanip <ip>` and return the updated banned list for that jail. An IP that is not currently banned SHALL return a 200 with an explanatory flag, not an error.

#### Scenario: Banned IP is released
- **WHEN** an admin unbans an IP present in the jail's banned list
- **THEN** `fail2ban-client set <jail> unbanip <ip>` is invoked and the returned banned list no longer contains that IP

#### Scenario: Unbanning a non-banned IP is not an error
- **WHEN** an admin unbans an IP that is not currently banned
- **THEN** the response is 200 with a flag indicating no ban was present

### Requirement: Ban endpoint
`POST /api/fail2ban/jails/{jail}/ban` with body `{"ip":"<addr>"}` SHALL run `fail2ban-client set <jail> banip <ip>` and return the updated banned list.

#### Scenario: Manual ban is applied
- **WHEN** an admin bans `203.0.113.7` in the `sshd` jail
- **THEN** `fail2ban-client set sshd banip 203.0.113.7` is invoked and the returned banned list contains that IP

### Requirement: Input validation on jail name and IP
The `{jail}` path parameter SHALL match `^[A-Za-z0-9_-]{1,64}$` **and** be present in the live jail list before any command is built; unknown or malformed jail names SHALL return 404 (unknown) or 422 (malformed) without invoking `fail2ban-client`. The `ip` body field SHALL parse as an IP address or CIDR prefix; anything else SHALL return 422 without invoking `fail2ban-client`.

#### Scenario: Malformed jail name is rejected before exec
- **WHEN** a request targets jail `../../etc`
- **THEN** the response is 422 and no `fail2ban-client` invocation is recorded

#### Scenario: Unknown jail returns 404
- **WHEN** a request targets a syntactically valid jail name that is not in the live jail list
- **THEN** the response is 404 and no `fail2ban-client` invocation is recorded

#### Scenario: Malformed IP is rejected before exec
- **WHEN** a ban or unban body contains `ip: "1.2.3.4; rm -rf /"`
- **THEN** the response is 422 and no `fail2ban-client` invocation is recorded

### Requirement: Self-ban and whitelisted-ban are refused
The ban endpoint SHALL refuse, with 422, any target IP that equals the requesting client's resolved address (through the existing trusted-proxy resolution) or that falls inside the effective `ignoreip` set, since such a ban would either lock the admin out of the panel or be silently ignored by fail2ban.

#### Scenario: Admin cannot ban their own address
- **WHEN** an admin submits a ban for the address the request originates from
- **THEN** the response is 422 and no `fail2ban-client` invocation is recorded

#### Scenario: Whitelisted address is refused
- **WHEN** an admin submits a ban for an address inside the configured `ignoreip` set
- **THEN** the response is 422 explaining the address is whitelisted

#### Scenario: Unban of a whitelisted address is still allowed
- **WHEN** an admin unbans an address inside `ignoreip`
- **THEN** the request proceeds normally (unban never widens exposure)

### Requirement: Admin-only access and audit logging
All fail2ban endpoints SHALL be admin-only, gated by the same pattern as the firewall entity (`AdminOnly` plus an `admin_allow_fail2ban_config` security policy key). Every ban, unban and apply SHALL be logged with the acting panel user, the target jail, the target IP and the requesting source address.

#### Scenario: Non-admin is refused
- **WHEN** an authenticated non-admin user calls any `/api/fail2ban/*` endpoint
- **THEN** the response is 403 and no `fail2ban-client` invocation is recorded

#### Scenario: Policy key can further restrict access
- **WHEN** `admin_allow_fail2ban_config` restricts the capability and a non-matching admin calls the endpoint
- **THEN** the response is 403

#### Scenario: Ban action is audited
- **WHEN** an admin bans an IP
- **THEN** a log record contains the panel user, jail, target IP and the request source address

### Requirement: Config CRUD through the entity framework
Jail settings SHALL be exposed as an admin-only declarative `Entity` over the `server.config` `[fail2ban]` section (`GET`/`PUT /api/fail2ban/config`), producing a normal `server` row update with `{old,new}` datalog so the daemon plugin applies it. Field validation SHALL reject non-numeric `bantime`/`findtime`/`maxretry`, unknown `backend` values and invalid `ignoreip` entries.

#### Scenario: Saving settings triggers a daemon apply
- **WHEN** an admin saves changed fail2ban settings
- **THEN** the `server` row is updated, a datalog row is written, and the daemon re-renders `jail.local`

#### Scenario: Invalid whitelist entry is rejected
- **WHEN** a settings save submits `ignoreip` containing `not-an-ip`
- **THEN** the response is 422 with a field error and nothing is persisted

#### Scenario: Non-numeric bantime is rejected
- **WHEN** a settings save submits `bantime = forever`
- **THEN** the response is 422 and nothing is persisted

### Requirement: Explicit apply endpoint
`POST /api/fail2ban/reload` SHALL re-render the configuration, run the config test and reload the daemon, returning the test output when the test fails so the admin can see why the apply was rejected.

#### Scenario: Apply surfaces a failing config test
- **WHEN** an admin triggers an apply whose rendered configuration fails `fail2ban-client -t`
- **THEN** the response reports failure including the test output, and the previously active configuration remains in place

#### Scenario: Successful apply reloads
- **WHEN** an admin triggers an apply whose configuration passes the test
- **THEN** the configuration is activated, the daemon is reloaded and the response reports success

### Requirement: Fail2ban admin page
The panel SHALL add an admin-only `System → Fail2ban` entry directly below `System → Firewall` in the sidebar, rendering a jails view — per-jail name, enabled flag, currently-banned and total-banned counts, and effective `maxretry`/`findtime`/`bantime` — with an expandable banned-IP table offering per-row Unban (with confirmation) and a Ban-IP input, plus a settings view for global defaults, per-jail overrides and the `ignoreip` whitelist. Jails are per-server, so the page SHALL offer the same server selector the monitor views use.

#### Scenario: Jail list is visible to an admin
- **WHEN** an admin opens `/system/fail2ban` on a server with active jails
- **THEN** each jail is listed with its ban counts and current settings

#### Scenario: Unban from the UI removes the row
- **WHEN** an admin clicks Unban on a banned IP and confirms
- **THEN** the unban endpoint is called and the IP disappears from the refreshed banned list

#### Scenario: Whitelist editing round-trips
- **WHEN** an admin adds a CIDR to the whitelist and saves
- **THEN** the settings endpoint persists it and the value is present when the page is reloaded

#### Scenario: Missing fail2ban shows an explanatory state
- **WHEN** an admin opens the page on a server where fail2ban is not installed
- **THEN** the page renders an explanatory panel with installation guidance instead of an empty table or an error

#### Scenario: Non-admin never sees the entry
- **WHEN** a non-admin user is logged in
- **THEN** the `System → Fail2ban` sidebar entry is not rendered and the route is not reachable
