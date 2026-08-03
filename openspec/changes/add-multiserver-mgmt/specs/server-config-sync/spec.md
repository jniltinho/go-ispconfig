# server-config-sync

## ADDED Requirements

### Requirement: Per-server configuration API
The system SHALL expose an admin-only REST surface for reading and writing the `server.config` INI blob per server, section by section, port of `interface/web/admin/server_config_edit.php:96-181` and `interface/web/admin/form/server_config.tform.php`. Reads SHALL return the parsed section for a given `server_id`; writes SHALL merge the submitted section back into the full parsed configuration and persist the re-serialised INI, so keys the panel does not know about survive a round trip (parity with the merge at `interface/web/admin/server_config_edit.php:166`). Access SHALL require the administrator role and the `admin_allow_server_config` security policy (`interface/web/admin/server_config_edit.php:46`).

#### Scenario: Read a section of a remote server's config
- **WHEN** an administrator requests the `web` section of server 4
- **THEN** the parsed `[web]` keys from server 4's `config` column are returned

#### Scenario: Unknown keys survive a write
- **WHEN** an administrator saves the `web` section of a config that also contains a key the panel does not render
- **THEN** the stored INI still contains that key with its original value

#### Scenario: Empty section is rejected
- **WHEN** a submitted section parses to zero keys
- **THEN** the write is refused with an error and the stored config is unchanged

### Requirement: Configuration reaches the node as a datalog row
Writing `server.config` SHALL emit a `sys_datalog` row for `dbtable = server` targeting the edited `server_id`, port of `interface/web/admin/server_config_edit.php:176` (`datalogUpdate('server', array("config" => …), 'server_id', $server_id)`) and of the remote API equivalent `server_config_set` (`interface/lib/classes/remote.d/server.inc.php:155-166`). This SHALL be the only circumstance in which a `server` row change is journaled; all other `server` column changes remain unjournaled per the server-registry capability.

#### Scenario: Config edit is journaled
- **WHEN** an administrator saves the `web` section for server 4
- **THEN** a `sys_datalog` row exists with `dbtable = server`, `server_id = 4` and the new `config` value in its new record

#### Scenario: Role flag edit is not journaled
- **WHEN** an administrator changes `dns_server` on the same form submission path
- **THEN** no `sys_datalog` row is emitted for that change

#### Scenario: Remote node applies the config
- **WHEN** node 4 consumes that datalog row
- **THEN** its local `server` row's `config` column matches the master's and subsequent config reads on the node return the new values

### Requirement: Nodes read configuration locally by server id
Daemon-side configuration SHALL continue to be read through `GetServerConfig(db, serverID)` against the node's **local** database (`internal/getconf/getconf.go:271-299`, port of `server/lib/classes/getconf.inc.php:33-46`). No getconf call site SHALL be changed to reach across to the master: the replication step in the server-deploy capability keeps the local `server.config` current. Global configuration SHALL continue to come from `sys_ini` (`internal/getconf/getconf.go:303-313`).

#### Scenario: Handler resolves config for the payload's server
- **WHEN** a handler on node 4 processes a payload with `server_id = 4`
- **THEN** it loads the `web`/`mail`/`dns` section from node 4's local `server` row

#### Scenario: Config read does not touch the master
- **WHEN** any module resolves its configuration
- **THEN** the query is issued on the local database handle

### Requirement: Panel and API resolve configuration for the target server
Panel-side and API-side configuration reads SHALL use the `server_id` of the object being acted on rather than any implicit local server. This covers the existing call sites `internal/api/sites.go:511`, `internal/api/mailbox.go:194`, `internal/api/mail.go:223` and `internal/api/sitesdb.go:571`. Where a configuration value is needed and the object carries no server, the request SHALL fail validation rather than fall back to a fixed server id — the fallback at `internal/api/mail.go:220` SHALL be removed.

#### Scenario: DKIM strength comes from the target mail server
- **WHEN** a mail domain on server 3 is saved with DKIM enabled
- **THEN** the DKIM key strength is read from server 3's `[mail]` configuration, not server 1's

#### Scenario: Missing server fails rather than defaulting
- **WHEN** a request needing server configuration carries no resolvable `server_id`
- **THEN** a validation error is returned

### Requirement: Module enablement follows the node's role flags
Each node SHALL load only the daemon modules its own `server` row enables, as it already does at `cmd/daemon.go:83-167` — mail when `mail_server = 1`, DNS when `dns_server = 1`, database when `db_server = 1`, firewall when `firewall_server = 1`, cron/web/ftp/shell with the web role — combined with the module-disable flags in `config.toml`. Datalog rows addressed to a role the node does not have SHALL still advance the cursor without being dispatched.

#### Scenario: Mail-only node ignores DNS work
- **WHEN** node 4 has `mail_server = 1` and `dns_server = 0` and a broadcast (`server_id = 0`) datalog row for a DNS table arrives
- **THEN** no DNS handler runs and the cursor advances past the row

#### Scenario: Enabling a role loads its module
- **WHEN** `db_server` is set to 1 on node 4 and its daemon restarts
- **THEN** the database module is registered on node 4

### Requirement: Per-server monitoring and log visibility
Monitoring data and node logs SHALL remain keyed on `server_id` (`internal/monitor/write.go:57-89`, `internal/monitor/repo.go:73-267`) and the panel SHALL provide a server selector so an administrator can view any node's state, services, quota and logs. Collection SHALL run only on the node it describes.

#### Scenario: Admin views a remote node's state
- **WHEN** an administrator selects server 4 in the monitor UI
- **THEN** the `monitor_data` rows with `server_id = 4` are shown

#### Scenario: Collection stays local
- **WHEN** node 4 runs its monitor jobs
- **THEN** it writes rows only with `server_id = 4` and never probes another node

### Requirement: Per-server PHP versions
`server_php` rows SHALL remain scoped to a `server_id` and SHALL be listed and selected per server, port of `interface/web/admin/server_php_edit.php` and the `mirror_server_id = 0` server filter in `interface/web/admin/list/server_php.list.php:66`. A website SHALL only be able to select a PHP version registered on its own server.

#### Scenario: PHP picker is scoped to the website's server
- **WHEN** a website on server 4 opens its PHP version picker
- **THEN** only `server_php` rows with `server_id = 4` (or the global entries) are offered

#### Scenario: Cross-server PHP selection rejected
- **WHEN** a website on server 4 is saved referencing a `server_php` row belonging to server 2
- **THEN** a validation error is returned
