# powerdns-service

Daemon wiring for the PowerDNS backend: exclusive backend selection, `powerdns` service registration with AXFR allow-list rewrite, and `pdns_control` operational commands. Port of `dns_module.inc.php::restartPowerDNS` and the control helpers in `powerdns_plugin.inc.php`.

## ADDED Requirements

### Requirement: Exclusive per-server DNS backend selection
The daemon SHALL read `server.config` section `[dns]` key `dns_backend`. When the value is `powerdns`, it SHALL load the PowerDNS plugin and register the `powerdns` service, and SHALL NOT load the Bind plugin or register Bind zone application for that server. When the value is `bind`, empty, or missing, it SHALL load the Bind plugin as today and SHALL NOT load the PowerDNS plugin. Exactly one applying DNS plugin SHALL be active per server. The dns module table hooks and nine named events remain registered whenever `server.dns_server = 1` and the module is enabled, regardless of backend.

#### Scenario: PowerDNS backend loads only PowerDNS plugin
- **WHEN** the daemon starts on a DNS server with `dns_backend=powerdns`
- **THEN** PowerDNS event handlers are registered and Bind zone-file handlers are not

#### Scenario: Default backend remains Bind
- **WHEN** the daemon starts on a DNS server with no `dns_backend` key
- **THEN** the Bind plugin is loaded and the PowerDNS plugin is not

#### Scenario: Non-DNS server loads neither
- **WHEN** `server.dns_server = 0`
- **THEN** no dns table hooks are registered and neither DNS plugin loads

### Requirement: powerdns service registration and unit resolution
The dns stack SHALL register a `powerdns` service in the delayed services registry supporting at least `restart`. The systemd unit name SHALL resolve at runtime: `powerdns` if such a unit exists, otherwise `pdns` (port of `restartPowerDNS` daemon detection). Restart requests in the same datalog run SHALL be deduplicated by the foundation services registry.

#### Scenario: Debian-style pdns unit
- **WHEN** a delayed restart of `powerdns` is flushed on a host with unit `pdns` and without `powerdns`
- **THEN** `systemctl restart pdns` (or equivalent) is executed

#### Scenario: Duplicate restarts collapse
- **WHEN** two events request `restart` of `powerdns` in one datalog run
- **THEN** exactly one restart runs at end of run

### Requirement: AXFR allow-list rewritten on powerdns restart
On delayed `restart` of the `powerdns` service the executor SHALL: collect distinct non-empty `xfer` values from all active `dns_soa` and `dns_slave` rows for this server; always include `127.0.0.1`; build a single line `allow-axfr-ips=<comma-separated unique IPs>`; write it atomically to `/etc/powerdns/pdns.d/pdns.ispconfig-axfr` (or getconf override `powerdns_axfr_conf`); then restart the resolved unit. This matches ISPConfig's global AXFR ACL limitation (any listed IP may transfer any master zone).

#### Scenario: Xfer IPs merged from zones and slaves
- **WHEN** one active zone has `xfer=1.2.3.4` and one active slave has `xfer=5.6.7.8,1.2.3.4`
- **THEN** the AXFR file contains `allow-axfr-ips=127.0.0.1,1.2.3.4,5.6.7.8` (order may normalize but the set is complete and unique)

#### Scenario: Empty xfer still allows localhost
- **WHEN** no active zone or slave has a non-empty `xfer`
- **THEN** the file contains `allow-axfr-ips=127.0.0.1`

### Requirement: pdns_control rediscover, notify, and retrieve
After a successful active master SOA insert the plugin SHALL call `pdns_control rediscover` when the binary is found. After successful active master SOA insert/update it SHALL call `pdns_control notify <origin>` (origin without trailing dot). After successful active slave insert/update it SHALL call `pdns_control retrieve <origin>`. When `pdns_control` is missing, these steps SHALL no-op with a log and MUST NOT roll back PowerDNS SQL writes.

#### Scenario: New master zone rediscovers and notifies
- **WHEN** `dns_soa_insert` completes for an active zone `example.com.`
- **THEN** `pdns_control rediscover` and `pdns_control notify example.com` are attempted

#### Scenario: Missing pdns_control does not fail SQL sync
- **WHEN** `pdns_control` is not on PATH after a successful domain INSERT
- **THEN** the PowerDNS domain row remains committed and a warning is logged

### Requirement: SOA/slave mutations queue powerdns restart when xfer may change
After handling master SOA insert/update/delete and slave insert/update/delete that can affect domain presence or `xfer`, the plugin SHALL request a delayed `restart` of the `powerdns` service so the AXFR allow-list is rebuilt. Pure RR insert/update/delete paths SHALL NOT require a full pdns restart (SQL + rectify/notify suffice).

#### Scenario: Zone xfer change schedules restart
- **WHEN** a SOA update changes `xfer` on an active zone under PowerDNS backend
- **THEN** a delayed `restart` is queued for service `powerdns`

#### Scenario: Record-only change does not restart pdns
- **WHEN** only `dns_rr_update` fires for an active record
- **THEN** no `powerdns` service restart is queued

### Requirement: PowerDNS getconf keys
The `[dns]` server config section SHALL document and accept at least: `dns_backend` (`bind`|`powerdns`), `powerdns_axfr_conf` (default `/etc/powerdns/pdns.d/pdns.ispconfig-axfr`), and optional DSN-related keys consumed by the daemon for the PowerDNS database connection when not using the default same-host `powerdns` database. Defaults MUST leave Bind behavior unchanged when `dns_backend` is unset.

#### Scenario: Unset keys keep Bind defaults
- **WHEN** a migrated `server.config` has only Bind keys under `[dns]`
- **THEN** the daemon still selects the Bind plugin and existing Bind paths work
