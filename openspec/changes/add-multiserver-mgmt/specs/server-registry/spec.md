# server-registry

## ADDED Requirements

### Requirement: Server CRUD API
The system SHALL expose an admin-only REST entity for the `server` table at `/api/server` supporting list, read, create, update and delete, with fields `server_name`, the role flags `mail_server`, `web_server`, `dns_server`, `file_server`, `db_server`, `firewall_server`, `proxy_server`, `mirror_server_id` and `active` (port of `interface/web/admin/server_edit.php` and `interface/web/admin/form/server.tform.php:40-140`). Access SHALL be gated on the administrator role and the `admin_allow_server_services` security policy, matching `interface/web/admin/server_edit.php:46-49`. `server_name` SHALL be validated as a hostname and SHALL be unique.

#### Scenario: Admin lists servers
- **WHEN** an authenticated administrator calls `GET /api/server`
- **THEN** every `server` row is returned with its role flags, `active`, `mirror_server_id` and `updated` cursor value

#### Scenario: Non-admin is refused
- **WHEN** a client user calls `GET /api/server` or `PUT /api/server/2`
- **THEN** the request is rejected with a permission error and no row is read or written

#### Scenario: Duplicate server name rejected
- **WHEN** an administrator creates a server whose `server_name` already exists
- **THEN** a validation error is returned and no row is inserted

### Requirement: Pre-registering a server
The API SHALL allow creating a `server` row before any software is installed on that host, so the installer can claim an existing row instead of inserting one. A pre-registered row SHALL default to `active = 0` and SHALL be assignable a `server_id` only by the database AUTO_INCREMENT, never by the caller (parity with `install/sql/ispconfig3.sql:1381`, where `server_id` is auto-assigned and `install/install.php:275` shows the manual prompt as dead code). This extends ISPConfig3, whose panel offers no add action (`interface/web/admin/templates/server_list.htm:39` renders edit links only).

#### Scenario: Pre-registered server is created inactive
- **WHEN** an administrator creates a server named `web02.example.com` with `web_server = 1` and no `active` value
- **THEN** the row is created with a database-assigned `server_id` and `active = 0`

#### Scenario: Caller-supplied server_id ignored
- **WHEN** a create request includes `server_id = 7`
- **THEN** the supplied value is discarded and the row receives the next AUTO_INCREMENT id

### Requirement: Server deletion is refused while referenced
Deleting a `server` row SHALL be refused when any object still references it — `web_domain`, `mail_domain`, `dns_soa`, `dns_slave`, `web_database`, `cron`, `firewall`, `shell_user`, `ftp_user`, `server_ip`, `server_php` — or when another server names it as its `mirror_server_id` (intent ported from `interface/web/admin/server_del.php`). The response SHALL name the blocking references.

#### Scenario: Server with websites cannot be deleted
- **WHEN** an administrator deletes a server that owns three `web_domain` rows
- **THEN** the request fails with an error naming `web_domain` and the row remains

#### Scenario: Server used as a mirror target cannot be deleted
- **WHEN** server 2 has `mirror_server_id = 3` and an administrator deletes server 3
- **THEN** the request fails and server 3 remains

#### Scenario: Unreferenced server is deleted
- **WHEN** an administrator deletes a server with no referencing rows and no mirrors
- **THEN** the `server` row and its `server_ip` rows are removed

### Requirement: Mirror selection rules
`mirror_server_id` SHALL identify another server whose configuration this server clones. The selectable set SHALL exclude the server itself and any server that is already a mirror (`SELECT server_id, server_name FROM server WHERE server_id != ? AND mirror_server_id != ?`, port of `interface/web/admin/server_edit.php:60`). A server SHALL NOT mirror itself, and `server_id = 1` SHALL never be a mirror; violating submissions SHALL be coerced to `mirror_server_id = 0` (port of `interface/web/admin/server_edit.php:78`).

#### Scenario: Self-mirror is coerced to none
- **WHEN** an administrator saves server 4 with `mirror_server_id = 4`
- **THEN** the stored value is `0`

#### Scenario: Server 1 can never be a mirror
- **WHEN** an administrator saves server 1 with `mirror_server_id = 2`
- **THEN** the stored value is `0`

#### Scenario: Mirror candidates exclude existing mirrors
- **WHEN** server 3 already has `mirror_server_id = 2` and an administrator opens the mirror picker for server 5
- **THEN** server 3 and server 5 are absent from the candidate list

### Requirement: Server pickers exclude mirrors and inactive servers
Every endpoint or UI control that lets a user choose a target server SHALL list only servers with `active = 1` and `mirror_server_id = 0`, and SHALL additionally filter on the role flag the object requires (port of the `mirror_server_id = 0` filters in `interface/lib/classes/custom_datasource.inc.php:53-176`, `interface/web/dns/form/dns_soa.tform.php:97` and `interface/web/admin/list/server_php.list.php:66`; the pattern already exists in Go at `internal/api/dns.go:189`).

#### Scenario: DNS picker omits mirrors
- **WHEN** a user opens the server picker on a DNS zone form and one `dns_server = 1` server is a mirror
- **THEN** the mirror is not offered

#### Scenario: Inactive server omitted
- **WHEN** a server has `active = 0`
- **THEN** it does not appear in any target-server picker

### Requirement: Shared target-server validation on server_id inputs
Every create or update that carries a `server_id` for a server-bound object SHALL validate through one shared helper that the referenced server exists, has `active = 1`, has `mirror_server_id = 0`, and has the role flag the object requires (`web_domain` → `web_server`, `mail_domain` → `mail_server`, `dns_soa`/`dns_slave` → `dns_server`, `web_database` → `db_server`, `firewall` → `firewall_server`, `cron`/`ftp_user`/`shell_user` → `web_server`). The hardcoded fallback `if serverID == 0 { serverID = 1 }` at `internal/api/mail.go:220` SHALL be removed and `server_id` SHALL be required for server-bound objects. Rows that are intentionally broadcast — notably `web_database_user`, which is created with `server_id = 0` because the user exists on every DB server (`internal/api/sitesdb.go:316-321`) — SHALL keep `server_id = 0` and SHALL NOT be validated against a server row.

#### Scenario: Object rejected for wrong role
- **WHEN** a website is created with a `server_id` whose row has `web_server = 0`
- **THEN** a validation error is returned and no row or datalog entry is written

#### Scenario: Mail domain no longer defaults to server 1
- **WHEN** a mail domain create request omits `server_id`
- **THEN** a validation error is returned rather than the row silently landing on server 1

#### Scenario: Database user stays broadcast
- **WHEN** a `web_database_user` is created
- **THEN** it is stored with `server_id = 0` and no target-server validation is performed

### Requirement: Server IP and IP map management
The system SHALL manage `server_ip` rows per server (the existing entity at `internal/api/serverip.go:16-98`) and SHALL add a `server_ip_map` entity mapping a mirror server's IP onto a source server's IP, port of `interface/web/admin/server_ip_map_edit.php:51-64` — the source list is drawn from IPv4 `server_ip` rows of servers with `web_server = 1`, `mirror_server_id = 0`, `virtualhost = 'y'`, and the destination server list is restricted to servers with `mirror_server_id > 0`. Changing the `server_id` of an existing `server_ip` row SHALL be refused (port of `interface/web/admin/server_ip_edit.php:56-66`).

#### Scenario: IP map target must be a mirror
- **WHEN** an administrator creates a `server_ip_map` whose destination server has `mirror_server_id = 0`
- **THEN** a validation error is returned

#### Scenario: Moving an IP between servers is refused
- **WHEN** an update changes `server_ip.server_id` from 1 to 2
- **THEN** the original `server_id` is preserved and the change is rejected

### Requirement: Server row changes are not journaled except config
`server` rows SHALL remain excluded from datalog journaling (`internal/model/datalog.go:3-6`), with the single exception of the `config` column described in the server-config-sync capability. Role-flag and `active` changes SHALL take effect on the target node at its next daemon start, matching ISPConfig3, where the daemon reads its own `server` row once at boot (`server/server.php:68`).

#### Scenario: Toggling a role flag writes no datalog row
- **WHEN** an administrator sets `dns_server = 1` on server 2
- **THEN** the `server` row is updated and no `sys_datalog` row is created

#### Scenario: Role change applies on daemon restart
- **WHEN** server 2's daemon restarts after `dns_server` was set to 1
- **THEN** the DNS module is loaded on that node
