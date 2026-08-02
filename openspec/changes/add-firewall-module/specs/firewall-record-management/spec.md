# firewall-record-management

## ADDED Requirements

### Requirement: Firewall GORM model matches the ISPConfig3 schema
The system SHALL provide a GORM model for table `firewall` with explicit column tags for every column in `ispconfig3.sql`: `firewall_id`, `sys_userid`, `sys_groupid`, `sys_perm_user`, `sys_perm_group`, `sys_perm_other`, `server_id`, `tcp_port`, `udp_port`, `active`. No columns SHALL be added, renamed or type-changed. The `iptables` table SHALL NOT receive a model in this change.

#### Scenario: Model round-trips against MariaDB
- **WHEN** a firewall row is inserted and reloaded through the GORM model
- **THEN** every column value matches what was stored, including `active` enum `'y'`/`'n'`

### Requirement: Field validation ports firewall.tform.php
Create/update SHALL enforce:

- `server_id`: required positive integer, UNIQUE among `firewall` rows (error key `firewall_error_unique`);
- `tcp_port` / `udp_port`: match regex `^$|\d{1,5}(?::\d{1,5})?(?:,\d{1,5}(?::\d{1,5})?)*$` (error keys `tcp_ports_error_regex` / `udp_ports_error_regex`); empty string allowed;
- `active`: `'y'` or `'n'` (checkbox), default `'y'`.

Create defaults SHALL match the tform: `tcp_port=21,22,25,53,80,110,143,443,465,587,993,995,3306,4190,8080,8081,40110:40210`, `udp_port=53`, `active=y`.

#### Scenario: Duplicate server_id rejected
- **WHEN** a second firewall record is created for a `server_id` that already has one
- **THEN** validation fails with `firewall_error_unique` and no row or datalog entry is written

#### Scenario: Invalid port characters rejected
- **WHEN** `tcp_port` is submitted as `80;443` or `http`
- **THEN** validation fails with `tcp_ports_error_regex`

#### Scenario: Valid range list accepted
- **WHEN** `tcp_port` is `22,80,443,40110:40210`
- **THEN** validation passes and the value is stored unchanged

### Requirement: server_id is immutable after create
Update SHALL reject a change of `server_id` (port of `firewall_edit.php::onBeforeUpdate`: "The Server can not be changed."), keeping the stored server and returning a validation/business error.

#### Scenario: Server reassignment blocked
- **WHEN** an update body sets `server_id` to a different server
- **THEN** the stored `server_id` is unchanged and the client receives an error naming the immutability rule

### Requirement: Admin-only access with security policy
All firewall read/write operations SHALL require an admin session (`typ=admin`) **and** pass the security policy flag `admin_allow_firewall_config` (default `superadmin` → only `sys_user.id = 1`). Client and reseller sessions SHALL receive 403 even if riud bits would otherwise allow access.

#### Scenario: Client is denied
- **WHEN** a client user calls any firewall endpoint
- **THEN** the API returns 403 and no datalog row is written

#### Scenario: Non-superadmin admin blocked by default policy
- **WHEN** an admin with `userid != 1` calls a firewall mutation while `admin_allow_firewall_config=superadmin`
- **THEN** the API returns 403

#### Scenario: Policy relaxed to yes allows any admin
- **WHEN** `admin_allow_firewall_config` is set to `yes` and any admin mutates a firewall record
- **THEN** the operation succeeds (subject to validation)

### Requirement: Ownership stamps and datalog on every mutation
Creates SHALL stamp `sys_userid` / `sys_groupid` / `sys_perm_*` from the foundation auth preset (tform defaults `riud`/`riud`/`''`). Every successful create/update/delete SHALL write a `sys_datalog` row with `dbtable=firewall`, `dbidx` identifying `firewall_id`, `action` `i`/`u`/`d`, `server_id` equal to the record's `server_id`, and JSON `{old,new}` payload. The interface process SHALL NOT execute UFW or any OS firewall command.

#### Scenario: Create journals insert for the target server
- **WHEN** an authorized admin creates a firewall record for `server_id=1`
- **THEN** a datalog row with `action=i`, `dbtable=firewall` and `server_id=1` is present and no UFW process was spawned by the API process

#### Scenario: Delete journals old state
- **WHEN** an authorized admin deletes a firewall record
- **THEN** a datalog row with `action=d` contains the previous field values in `old` and the daemon is the only component that may run `ufw`
