# firewall-ufw-plugin

## ADDED Requirements

### Requirement: Port list cleaning (clean_ports)
The plugin SHALL expose a pure `CleanPorts(list string) string` port of `firewall_plugin::clean_ports` with comma spacer: split on `,`; keep single ports whose integer value is in `1..65535`; keep ranges `a:b` only when both ends are in `1..65535` and lower < higher; drop all other tokens; re-join survivors with `,`. Empty or all-invalid input SHALL yield the empty string.

#### Scenario: Mixed valid and invalid tokens
- **WHEN** `CleanPorts` is called with `22,abc,80:90,70000,443,10:5`
- **THEN** the result is `22,80:90,443`

#### Scenario: Empty input
- **WHEN** `CleanPorts` is called with `""`
- **THEN** the result is `""`

#### Scenario: Whitespace-only tokens dropped
- **WHEN** `CleanPorts` is called with `22, ,443`
- **THEN** only valid numeric/range tokens remain (`22,443`)

### Requirement: UFW binary and version gate
Before mutating rules, the plugin SHALL verify `ufw` is installed and that `ufw --version` reports version ≥ `0.30`. If either check fails, the plugin SHALL log a warning and return without changing firewall state (PHP parity).

#### Scenario: UFW missing
- **WHEN** a `firewall_update` event fires and `ufw` is not on PATH
- **THEN** no allow/delete/enable/disable commands run and a warning is logged

#### Scenario: UFW too old
- **WHEN** `ufw --version` reports `0.29`
- **THEN** the plugin aborts the apply with a warning and leaves rules unchanged

### Requirement: Insert baseline resets UFW defaults
On `firewall_insert` only, before applying port diffs, the plugin SHALL run (in order): `ufw --force disable`, `ufw --force reset`, `ufw default deny incoming`, `ufw default allow outgoing` (port of `ufw_update` insert branch).

#### Scenario: First firewall record establishes default policy
- **WHEN** `firewall_insert` fires for an active record
- **THEN** the command sequence includes force-disable, force-reset, default deny incoming and default allow outgoing before any `ufw allow` for the record's ports

#### Scenario: Update does not reset
- **WHEN** `firewall_update` fires
- **THEN** the command sequence does not include `ufw --force reset`

### Requirement: Differential TCP/UDP allow and delete
On insert and update the plugin SHALL clean old/new `tcp_port` and `udp_port`, compute set differences, run `ufw allow <port>/tcp` (resp. `/udp`) for ports only in new with value `> 0`, and `ufw delete allow <port>/tcp` (resp. `/udp`) for ports only in old with value `> 0` (port of `ufw_update` loops). All invocations SHALL use the foundation command runner with argv slices (no shell string interpolation).

#### Scenario: New TCP port is allowed
- **WHEN** update changes `tcp_port` from `22,80` to `22,80,443`
- **THEN** exactly one new allow command is issued: `ufw allow 443/tcp`

#### Scenario: Removed UDP port is deleted
- **WHEN** update changes `udp_port` from `53,123` to `53`
- **THEN** a `ufw delete allow 123/udp` command is issued and `53/udp` is not deleted

#### Scenario: Range tokens are passed through
- **WHEN** `tcp_port` adds `40110:40210`
- **THEN** the allow argument is `40110:40210/tcp`

### Requirement: Active flag controls enable, reload and disable
When `new.active == 'y'` and old active was also `'y'`, the plugin SHALL run `ufw reload`. When `new.active == 'y'` and old active was not `'y'` (including insert), the plugin SHALL run `ufw --force enable`. When `new.active != 'y'`, the plugin SHALL run `ufw disable` (port of `ufw_update` active branch; Bastille stop/update-rc.d is not ported).

#### Scenario: Freshly activated firewall is force-enabled
- **WHEN** update sets `active` from `n` to `y`
- **THEN** `ufw --force enable` is executed (not merely reload)

#### Scenario: Port change on already-active firewall reloads
- **WHEN** update changes ports while `active` stays `y`
- **THEN** `ufw reload` is executed and `ufw --force enable` is not

#### Scenario: Deactivated firewall is disabled
- **WHEN** update sets `active` to `n`
- **THEN** `ufw disable` is executed

### Requirement: Delete resets and disables UFW
On `firewall_delete` the plugin SHALL run `ufw --force reset` then `ufw disable` (port of `ufw_delete`), subject to the lock-out guard for any interim state.

#### Scenario: Firewall record deletion stops UFW
- **WHEN** `firewall_delete` fires and UFW is installed
- **THEN** the command sequence includes force-reset and disable

### Requirement: Lock-out guard for panel and SSH ports
The plugin SHALL compute protected TCP ports as: the panel listen port from `config.toml` `server.port` (fallback `8080`) and the SSH port from `server.config` `[server] ssh_port` (fallback `22`). Before any enable (and after any reset that will be followed by an enabled state), the effective TCP allow set SHALL be the cleaned record ports union the protected ports. The plugin SHALL never issue `ufw delete allow <protected>/tcp` when the resulting enabled state would lack that port. A unit test with a recording command runner SHALL assert that no apply fixture ends with UFW enabled without both protected ports allowed.

#### Scenario: Empty tcp_port on insert still opens protected ports
- **WHEN** `firewall_insert` fires with `tcp_port=""` and `active=y`
- **THEN** allow commands for both the panel port and the SSH port are issued before `ufw --force enable`

#### Scenario: Admin removes SSH from the list while active
- **WHEN** `firewall_update` changes `tcp_port` from `22,80,443` to `80,443` with `active=y` and SSH port is `22`
- **THEN** no `ufw delete allow 22/tcp` is issued (or it is immediately re-allowed before reload/enable completes)

#### Scenario: Custom panel port is protected
- **WHEN** `config.toml` sets `server.port = 9443` and a record omits `9443` from `tcp_port` with `active=y`
- **THEN** `ufw allow 9443/tcp` is part of the effective apply set

#### Scenario: Lock-out test rejects a broken apply sequence
- **WHEN** the lock-out unit suite runs a fixture that would enable UFW without the SSH port
- **THEN** the test fails (guard invariant is mandatory CI coverage)

### Requirement: Bastille path is not executed
The plugin SHALL NOT write Bastille config, start/stop `bastille-firewall`, or call `update-rc.d`/`insserv` for Bastille (proposal non-goal), regardless of `server.config` `[server] firewall` value.

#### Scenario: firewall=bastille still uses UFW path
- **WHEN** getconf reports `firewall=bastille` and UFW is installed
- **THEN** the UFW update path runs and no Bastille commands are invoked
