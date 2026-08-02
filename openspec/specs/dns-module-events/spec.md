# dns-module-events Specification

## Purpose
TBD - created by archiving change add-dns-bind-module. Update Purpose after archive.
## Requirements
### Requirement: DNS table hooks raise named events
The daemon dns module SHALL register table hooks for `dns_soa`, `dns_slave` and `dns_rr` and announce/raise the events `dns_soa_insert`, `dns_soa_update`, `dns_soa_delete`, `dns_slave_insert`, `dns_slave_update`, `dns_slave_delete`, `dns_rr_insert`, `dns_rr_update`, `dns_rr_delete`, mapping datalog actions `i`/`u`/`d` respectively (port of `dns_module.inc.php::process`).

#### Scenario: SOA update datalog row dispatches dns_soa_update
- **WHEN** the daemon processes a `sys_datalog` row with `dbtable=dns_soa` and `action=u`
- **THEN** the `dns_soa_update` event is raised with the `{old,new}` payload

#### Scenario: RR insert datalog row dispatches dns_rr_insert
- **WHEN** the daemon processes a `sys_datalog` row with `dbtable=dns_rr` and `action=i`
- **THEN** the `dns_rr_insert` event is raised with the `{old,new}` payload

#### Scenario: Unregistered plugin cannot subscribe to unannounced event
- **WHEN** a plugin attempts to register a handler for an event the dns module did not announce
- **THEN** registration is rejected (foundation registry contract)

### Requirement: Bind service registration
The dns module SHALL register a `bind` service in the services registry supporting `restart` and `reload` actions, resolving the systemd unit name at runtime: `bind9` if such a unit exists, otherwise `named`.

#### Scenario: Reload on Debian-style unit
- **WHEN** a delayed `reload` of the `bind` service is flushed on a host with a `bind9` systemd unit
- **THEN** `systemctl reload bind9` (or equivalent) is executed and its exit status logged

#### Scenario: Restart wins over reload in the same run
- **WHEN** one event requests `reload` and a later event requests `restart` for `bind` in the same datalog run
- **THEN** exactly one `restart` is executed at the end of the run

### Requirement: Module enablement follows server role and config
The dns module SHALL only load when the daemon's server record has `dns_server = 1` and the module is enabled in `config.toml`.

#### Scenario: Non-DNS server skips module
- **WHEN** the daemon starts on a server whose `server.dns_server` flag is 0
- **THEN** no dns table hooks are registered and dns datalog rows are ignored by this module

