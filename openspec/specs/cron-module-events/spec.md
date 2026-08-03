# cron-module-events

## ADDED Requirements

### Requirement: Cron table hook raises named events
The daemon cron module SHALL register a table hook for the `cron` table and announce/raise the events `cron_insert`, `cron_update` and `cron_delete`, mapping datalog actions `i`/`u`/`d` respectively (port of `cron_module.inc.php::process`).

#### Scenario: Cron insert datalog row dispatches cron_insert
- **WHEN** the daemon processes a `sys_datalog` row with `dbtable=cron` and `action=i`
- **THEN** the `cron_insert` event is raised with the `{old,new}` payload

#### Scenario: Cron update datalog row dispatches cron_update
- **WHEN** the daemon processes a `sys_datalog` row with `dbtable=cron` and `action=u`
- **THEN** the `cron_update` event is raised with the `{old,new}` payload

#### Scenario: Cron delete datalog row dispatches cron_delete
- **WHEN** the daemon processes a `sys_datalog` row with `dbtable=cron` and `action=d`
- **THEN** the `cron_delete` event is raised with the `{old,new}` payload

#### Scenario: Unregistered plugin cannot subscribe to unannounced event
- **WHEN** a plugin attempts to register a handler for an event the cron module did not announce
- **THEN** registration is rejected (foundation registry contract)

### Requirement: Module enablement follows server role and config
The cron module SHALL only load when the daemon's server record has `web_server = 1` and the module is enabled in `config.toml` (port of `cron_plugin::onInstall` which requires the web service).

#### Scenario: Non-web server skips module
- **WHEN** the daemon starts on a server whose `server.web_server` flag is 0
- **THEN** no cron table hook is registered and cron datalog rows are ignored by this module

#### Scenario: Module disabled in config
- **WHEN** the daemon starts with the cron module disabled in `config.toml` even though `web_server = 1`
- **THEN** no cron table hooks or client-job runner are started
