# database-module-events Specification

## Purpose
TBD - created by archiving change add-database-module. Update Purpose after archive.
## Requirements
### Requirement: Database table hooks raise named events
The daemon database module SHALL register table hooks for `web_database` and `web_database_user` and announce/raise the events `database_insert`, `database_update`, `database_delete`, `database_user_insert`, `database_user_update`, `database_user_delete`, mapping datalog actions `i`/`u`/`d` respectively (port of `database_module.inc.php::process`).

#### Scenario: Database update datalog row dispatches database_update
- **WHEN** the daemon processes a `sys_datalog` row with `dbtable=web_database` and `action=u`
- **THEN** the `database_update` event is raised with the `{old,new}` payload

#### Scenario: Database user delete datalog row dispatches database_user_delete
- **WHEN** the daemon processes a `sys_datalog` row with `dbtable=web_database_user` and `action=d`
- **THEN** the `database_user_delete` event is raised with the `{old,new}` payload

#### Scenario: Unregistered plugin cannot subscribe to unannounced event
- **WHEN** a plugin attempts to register a handler for an event the database module did not announce
- **THEN** registration is rejected (foundation registry contract)

### Requirement: Module enablement follows server role and config
The database module SHALL only load when the daemon's server record has `db_server = 1` and the module is enabled in `config.toml`.

#### Scenario: Non-DB server skips module
- **WHEN** the daemon starts on a server whose `server.db_server` flag is 0
- **THEN** no database table hooks are registered and database datalog rows are ignored by this module

#### Scenario: Module disabled in config
- **WHEN** the daemon starts with the database module disabled in `config.toml` even if `db_server = 1`
- **THEN** no database table hooks are registered

### Requirement: No service registration for MySQL
The database module SHALL NOT register a systemd service for restart/reload; privilege changes are applied in-process via SQL (`FLUSH PRIVILEGES`) by the plugin.

#### Scenario: Event processing does not queue a service action
- **WHEN** a `database_insert` event is handled successfully
- **THEN** no entry is added to the services delayed-restart registry for MySQL/MariaDB

