# Proposal: Multi-server management (server registry, slave join, per-server config)

## Why

ISPConfig3 panels are multi-server from day 1: one master runs the interface and owns the database, and any
number of slave nodes (web, mail, dns, db, firewall, mirror) pull their work from it. The whole architecture
exists for it — `sys_datalog.server_id` is the routing key (`interface/lib/classes/db_mysql.inc.php:744-746`),
`server/lib/app.inc.php:96-107` opens a second `dbmaster` connection when the node is not the master, and
`install/install.php:279` offers *"Shall this server join an existing ISPConfig multiserver setup"* as the
one and only way a node enters the cluster.

go-ispconfig currently assumes **exactly one server**. `internal/engine/daemon.go:82-97` (`GuardServer`)
actively refuses to start when more than one active `server` row exists or when `mirror_server_id != 0`.
There is a single `*gorm.DB` handle opened from `cfg.Database.DSN` (`cmd/daemon.go:55`) — no master/local
split. The daemon infers its own identity by hostname match or "the one active row"
(`cmd/serve.go:160-178`), because `internal/config/config.go` has no `server_id` key at all. There is no
`server` CRUD API (only `server_ip`, `internal/api/serverip.go:82`, plus a read-only picker at
`internal/api/meta.go:188-196`), and `internal/installer/steps.go:5-22` has no join/slave path — it always
seeds a single local server row (`internal/database/seed.go:62-78`).

The good news is that the load-bearing parts already generalise: the engine's datalog query is already
`WHERE datalog_id > ? AND (server_id = ? OR server_id = 0)` against a per-server cursor
(`internal/engine/daemon.go:225`, cursor read at `:208-211`, advanced at `:272-279`), `getconf` is already
`GetServerConfig(db, serverID)` (`internal/getconf/getconf.go:271-299`), and every daemon module is already
gated on the `server` row's role flags (`cmd/daemon.go:83-167`). What is missing is the cluster: registering
a second node, giving it credentials, letting it reach the master DB, and letting an admin manage all of it
from the panel.

Reference PHP sources being ported (read-only under `base/ispconfig3_install/`):
- `interface/web/admin/server_edit.php`, `server_list.php`, `form/server.tform.php`, `list/server.list.php` — server row editing, mirror selection, role flags
- `interface/web/admin/server_ip_edit.php`, `server_ip_map_edit.php`, `server_php_edit.php` — per-server IPs, mirror IP mapping, per-server PHP versions
- `interface/web/admin/server_config_edit.php` + `form/server_config.tform.php` — the `server.config` INI editor pushed via `datalogUpdate('server', …)`
- `install/install.php:279-311` + `install/lib/installer_base.lib.php:556-905` — join-an-existing-setup flow, dual server-row insert, `ispcsrv<N>` user and its grants
- `install/tpl/config.inc.php.master:68-126` — `db_*` vs `dbmaster_*` vs `server_id`
- `server/server.php:67-190`, `server/lib/classes/modules.inc.php:100-272` — dual-DB bootstrap, datalog pull + local replication, `server.updated` cursor, mirror rewriting
- `server/lib/app.inc.php:96-107,386,396` — `dbmaster` construction, `running_on_masterserver()` / `running_on_slaveserver()`
- `interface/lib/classes/remote.d/server.inc.php:118-231` — `server_get`, `server_config_set`, `server_get_functions`
- Field research: `.hermes/prints/multiserver-php-howto.md`

## What Changes

> **Re-scoped 2026-08-04.** This proposal was written when the port was
> single-server by construction. Since then a good half of it shipped, so the
> lists below were re-checked line by line against the tree at `v0.4.0` — what
> was already delivered is recorded as such rather than left to mislead whoever
> picks this up.

**Already delivered (was listed as missing here, now in tree):**

- **Explicit `server_id` in config** — `config.ServerID` exists and
  `engine.ResolveServer` resolves the node's row from it, then by hostname,
  then by "the single active row". There is no hostname-guessing left, and no
  `GuardServer`: several active rows are supported, and a node refuses to start
  only when it cannot tell which row is its own.
- **`server` CRUD** — `serverEntity()` (`internal/api/servers.go`) with the role
  flags, `active` and `mirror_server_id`, including the validation that refuses
  an illegal mirror target.
- **Per-server target validation** — `requireTargetServer(role)` is applied by
  the entities that carry a `server_id`, so a create against an absent,
  inactive or wrong-role server is refused. The literal `server_id = 1`
  fallback is gone.
- **`server_config` API + UI** — `GET|PUT /api/servers/:id/config[/:section]`
  and the System → Server Config editor, delivering changes through a
  `sys_datalog` row for `dbtable=server`. This is the whole
  `server-config-sync` capability below; it is **done**.
- Everything the original "already exists" list named — models, `server_ip`
  CRUD, per-server `getconf`, the per-server datalog cursor and filter,
  role-flag module gating.

**Still missing — the actual remaining scope of this change:**

- **`dbmaster` — a second DB handle**: `[database] master_dsn` in `config.toml`
  plus a resolved "is this node the master" predicate, port of
  `app.inc.php:96-107,386,396`. Today there is exactly one handle, so every
  node must reach the master's database directly (see `docs/multi-server.md`).
- **Datalog consumption against the master + local replication**: read
  `sys_datalog` from the master handle, `REPLACE INTO`/`DELETE FROM` the local
  mirror of the row, then dispatch — port of `modules.inc.php:150-193`.
- **Cursor write-back to both DBs**: `server.updated` on local *and* master
  (`modules.inc.php:200,204`).
- **Slave install / join**: an installer mode that registers this node against a
  remote master (server row + `server_ip` rows on the master, mirrored
  locally), provisions the per-server MySQL account, and writes a slave
  `config.toml`. Port of `install.php:279-311` +
  `installer_base.lib.php:556-572`. Today the join is the manual four-step
  procedure documented in `docs/multi-server.md`.
- **`goispsrv<N>` MySQL account and grants**: the Go equivalent of `ispcsrv<N>`
  (`installer_base.lib.php:570,680-905`) — a least-privilege per-node master-DB
  user. Today every node uses the same all-privileges app user.
- **Mirror support**: widen the datalog filter, rewrite `server_id` on mirrored
  payloads and expose a `Mirrored` flag so plugins can suppress
  once-per-cluster side effects (`modules.inc.php:104-143`).
- **`server_ip_map` model + CRUD**: the model exists, nothing reads or writes
  it.
- **Server Services UI**: the role flags are editable through `/api/server`
  only. Covered by `complete-system-module`; noted here because a cluster is
  unusable without it.

## Impact

- **Depends on** `port-ispconfig3-to-go` (datalog engine, getconf, riud permissions, REST core, panel skeleton) and `add-installer-cli`.
- **Go packages that must become server-aware** (today they resolve config from a payload `server_id` but assume the row is local and always exists):
  - `internal/engine` — dual DB, replication, mirror rewrite, cursor write-back (`daemon.go:82-97,208-279`)
  - `internal/config` — `server_id`, `master_dsn` (`config.go:97-130`), `config.toml.example`
  - `internal/installer` — join mode, master registration, per-node DB account (`steps.go:5-22`, `mariadb.go:53-64`, `serverip.go:21-67`), `internal/database/seed.go:62-78`
  - `internal/api` — new `server` / `server_config` / `server_ip_map` entities, target-server validation on every `server_id` field; remove the `server_id = 1` fallback in `mail.go:220`; extend the picker in `meta.go:188-196`
  - `internal/web` + `internal/nginx` — vhost/pool/ssl paths already read config per payload `server_id` (`nginx/plugin.go:96-97`, `pool.go:201-306`, `ssl.go:161`); need mirror-aware IP fallback (`*` instead of a bound IP) and once-per-cluster SSL/ACME suppression
  - `internal/mail` — stored `serverID` (`plugin.go:22,40,85`); welcome-mail-style once-per-cluster suppression
  - `internal/dns` (bind) and `internal/powerdns` — `namedconf.go:169,177`, `dnssec.go:331`, `powerdns/plugin.go:79-84`; DNS slave/mirror interplay with `mirror_server_id`
  - `internal/clientdb` (database module) — `plugin.go:73,87`, `handlers.go:290`; DB users are intentionally `server_id = 0` (`internal/api/sitesdb.go:316-321`) and must stay broadcast
  - `internal/cron` — `load.go:35`
  - `internal/firewall` — `plugin.go:17,42`
  - `internal/monitor` — every write and read is already server-scoped (`write.go:57-89`, `repo.go:73-267`, `quota.go:54-189`); the panel must be able to select which server it is looking at
  - `internal/ftp`, `internal/shell`, `internal/jailkit` — `ftp/plugin.go:129,206`, `shell/plugin.go:137,428`, `jailkit/plugin.go:173,319-361`
  - `internal/queue` — `ReadyNotifier(defaultServerID)` (`queue.go:98-104`) must notify the right node, and cannot wake a remote one
  - `frontend/` — Vue admin module for servers, server IPs, IP maps, per-server config editor; a server selector on monitor and on every server-scoped list
- **DB**: no schema changes. `server`, `server_ip`, `server_ip_map`, `server_php`, `sys_datalog`, `sys_remoteaction`, `sys_log`, `monitor_data` are used as-is; `server_ip_map` needs a new GORM model only.
- **Operational**: the master DB must accept remote connections from each node; each node needs its own MySQL account; `server.updated` becomes a cross-host cursor.

## Non-goals

- **Failover of any kind.** ISPConfig3 has none (zero hits for `failover`/`keepalived`/`heartbeat` across the tree); the rescue path in `server/server.php:120-152` is a local-database rescue, not a promotion. This change does not add leader election, master promotion, or automatic `master_dsn` re-pointing. Mirroring gives a pre-configured standby; promoting it stays an operator task.
- MySQL/MariaDB replication between nodes, and any change to how the master DB itself is made highly available.
- Automated remote software installation (no SSH orchestration, no push-based agent). A node is joined by running the installer on that node, exactly as upstream does.
- `sys_dbsync`, OpenVZ/vserver nodes, XMPP and proxy server roles — out of scope roles.
- Cross-server object migration (moving a website from server A to server B) — a later change.
- Rewriting the API's remote-user model; per-server API scoping beyond the existing riud/admin checks.
- Translations beyond English.
