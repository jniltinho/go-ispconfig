# server-deploy

## ADDED Requirements

### Requirement: Installer join mode
The installer SHALL support joining a node to an existing installation via a `--join-master` mode with master host, port, root user, root password and database as answers, port of `install/install.php:279-311`. In join mode the installer SHALL NOT seed a local server row through the standard path (`internal/database/seed.go:62-78`); it SHALL instead register against the master as described below. The panel/web-interface steps SHALL be skipped by default in join mode, matching upstream's default of `n` for "Install ISPConfig Web Interface" when `master_slave_setup == 'y'` (`install/install.php:584`). Standard single-server installation SHALL be unchanged.

#### Scenario: Join mode registers instead of seeding
- **WHEN** the installer runs with `--join-master` against a reachable master
- **THEN** no server row is seeded by the local schema step and the node is registered on the master instead

#### Scenario: Unreachable master aborts before any local change
- **WHEN** the supplied master credentials fail to connect
- **THEN** the installer aborts with a connection error and leaves the local system untouched

#### Scenario: Standard install unaffected
- **WHEN** the installer runs without `--join-master`
- **THEN** it seeds one local server row and writes a `config.toml` with no master DSN, exactly as before

### Requirement: Dual server-row registration
On join, the installer SHALL insert the `server` row into the **master** database, take the database-assigned `server_id`, and insert an identical row with that same `server_id` into the **local** database (port of `install/lib/installer_base.lib.php:556-565`). Role flags SHALL be derived from the same installer answers used by the standard install, not hardcoded. Detected host IPs SHALL be inserted as `server_ip` rows into both databases when the master and local hosts differ (port of `install/lib/installer_base.lib.php:604-666`).

#### Scenario: Same server_id on both sides
- **WHEN** the master assigns `server_id = 4` to the joining node
- **THEN** the local database contains a `server` row with `server_id = 4` carrying the same name, role flags and config

#### Scenario: Host IPs registered on both sides
- **WHEN** the joining host has two IPv4 addresses
- **THEN** two `server_ip` rows exist on the master and two matching rows exist locally, all with `server_id = 4`

### Requirement: Per-node least-privilege master database account
On join, the installer SHALL create a MySQL account named `goispsrv<server_id>` on the master, granted from the node's hostname and from each detected host IP, with exactly these privileges on the master database (port of `install/lib/installer_base.lib.php:570` and the grant block at `:680-905`): `server` → `SELECT` plus `UPDATE(updated)`; `sys_datalog` → `SELECT` plus `UPDATE(status, error)`; `sys_log` → `SELECT, INSERT`; `sys_remoteaction` → `SELECT` plus `UPDATE(action_state, response)`; `sys_group` and `web_database` → `SELECT`; `web_domain` → `SELECT` plus `UPDATE(ssl, ssl_letsencrypt, ssl_request, ssl_cert, ssl_action, ssl_key)`; `dns_soa` → `SELECT` plus `UPDATE(dnssec_initialized, dnssec_info, dnssec_last_signed, rendered_zone)`; `monitor_data` → `SELECT, INSERT, UPDATE, DELETE`; `mail_traffic`, `web_traffic`, `ftp_traffic` → `SELECT, INSERT, UPDATE`; `mail_user` → `SELECT` plus `UPDATE(last_access)`; `web_backup` and `mail_backup` → `SELECT, INSERT, DELETE`. The master root credentials SHALL be used only during the join and SHALL NOT be persisted to disk.

#### Scenario: Account created with scoped grants
- **WHEN** node 4 joins successfully
- **THEN** a `goispsrv4` account exists on the master with the privileges above and no others

#### Scenario: Slave cannot author datalog rows
- **WHEN** the node attempts `INSERT INTO sys_datalog` using its own credentials
- **THEN** the statement is denied by the database

#### Scenario: Master root password is not stored
- **WHEN** the join finishes
- **THEN** the master root password appears in no file written by the installer, including `config.toml` and any credentials file

### Requirement: Node configuration carries server id and master DSN
`config.toml` SHALL gain `[server] id` and `[database] master_dsn` (Go equivalents of `$conf['server_id']` and the `dbmaster_*` keys in `install/tpl/config.inc.php.master:87-126`). `[database] dsn` SHALL always address the node's **local** database. On a master or single-server install, `master_dsn` SHALL be empty or equal to `dsn`. On a joined node, `master_dsn` SHALL address the master database using the `goispsrv<N>` account. The installer SHALL write `[server] id` on every install from now on.

#### Scenario: Slave config written on join
- **WHEN** node 4 completes a join
- **THEN** its `config.toml` contains `[server] id = 4`, a local `dsn`, and a `master_dsn` using `goispsrv4`

#### Scenario: Single-server config omits master DSN
- **WHEN** a standard install completes
- **THEN** `master_dsn` is empty and the node operates against one database

### Requirement: Master and local database handle resolution
The daemon SHALL resolve two database handles following `server/lib/app.inc.php:96-107`: when `master_dsn` is empty or equal to `dsn`, the master handle SHALL be the same handle as the local one; otherwise a second connection SHALL be opened. The predicate "this node is the master" SHALL be defined as the two handles being identical, matching `running_on_masterserver()` / `running_on_slaveserver()` (`server/lib/app.inc.php:386,396`). All non-datalog module queries SHALL continue to use the local handle.

#### Scenario: Single database yields master mode
- **WHEN** `master_dsn` is empty
- **THEN** the node reports itself as master and performs no replication step

#### Scenario: Distinct master DSN yields slave mode
- **WHEN** `master_dsn` points at a different host or database than `dsn`
- **THEN** a second connection is opened and the node reports itself as a slave

#### Scenario: Unreachable master does not stop local operation
- **WHEN** the master connection fails at startup
- **THEN** the daemon logs the failure, processes no datalog rows, and still runs local scheduler and monitoring jobs (parity with `server/server.php:162-200`)

### Requirement: Server identity is configured, not guessed
When `[server] id` is non-zero, the daemon SHALL use it as its `server_id` and SHALL fail to start if no matching `server` row exists or the row is not `active`. When `[server] id` is `0`, the current inference SHALL be preserved — hostname match against `active = 1 AND mirror_server_id = 0`, otherwise the single active row (`cmd/serve.go:160-178`). The refusal to start when more than one active server row exists (`internal/engine/daemon.go:90-91`) SHALL be removed.

#### Scenario: Configured id is authoritative
- **WHEN** `[server] id = 4` and a matching active row exists
- **THEN** the daemon adopts `server_id = 4` regardless of the host's name

#### Scenario: Configured id with no row is fatal
- **WHEN** `[server] id = 9` and no `server` row with id 9 exists
- **THEN** the daemon exits with an error and processes nothing

#### Scenario: Multiple active servers no longer blocks startup
- **WHEN** four active `server` rows exist and `[server] id` identifies one of them
- **THEN** the daemon starts normally

### Requirement: Datalog is pulled from the master and replicated locally
On a slave, the daemon SHALL select its datalog batch from the **master** handle using the existing filter `datalog_id > cursor AND (server_id = ? OR server_id = 0) ORDER BY datalog_id LIMIT 1000` (`internal/engine/daemon.go:225-226`), and for each row SHALL first apply the change to the **local** database — upsert for `insert`/`update`, delete by the payload's index for `delete` — and only then dispatch to module handlers (port of `server/lib/classes/modules.inc.php:150-193,203`). A replication failure SHALL abort the remaining batch without advancing the cursor, so ordering is never violated (port of `server/lib/classes/modules.inc.php:226`). A payload naming a `dbtable` with no registered model SHALL be skipped with a warning rather than aborting the batch. On a master, no replication step SHALL occur and behaviour SHALL be identical to today.

#### Scenario: Row is replicated before handlers run
- **WHEN** a slave consumes an `insert` datalog row for `web_domain`
- **THEN** the row exists in the slave's local `web_domain` table before the web module handler is invoked

#### Scenario: Replication failure halts the batch
- **WHEN** applying row `datalog_id = 51` to the local database fails
- **THEN** rows 52 and later are not processed, the cursor still points before 51, and the failure is recorded on `sys_datalog.status`/`error`

#### Scenario: Unknown table is skipped, not fatal
- **WHEN** a payload references a `dbtable` this node has no model for
- **THEN** a warning is logged, the row is skipped, and the batch continues

#### Scenario: Master path unchanged
- **WHEN** a single-server node consumes a datalog row
- **THEN** no replication is attempted and the handler is dispatched directly

### Requirement: Cursor is written to both databases
The `server.updated` column SHALL remain the datalog cursor (there is no `sys_datalog_status` table in ISPConfig3). After each successfully processed row a slave SHALL write the `datalog_id` to `server.updated` in both the local and the master database (port of `server/lib/classes/modules.inc.php:200,204`). At startup the effective cursor SHALL be the greater of the local and master values, so a run that could not reach the master is not replayed (port of `server/server.php:74-77`).

#### Scenario: Both cursors advance
- **WHEN** a slave processes `datalog_id = 77`
- **THEN** `server.updated = 77` for that server in both the local and the master database

#### Scenario: Higher local cursor wins at startup
- **WHEN** the local row reports `updated = 90` and the master row reports `updated = 85`
- **THEN** the daemon resumes from 90 and does not reprocess rows 86–90

### Requirement: Mirror servers apply the mirrored server's datalog
When the node's `mirror_server_id` is greater than zero, the datalog filter SHALL widen to `(server_id = ? OR server_id = ? OR server_id = 0)` including the mirrored id, and for every payload whose table is not `server` the `server_id` value in the old and new records SHALL be rewritten from the mirrored id to the local id, with the dispatched event flagged as mirrored (port of `server/lib/classes/modules.inc.php:104-110,136-143`). The startup refusal on `mirror_server_id != 0` (`internal/engine/daemon.go:92-94`) SHALL be removed.

#### Scenario: Mirror consumes the source server's rows
- **WHEN** node 5 has `mirror_server_id = 2` and a datalog row targets `server_id = 2`
- **THEN** node 5 processes the row and provisions the object locally

#### Scenario: server_id is rewritten on the mirror
- **WHEN** node 5 (mirror of 2) receives a `web_domain` payload with `server_id = 2`
- **THEN** the handler sees `server_id = 5` and the event is flagged mirrored

#### Scenario: server table payloads are not rewritten
- **WHEN** a payload for the `server` table is consumed on a mirror
- **THEN** its `server_id` values are left untouched

### Requirement: Once-per-cluster side effects are suppressed on mirrors
Handlers whose side effects must occur exactly once per cluster SHALL skip those effects when the event is flagged mirrored: ACME/Let's Encrypt certificate issuance (port of `server/plugins-available/apache2_plugin.inc.php:1306` and `server/plugins-available/nginx_plugin.inc.php:1375`), welcome mail on mailbox creation (port of `server/plugins-available/mail_plugin.inc.php:269`), and SSL create actions (port of `server/plugins-available/nginx_plugin.inc.php:119`). Vhosts bound to a specific IP SHALL fall back to a wildcard listener on a mirror (port of `server/plugins-available/nginx_plugin.inc.php:1047`).

#### Scenario: Mirror does not request a certificate
- **WHEN** a mirrored `web_domain` event with `ssl_letsencrypt = y` is processed on a mirror node
- **THEN** the vhost is written but no ACME issuance is attempted

#### Scenario: Mirror does not send welcome mail
- **WHEN** a mirrored mailbox insert is processed
- **THEN** the maildir is created and no welcome mail is sent

#### Scenario: IP-bound vhost becomes wildcard on the mirror
- **WHEN** a mirrored `web_domain` specifies a concrete `ip_address`
- **THEN** the mirror renders the vhost listening on `*`

### Requirement: Remote actions and node logs use the master database
Pending `sys_remoteaction` rows SHALL be read from the master handle filtered on the node's `server_id`, and their result state written back through the same handle (port of `server/lib/classes/modules.inc.php:250-272`). Daemon `sys_log` entries and `monitor_data` writes SHALL likewise target the master database so the panel shows every node's state in one place (port of `server/lib/app.inc.php:311-334`).

#### Scenario: Slave picks up a remote action
- **WHEN** a `sys_remoteaction` row targeting server 4 is pending on the master
- **THEN** node 4 executes it and writes the resulting `action_state` back to the master

#### Scenario: Slave errors are visible on the master
- **WHEN** a handler on node 4 logs an error
- **THEN** a `sys_log` row with `server_id = 4` exists in the master database

### Requirement: No push channel between nodes
The system SHALL NOT open any node-to-node or master-to-node network channel for provisioning. The only inter-node mechanism SHALL be the slave's pull of `sys_datalog` and `sys_remoteaction` from the master database. The local ready-notifier that wakes the daemon after an API write (`internal/queue/queue.go:98-104`) SHALL fire only for the local server id or for broadcast rows; work targeting a remote node SHALL be picked up on that node's next poll tick, and that latency SHALL be documented.

#### Scenario: Remote work waits for the poll
- **WHEN** the panel on the master creates a website on server 4
- **THEN** no request is made to server 4 and the row is provisioned on server 4's next datalog poll

#### Scenario: Local work is still notified immediately
- **WHEN** the panel creates a website on the master's own server id
- **THEN** the local daemon is woken without waiting for the tick

### Requirement: No failover
The system SHALL NOT implement leader election, master promotion, or automatic re-pointing of `master_dsn`. ISPConfig3 has no failover mechanism of any kind — its only master-outage behaviour is to fall back to a cached rescue configuration, process nothing, and run local core jobs (`server/server.php:120-152`). Recovery from a master outage SHALL be an operator action, and promoting a mirror SHALL be documented as a manual procedure.

#### Scenario: Master outage degrades, never promotes
- **WHEN** the master database is unreachable for an extended period
- **THEN** each node continues serving already-provisioned configuration, processes no datalog, and no node takes over the master role

#### Scenario: Mirror is never auto-promoted
- **WHEN** the server a mirror clones goes offline
- **THEN** the mirror keeps its `mirror_server_id` and no automatic reassignment of objects occurs
