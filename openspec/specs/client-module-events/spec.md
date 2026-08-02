# client-module-events Specification

## Purpose
TBD - created by archiving change add-client-module. Update Purpose after archive.
## Requirements

### Requirement: Client table hooks raise named events
The daemon client module SHALL register a table hook for `client` and announce/raise the events `client_insert`, `client_update` and `client_delete`, mapping datalog actions `i` / `u` / `d` respectively (port of `client_module.inc.php::process`). Event payloads SHALL be the decoded `{old,new}` datalog data.

#### Scenario: Client insert datalog row dispatches client_insert
- **WHEN** the daemon processes a `sys_datalog` row with `dbtable = client` and `action = i`
- **THEN** the `client_insert` event is raised with the `{old,new}` payload

#### Scenario: Client update datalog row dispatches client_update
- **WHEN** the daemon processes a `sys_datalog` row with `dbtable = client` and `action = u`
- **THEN** the `client_update` event is raised with the `{old,new}` payload

#### Scenario: Client delete datalog row dispatches client_delete
- **WHEN** the daemon processes a `sys_datalog` row with `dbtable = client` and `action = d`
- **THEN** the `client_delete` event is raised with the `{old,new}` payload (plugins such as nginx may remove `/var/www/clients/client{N}`)

#### Scenario: Unregistered plugin cannot subscribe to unannounced event
- **WHEN** a plugin attempts to register a handler for an event the client module did not announce
- **THEN** registration is rejected (foundation registry contract)

### Requirement: Module loads for broadcast client datalog
The client module SHALL load with the daemon regardless of web/dns server role flags, because `client` datalog rows use `server_id = 0` and must be visible on every node for local cleanup handlers. It MAY still be disableable via `config.toml` for emergency rollback.

#### Scenario: Non-web server still receives client_delete
- **WHEN** the daemon runs on a DNS-only server and a `client` delete datalog row with `server_id = 0` is processed
- **THEN** the client module raises `client_delete` so any subscribed local handlers run

### Requirement: No OS mutation inside the client module itself
The client module SHALL only translate table hooks into events. It SHALL NOT write files, manage users/groups, or restart services; those remain the responsibility of plugins subscribed to the announced events.

#### Scenario: client_update alone leaves filesystem untouched by this module
- **WHEN** only the client module handles a `client_update` and no plugin is subscribed
- **THEN** no filesystem or service action is performed by the client module
