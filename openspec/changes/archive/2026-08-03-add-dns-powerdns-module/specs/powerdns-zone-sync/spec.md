# powerdns-zone-sync

SQL zone/record/slave sync from panel tables `dns_soa`, `dns_rr`, `dns_slave` into the separate PowerDNS database schema (`domains`, `records` from embedded `powerdns.sql`). Port of `powerdns_plugin.inc.php` event handlers. The panel interface never touches the OS or the PowerDNS DB — only the daemon plugin does, after consuming `sys_datalog`.

## ADDED Requirements

### Requirement: Master zone insert into powerdns.domains and SOA record
On `dns_soa_insert` with `new.active = 'Y'` the PowerDNS plugin SHALL strip the trailing dot from `new.origin` and INSERT into `powerdns.domains` with `name = origin`, `type = 'MASTER'`, `notified_serial = dns_soa.serial` (re-read from panel DB by id), and `ispconfig_id = new.id`. It SHALL INSERT a `powerdns.records` row with `type = 'SOA'`, `name = origin`, `ttl = new.ttl`, `prio = 0`, `change_date = UNIX_TIMESTAMP()`, `ispconfig_id = new.id`, and `content` built as `"<ns> <hostmaster> <serial> <refresh> <retry> <expire> <minimum>"` where `ns` is absolute when it ends with `.` (dot stripped), else `ns + "." + origin` (empty ns falls back to origin), and `hostmaster` is `new.mbox` without a trailing dot. When `new.active != 'Y'` the handler SHALL be a no-op.

#### Scenario: Active zone creates MASTER domain and SOA
- **WHEN** `dns_soa_insert` fires for active zone `example.com.` with ns `ns1.example.com.`, mbox `hostmaster.example.com.`, serial `2026080201`, refresh/retry/expire/minimum/ttl set
- **THEN** `powerdns.domains` has one MASTER row with `name=example.com`, `ispconfig_id` equal to the zone id, and `powerdns.records` has a SOA row whose content starts with `ns1.example.com hostmaster.example.com 2026080201`

#### Scenario: Inactive SOA insert is ignored
- **WHEN** `dns_soa_insert` fires with `new.active = 'N'`
- **THEN** no rows are written to `powerdns.domains` or `powerdns.records`

### Requirement: Master zone update and deactivation
On `dns_soa_update`: if `new.active != 'Y'` and `old.active = 'Y'` the plugin SHALL run the SOA delete path; if both are inactive it SHALL no-op; if `old.active = 'Y'` and a PowerDNS domain with `ispconfig_id = new.id` exists it SHALL UPDATE the SOA `records` row (`type='SOA'`, matching `ispconfig_id`) with the new name/content/ttl and `change_date = UNIX_TIMESTAMP()`; otherwise it SHALL run the insert path and re-insert every active `dns_rr` for that zone (`zone = new.id AND active = 'Y'`). After a successful active update it SHALL invoke rectify and notify (see powerdns-service).

#### Scenario: Deactivating a zone removes PowerDNS data
- **WHEN** `dns_soa_update` changes `active` from Y to N on a zone that exists in PowerDNS
- **THEN** the matching MASTER domain and all its `powerdns.records` rows are deleted

#### Scenario: Activating a previously inactive zone imports records
- **WHEN** `dns_soa_update` changes `active` from N to Y and no PowerDNS domain exists yet
- **THEN** the MASTER domain and SOA record are created and every active panel `dns_rr` for that zone is inserted into `powerdns.records`

### Requirement: Master zone delete
On `dns_soa_delete` the plugin SHALL locate `powerdns.domains` where `ispconfig_id = old.id AND type = 'MASTER'`, DELETE all `powerdns.records` with that `domain_id`, then DELETE the domain row. Missing domain SHALL be a no-op (no error).

#### Scenario: Zone delete purges PowerDNS domain
- **WHEN** `dns_soa_delete` fires for a zone that has a MASTER domain and several records
- **THEN** both the domain and all its records are gone from the PowerDNS database

### Requirement: Resource record insert mapping
On `dns_rr_insert` with `new.active = 'Y'` the plugin SHALL skip if a `powerdns.records` row with `ispconfig_id = new.id` already exists, and SHALL skip if no MASTER `powerdns.domains` row exists for `ispconfig_id = new.zone` (SOA not active yet). Otherwise it SHALL INSERT a record with `domain_id` from that domain, `type = new.type`, `ttl = new.ttl`, `prio = new.aux`, `change_date` set, `ispconfig_id = new.id`, and name/content derived as follows (port of PHP): `name` absolute if ends with `.` (strip), empty → zone origin, else `name + "." + origin`; for types CNAME, MX, NS, ALIAS, PTR, SRV, `content` uses the same absolute/relative rule on `new.data`; for HINFO, apply the PHP quote-to-underscore transform; for all other types, `content = new.data` as-is.

#### Scenario: Relative A record becomes FQDN name
- **WHEN** `dns_rr_insert` fires for type A, name `www`, data `1.2.3.4` on zone origin `example.com.`
- **THEN** PowerDNS has a record `name=www.example.com`, `type=A`, `content=1.2.3.4`, `ispconfig_id` equal to the panel RR id

#### Scenario: Apex MX with relative target
- **WHEN** `dns_rr_insert` fires for type MX, name empty, data `mail`, aux `10` on origin `example.com.`
- **THEN** PowerDNS has `name=example.com`, `type=MX`, `content=mail.example.com`, `prio=10`

#### Scenario: RR insert before active SOA is skipped
- **WHEN** `dns_rr_insert` fires and no MASTER domain exists for the parent zone
- **THEN** no PowerDNS record is written and no error is raised

### Requirement: Resource record update and delete
On `dns_rr_update`: if new inactive and old active → delete path; if old active and PowerDNS row exists → UPDATE name/type/content/ttl/prio/change_date WHERE `ispconfig_id = new.id AND type != 'SOA'`; else insert. On `dns_rr_delete` the plugin SHALL DELETE FROM `powerdns.records` WHERE `ispconfig_id = old.id AND type != 'SOA'`.

#### Scenario: Deactivating a record removes it from PowerDNS
- **WHEN** `dns_rr_update` sets `active` from Y to N
- **THEN** the PowerDNS row with that `ispconfig_id` is deleted and the SOA row is untouched

#### Scenario: RR delete never removes SOA
- **WHEN** `dns_rr_delete` fires with an id that somehow matched only a SOA row
- **THEN** no SOA row is deleted (`type != 'SOA'` guard)

### Requirement: Secondary zone SLAVE domains
On `dns_slave_insert` with `new.active = 'Y'` the plugin SHALL INSERT `powerdns.domains` with `name = origin` (trailing dot stripped), `type = 'SLAVE'`, `master = new.ns`, `ispconfig_id = new.id`, then request a fetch from master (`pdns_control retrieve`). On `dns_slave_update`, deactivation runs delete; active update UPDATEs name/master, DELETEs `powerdns.records` WHERE `domain_id = ? AND ispconfig_id = 0` (AXFR cache), and retrieves again; transition inactive→active runs insert. On `dns_slave_delete`, DELETE records for the domain then DELETE the SLAVE domain by `ispconfig_id`.

#### Scenario: Secondary zone creates SLAVE domain
- **WHEN** `dns_slave_insert` fires for active origin `example.com.` with `ns = '1.2.3.4'`
- **THEN** `powerdns.domains` has `type=SLAVE`, `master=1.2.3.4`, `name=example.com`, and a retrieve is issued for `example.com`

#### Scenario: Slave update purges AXFR cache records
- **WHEN** `dns_slave_update` fires for an active secondary zone that has PowerDNS records with `ispconfig_id = 0`
- **THEN** those records are deleted before retrieve is issued

### Requirement: Event-to-SQL fidelity tests
Zone/record/slave mapping SHALL be covered by table-driven tests (and golden SQL fixtures where useful) derived from the PHP plugin behavior for absolute vs relative names, HINFO transform, inactive transitions, missing parent domain, and slave purge of `ispconfig_id = 0` rows.

#### Scenario: Relative name fixture matches PHP
- **WHEN** the Go mapper processes the same RR fixture the PHP plugin would
- **THEN** the resulting PowerDNS `name` and `content` equal the PHP-expected values
