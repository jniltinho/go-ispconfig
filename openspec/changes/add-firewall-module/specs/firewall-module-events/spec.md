# firewall-module-events

## ADDED Requirements

### Requirement: Firewall table hook raises named events
The daemon firewall module SHALL register a table hook for `firewall` and announce/raise the events `firewall_insert`, `firewall_update` and `firewall_delete`, mapping datalog actions `i`/`u`/`d` respectively (port of `server_module.inc.php::process` case `firewall`).

#### Scenario: Insert datalog row dispatches firewall_insert
- **WHEN** the daemon processes a `sys_datalog` row with `dbtable=firewall` and `action=i`
- **THEN** the `firewall_insert` event is raised with the `{old,new}` payload

#### Scenario: Update datalog row dispatches firewall_update
- **WHEN** the daemon processes a `sys_datalog` row with `dbtable=firewall` and `action=u`
- **THEN** the `firewall_update` event is raised with the `{old,new}` payload

#### Scenario: Delete datalog row dispatches firewall_delete
- **WHEN** the daemon processes a `sys_datalog` row with `dbtable=firewall` and `action=d`
- **THEN** the `firewall_delete` event is raised with the `{old,new}` payload

#### Scenario: Unregistered plugin cannot subscribe to unannounced event
- **WHEN** a plugin attempts to register a handler for an event the firewall module did not announce
- **THEN** registration is rejected (foundation registry contract)

### Requirement: Module enablement follows server role and config
The firewall module SHALL only load when the daemon's local server record has `firewall_server = 1` and the module is enabled in `config.toml`.

#### Scenario: Non-firewall server skips module
- **WHEN** the daemon starts on a server whose `server.firewall_server` flag is 0
- **THEN** no firewall table hook is registered and firewall datalog rows are ignored by this module

#### Scenario: Module disabled in config
- **WHEN** `config.toml` disables the firewall module while `firewall_server = 1`
- **THEN** no firewall table hook is registered

### Requirement: Events only applied for the local server_id
The firewall plugin handlers SHALL no-op when the event payload's `server_id` (from `new` or, on delete, `old`) does not match the daemon's local server id.

#### Scenario: Foreign server row is ignored
- **WHEN** a `firewall_update` event carries `server_id` for another server
- **THEN** no UFW commands are executed
