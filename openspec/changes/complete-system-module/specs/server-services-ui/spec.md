# server-services-ui

## ADDED Requirements

### Requirement: Server Services lists and edits the server rows
The panel SHALL provide an admin-only `System → Server Services` list and form over the `server` entity, exposing the server name, the role flags (`web_server`, `mail_server`, `dns_server`, `db_server`, `firewall_server`, `vserver_server`, `xmpp_server`, `proxy_server`), the mirror target and `active`. Port of `interface/web/admin/server_list.php` and `server_edit.php`.

#### Scenario: Roles are visible at a glance
- **WHEN** an admin opens Server Services
- **THEN** each server row shows its name, its enabled roles and whether it is active

#### Scenario: A role can be enabled
- **WHEN** an admin enables `dns_server` on a node that owns no DNS data
- **THEN** the flag is stored and journalled, and the node's daemon starts consuming DNS events

### Requirement: Disabling a role in use is refused
The system SHALL refuse to clear a role flag while rows belonging to that role exist on the server, naming the blocking table and row count.

#### Scenario: Mail role with mailboxes is refused
- **WHEN** an admin clears `mail_server` on a node that still owns `mail_domain` rows
- **THEN** the save is refused with 422 naming the blocking rows and the flag is unchanged

#### Scenario: Web role with sites is refused
- **WHEN** an admin clears `web_server` on a node that still owns `web_domain` rows
- **THEN** the save is refused

#### Scenario: Emptying the role first allows the change
- **WHEN** the blocking rows have been deleted or moved and the admin retries
- **THEN** the save succeeds

### Requirement: The panel cannot be left without a web server
The system SHALL refuse to clear `web_server` on, or deactivate, the last active server carrying that role.

#### Scenario: Last web server cannot be deactivated
- **WHEN** a single-server install deactivates its only server
- **THEN** the save is refused with 422

#### Scenario: One of several can be deactivated
- **WHEN** two active servers carry `web_server` and one is deactivated
- **THEN** the save succeeds

### Requirement: server_id is immutable
The system SHALL refuse an update that changes the primary key of an existing server row.

#### Scenario: Changing the id is refused
- **WHEN** an update body carries a different `server_id`
- **THEN** the save is refused with a field error

### Requirement: Gated by admin_allow_server_services
Access SHALL be gated by the `admin_allow_server_services` security policy, superadmin-only by default.

#### Scenario: Non-superadmin admin is refused
- **WHEN** an admin other than `userid 1` opens Server Services while the policy is `superadmin`
- **THEN** the request is refused with 403
