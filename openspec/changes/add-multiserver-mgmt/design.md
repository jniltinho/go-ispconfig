# Design: Multi-server management

## Context

ISPConfig3's cluster is four mechanisms and nothing else (full trace in
`.hermes/prints/multiserver-php-howto.md`):

1. **Registration is installer-only.** `install/install.php:279` asks whether the node joins an existing
   setup; `install/lib/installer_base.lib.php:556-572` inserts the `server` row on the master (getting the
   AUTO_INCREMENT id), inserts an identical row locally, names the node's MySQL user `ispcsrv<server_id>`
   and grants it. The panel can only *edit* servers (`interface/web/admin/templates/server_list.htm:39` has
   no add link).
2. **Two DB handles.** `server/lib/app.inc.php:96-107`: `db` = the node's local MariaDB (full schema copy),
   `dbmaster` = the panel's MariaDB reached as `ispcsrv<N>`. On the master they are the same object, which
   is literally the definition of `running_on_masterserver()` (`app.inc.php:386`).
3. **Pull, never push.** `server/lib/classes/modules.inc.php:103-110` selects
   `sys_datalog WHERE datalog_id > cursor AND (server_id = me OR server_id = 0)` from `dbmaster`,
   `REPLACE INTO`/`DELETE FROM` the row into the local DB (`:150-193`), *then* raises the table hook
   (`:203`). Cursor is `server.updated`, written to both DBs (`:200,204`); at boot the higher of the two
   wins (`server/server.php:74-77`). **There is no `sys_datalog_status` table.** `sys_remoteaction` is the
   same pull pattern (`:250-272`).
4. **Mirroring.** `mirror_server_id` widens the datalog filter and rewrites the payload's `server_id` to the
   local id (`modules.inc.php:104-143`), so plugins configure the object as if they owned it; plugins then
   suppress once-per-cluster effects — Let's Encrypt (`apache2_plugin.inc.php:1306`,
   `nginx_plugin.inc.php:1375`), welcome mail (`mail_plugin.inc.php:269`), and bind vhosts to `*` instead of
   an IP (`nginx_plugin.inc.php:1047`).

The Go side already has the *shape* of (3): `internal/engine/daemon.go:225` runs the identical query with a
`server.updated` cursor (`:208-211,272-279`), and every module already resolves config from the payload's
`server_id` via `getconf.GetServerConfig(db, serverID)` (`internal/getconf/getconf.go:271-299`). What is
absent is the second DB handle, the join flow, the `server` entity in the API, and mirroring —
`internal/engine/daemon.go:92-94` currently refuses to start on any mirror at all.

## Goals / Non-Goals

**Goals:**
- Behaviour-faithful port of the four mechanisms above, with `sys_datalog` remaining the only inter-node
  channel and pull remaining the only direction.
- A `server` / `server_config` / `server_ip_map` API + panel UI so an admin manages the fleet without SSH,
  including *pre-registering* a node before its installer runs (an improvement over PHP's edit-only UI).
- An installer join mode that produces a working slave in one command, with a least-privilege per-node
  master-DB account.
- Every `server_id` input in the API validated against a real, active, correctly-roled, non-mirror server.
- Single-server installs behave exactly as today: no master DSN configured ⇒ one handle, no replication
  step, no behavioural change, no new required config key.

**Non-Goals:**
- Failover, leader election, master promotion, HA of the master DB (see proposal Non-goals) — upstream has
  none and this change adds none.
- MySQL replication, SSH orchestration, push-based agents, cross-server object migration.
- OpenVZ/vserver, XMPP, proxy roles; `sys_dbsync`.
- Any schema change.

## Decisions

### D1 — `dbmaster` as an optional second handle, not a new abstraction
`[database] master_dsn` is added to `internal/config/config.go` (`DatabaseConfig`, currently `DSN` +
`ClientDBConf` at `:97-103`). Resolution mirrors `server/lib/app.inc.php:96-107`:

```
master_dsn == ""            → masterDB = localDB   (single server / master node)
master_dsn == dsn           → masterDB = localDB   (same DB, explicit)
otherwise                   → masterDB = second *gorm.DB
IsMaster() := masterDB == localDB
```

One extra field on `engine.Daemon` (which already holds `*gorm.DB` at `daemon.go:46`) and one extra
`gorm.Open` in `cmd/daemon.go:55`. No `DBProvider` interface, no repository layer — every existing call
site keeps using the local handle; only the datalog/remoteaction/cursor/log paths switch to `masterDB`.
*Alternative*: route everything through an interface with master/local implementations — rejected, one
predicate and two handles is the whole problem.

### D2 — `server_id` becomes configuration, with the current inference as fallback
`config.toml` gains `[server] id` (default `0`). When `0`, `cmd/serve.go:160-178` / `engine.GuardServer`
keep today's behaviour (hostname match, else the single active row) so no existing install breaks. When
non-zero it is authoritative and a mismatch against the DB is a startup error. This is the Go equivalent of
`$conf['server_id']` in `install/tpl/config.inc.php.master:126`; the installer writes it on every install
from now on (`internal/installer/configtoml.go:62-63` currently writes only the DSN).

### D3 — `GuardServer` relaxes from "refuse" to "verify"
`internal/engine/daemon.go:82-97` today errors on `>1` active server and on `mirror_server_id != 0`. It
becomes: resolve the local row by configured id (D2), require it exists and is `active`, load
`mirror_server_id` into the daemon, and log the resolved identity. Multiple active servers stop being an
error. The mirror check inverts from a refusal into the feature of D5.

### D4 — Datalog consumption: read master, replicate local, then dispatch
`internal/engine/daemon.go:218-283` gains, when `!IsMaster()`, the port of `modules.inc.php:150-193`:

| Step | Source | PHP |
|---|---|---|
| select batch | `masterDB` | `modules.inc.php:104-110` |
| apply `i`/`u` to local | `localDB` upsert on the row's PK | `:150-176` (`REPLACE INTO`) |
| apply `d` to local | `localDB` delete by `dbidx` | `:181-193` |
| dispatch handlers | in-process | `:203` |
| advance cursor | **both** handles | `:200,204` |

The upsert is a GORM `clause.OnConflict{UpdateAll: true}` against the JSON `new` map for the payload's
`dbtable` — the Go payload is JSON (`internal/model/datalog.go`), not PHP `serialize()`, so the column set
comes straight from the map. Only tables with a registered model are replicated; an unknown `dbtable`
logs a warning and is skipped rather than aborting. A replication *error*, like PHP, aborts the batch
(`return` at `modules.inc.php:226`) so ordering is never violated; the row is quarantined with the existing
`quarantineRow` helper (`internal/engine/daemon.go:286-298`).
*Alternative*: skip local replication and have slave modules read from the master. Rejected — it breaks the
offline/rescue property, doubles master DB load, and every existing module query
(`internal/cron/load.go:35`, `internal/dns/namedconf.go:169`, `internal/monitor/repo.go:*`) assumes a
complete local database.

### D5 — Mirroring is a payload rewrite plus a `Mirrored` flag
Port of `modules.inc.php:104-143`. When `mirror_server_id > 0`:
- the batch filter widens to `(server_id = ? OR server_id = ? OR server_id = 0)`;
- for every payload whose `dbtable != "server"`, `new.server_id` / `old.server_id` equal to
  `mirror_server_id` are rewritten to the local id and `Mirrored = true` is set on the dispatched event.

`Mirrored` is a field on the event struct the handlers already receive, not a new interface. Handlers that
must run once per cluster check it — the Go analogues of `apache2_plugin.inc.php:1306` (ACME issuance in
`internal/nginx/ssl.go:161`), `mail_plugin.inc.php:269` (welcome mail), `nginx_plugin.inc.php:1047`
(IP-bound vhost → `*`). Everything else ignores it.

### D6 — Per-node MySQL account: `goispsrv<N>` with upstream's grant matrix
Port of `installer_base.lib.php:570,680-905`, keeping the least-privilege intent verbatim. Created on the
**master** by the joining node, using master root credentials supplied to the installer once and never
stored. Grants (`<db>` = master database):

| Table | Grant |
|---|---|
| `server` | `SELECT`, `UPDATE(updated)` |
| `sys_datalog` | `SELECT`, `UPDATE(status, error)` |
| `sys_log` | `SELECT, INSERT` |
| `sys_remoteaction` | `SELECT`, `UPDATE(action_state, response)` |
| `sys_group`, `web_database` | `SELECT` |
| `web_domain` | `SELECT`, `UPDATE(ssl, ssl_letsencrypt, ssl_request, ssl_cert, ssl_action, ssl_key)` |
| `dns_soa` | `SELECT`, `UPDATE(dnssec_initialized, dnssec_info, dnssec_last_signed, rendered_zone)` |
| `monitor_data` | `SELECT, INSERT, UPDATE, DELETE` |
| `mail_traffic`, `web_traffic`, `ftp_traffic` | `SELECT, INSERT, UPDATE` |
| `mail_user` | `SELECT`, `UPDATE(last_access)` |
| `web_backup`, `mail_backup` | `SELECT, INSERT, DELETE` |

Grants are issued for the node's hostname *and* each detected host IP (`installer_base.lib.php:700-716`).
The consequence to preserve: **a slave can never insert a `sys_datalog` row** — all authoring stays on the
master, which is what makes the pull model safe.
*Alternative*: reuse the single all-privileges app user across nodes. Rejected: one leaked slave would own
the entire panel database.

### D7 — Join flow as installer steps, not a new command
`internal/installer/steps.go:5-22` gains a branch, not a parallel pipeline. `--join-master` (plus
`--master-dsn` / master host/port/root-user/root-password/database answers, matching
`install.php:285-297`) switches:

| Step | Standard | Join |
|---|---|---|
| `mariadb` | create local DB + app user, seed schema (`internal/database/seed.go:62-78`) | create local DB + app user, seed **schema only**, no server row |
| new `register-master` | — | insert `server` row on master → id; insert same row locally with that id; create `goispsrv<N>` + grants (D6) |
| `server-ips` | local insert (`internal/installer/serverip.go:21-67`) | insert on master *and* local, port of `installer_base.lib.php:604-666` |
| `config.toml` | `dsn` + `server.id` | `dsn` + `master_dsn` + `server.id` (D2) |
| panel/nginx/tls | as today | skipped by default — upstream defaults the interface to `n` on slaves (`install.php:584`) |

Role flags for the new row come from the same `--web/--dns/...` answers the standard install already uses
(`cmd/install.go:102-120`); `internal/database/seed.go`'s hardcoded `WebServer:1, DNSServer:1, DBServer:1`
becomes derived in both modes.

### D8 — `server` is an API entity, but a guarded one
New entity registered next to `server_ip` (`internal/api/serverip.go:82`) under `/api/server`, admin-only
(the existing `admin_allow_server_services` security policy, matching
`interface/web/admin/server_edit.php:47`). Two deliberate divergences from PHP:
- **Create is allowed.** PHP has no add form; we allow pre-registering a node (name + roles, `active=0`)
  so the installer can *claim* an existing row instead of inserting one. This is strictly additive and
  makes the join flow scriptable.
- **`server` rows are still not journaled.** `internal/model/datalog.go:3-6` deliberately excludes
  `Server` from `DBHistory()`; that stays, *except* for the `config` column, which must reach the node —
  see D9. Role-flag and `active` changes take effect on the node's next start (PHP behaves the same: the
  daemon reads its row at boot, `server/server.php:68`).

Delete refuses when the row is referenced by any object (`web_domain`, `mail_domain`, `dns_soa`,
`web_database`, `cron`, `firewall`, `shell_user`, `ftp_user`, `server_ip`, `server_php`) or is another
row's `mirror_server_id` — port of the `onAfterDelete` cascade intent in
`interface/web/admin/server_del.php`.

### D9 — `server.config` travels as a datalog row, like everything else
Port of `server_config_edit.php:136-181`: the API writes the INI blob with the normal datalog writer
targeting `dbtable = server`, `server_id = <target>`. That is the one `server`-table journal entry we
emit (D8), and it is what makes a remote node pick the config up on its next poll. The node keeps reading
it locally through `getconf.GetServerConfig(db, serverID)` (`internal/getconf/getconf.go:271-299`) — after
D4's replication step the local `server.config` is current, so **no getconf call site changes**.

The API renders/parses the INI with the existing `getconf.ParseINI` / INI writer, section by section, so
unknown keys survive round-trips (upstream merges into the parsed array at `server_config_edit.php:166`).

### D10 — `mirror_server_id` selection rules and UI filtering
Ported verbatim: the picker excludes the server itself and anything already a mirror
(`server_edit.php:60`), and `server_id = 1` can never become a mirror (`:78`). Every "choose a server"
dropdown filters `mirror_server_id = 0` (`custom_datasource.inc.php:53-176`,
`interface/web/dns/form/dns_soa.tform.php:97`, `list/server_php.list.php:66`) — in Go that is
`internal/api/meta.go:188-196` plus the per-module validators (D11). `internal/api/dns.go:189` already
does exactly this for DNS servers and is the pattern to copy.

### D11 — One shared target-server validator
Rather than repeating the check in nine modules, a single helper in `internal/api` validates a submitted
`server_id`: exists, `active = 1`, `mirror_server_id = 0`, and the required role flag is set. Each entity
declares which flag it needs (`web_domain` → `web_server`, `mail_domain` → `mail_server`, `dns_soa` →
`dns_server`, `web_database` → `db_server`, `firewall` → `firewall_server`, `cron`/`ftp_user`/`shell_user`
→ `web_server`). The literal `if serverID == 0 { serverID = 1 }` at `internal/api/mail.go:220` is deleted;
`server_id` becomes required where the object is server-bound. `web_database_user` stays `server_id = 0`
on purpose (`internal/api/sitesdb.go:316-321`) — broadcast, not unassigned.

### D12 — Logging and monitoring go to the master
Port of `server/lib/app.inc.php:311-334`: daemon `sys_log` writes target the master handle so the panel
shows every node's errors in one place. `monitor_data` writes (`internal/monitor/write.go:57-89`) likewise
go to the master — the grant matrix (D6) gives full CRUD on `monitor_data` precisely for this. The monitor
UI gains a server selector; reads are already `server_id`-scoped (`internal/monitor/repo.go:73-267`).

### D13 — Queue notifications stay local
`internal/queue/ReadyNotifier(defaultServerID)` (`internal/queue/queue.go:98-104`) wakes the local daemon
after an API write. It cannot wake a remote node and will not try — a remote node picks the row up on its
next poll tick, exactly as upstream's cron-driven `server.php` does. The notifier therefore fires only when
the write targets the local server id (or `0`); everything else relies on the poll interval. Documented as
the expected multi-server latency, not a bug.
*Alternative*: an HTTP wake endpoint on each node. Rejected — it introduces the push channel the whole
architecture avoids, plus a new auth surface, to save at most one tick.

### D14 — Package layout
No new top-level packages. Changes land in `internal/engine` (dual DB, replication, mirror),
`internal/config` (`server.id`, `database.master_dsn`), `internal/installer` (join steps, master
registration, `goispsrv<N>`), `internal/api` (`server`, `server_config`, `server_ip_map` entities, shared
validator), `internal/model` (add `ServerIPMap`), and `frontend/` (admin server module, per-server config
editor, server selectors).

## Risks / Trade-offs

- [Master DB exposed to the network] → per-node least-privilege account (D6), grants scoped to hostname/IP, TLS on the connection documented as required, and the "slave cannot write `sys_datalog`" invariant tested.
- [Local replication drift — a slave's copy diverges after a skipped/errored row] → batch aborts on replication error like PHP (D4); the cursor only advances on success; a `resync` path (upstream `server/cli/modules/resync.inc.php`) re-emits datalog rows per server and is the documented repair.
- [Unknown `dbtable` in a payload on a slave] → skipped with a warning instead of aborting, so a master running a newer version never wedges an older node; the row still advances the cursor.
- [Mirror double-provisioning: two nodes both issuing ACME certs] → the `Mirrored` flag (D5) with the same suppression points upstream uses; covered by tests asserting no ACME call on the mirror.
- [Poll latency on remote nodes] → accepted (D13); tick interval is already configurable in `DaemonConfig` (`internal/config/config.go:106-130`).
- [`GuardServer` was a real safety net] → replaced by explicit identity verification (D3), and a mismatch between configured `server.id` and the DB is a hard startup failure rather than a silent wrong-node run.
- [Config divergence between master and node] → `server.config` has exactly one writer (the master, via datalog D9); the node never writes it back.

## Migration Plan

- Code-only; no schema change. Existing single-server installs are untouched: `master_dsn` empty and
  `server.id = 0` reproduce today's behaviour bit for bit, including the hostname-based identity fallback.
- Existing installs may adopt `[server] id` at any time; the installer writes it going forward.
- Adding a node: run the installer on it with `--join-master` and the master's root credentials (D7). The
  master needs no downtime and no reconfiguration beyond allowing the remote connection.
- Rollback of a joined node: stop its daemon, set `active = 0` on its `server` row from the panel. Objects
  assigned to it stay in the master DB and re-provision when the node returns.
- Legacy ISPConfig3 clusters migrated with `add-legacy-migration` already carry correct `server`,
  `server_ip` and `sys_datalog.server_id` data; only `config.toml` per node has to be authored.

## Open Questions

- Should `goispsrv<N>` be named `ispcsrv<N>` for drop-in compatibility with a half-migrated cluster (a PHP
  master with a Go slave)? Leaning: keep a distinct prefix so the two never collide on the same master, and
  document the mixed-cluster case as unsupported.
- Does the local replication step need to cover `sys_group` / `client` rows (broadcast with `server_id = 0`,
  `cmd/daemon.go:87-88`)? Leaning: yes, they are already in the `server_id = 0` batch and modules read them
  locally — but the volume on a large panel deserves a look before shipping.
- Should `server` create in the API (D8) also be able to *pre-provision* the `goispsrv<N>` account so the
  installer needs only that account's password instead of master root? Leaning: yes as a follow-up; it
  removes master root from the slave install entirely.
