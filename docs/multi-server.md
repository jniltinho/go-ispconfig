# Multi-server setup

One panel and database, several nodes applying their own slice of the change
journal. This is the ISPConfig3 topology — a master that owns the interface
and the database, plus web / mail / DNS / database nodes that pull their work
from it — and the pieces that make it work are already in the Go port.

**Read this first:** the daemon side is multi-server today; the *setup* is
still manual. There is no `install --join`, no master/slave database split and
no mirror support. The proper flow is specified in
`openspec/changes/add-multiserver-mgmt/`; what follows is the procedure that
works with the current binary, and exactly where it is rough.

## How a node knows which server it is

`engine.ResolveServer` (`internal/engine/daemon.go`) picks the `server` row a
node serves, in this order:

1. **`server_id` in `config.toml`** — the explicit answer. Set it on every
   node of a multi-server install.
2. **hostname match** — the active row whose `server_name` equals the OS
   hostname.
3. **the only active row** — the single-server case.

With several active rows and no match, the daemon **refuses to start** rather
than apply another node's changes. That refusal is the safety property: a
misconfigured node does nothing instead of rendering someone else's vhosts.

The resolved row then drives everything else:

- the datalog cursor is per row — the engine reads
  `WHERE datalog_id > server.updated AND (server_id = <mine> OR server_id = 0)`
  and advances `server.updated` on that row only, so nodes never consume each
  other's work (`server_id = 0` means "every server", e.g. a global config
  change);
- **module loading is gated by the row's role flags** — `web_server`,
  `mail_server`, `dns_server`, `db_server`, `firewall_server`. A node with
  `mail_server = 0` never loads the mail plugin, so it cannot write Postfix
  config even if a mail event reaches it;
- `server.config` is per row, so each node has its own paths, PHP settings and
  DNS backend (see [server-config-module.md](server-config-module.md)).

## What a node needs

| | Master | Additional node |
|---|---|---|
| `go-ispconfig-serve` (panel + API) | yes | optional — it is just a second panel against the same DB |
| `go-ispconfig-daemon` | yes | **yes** |
| MariaDB | owns `dbispconfig` | reaches the master's `dbispconfig` over the network |
| Redis / Valkey | yes | optional (see below) |
| Its own `server` row | seeded at install | created by you |

**Redis is optional per node.** It only carries the instant wake-up; with no
reachable Redis the daemon falls back to its `tick_seconds` poll and applies
exactly the same changes, a few seconds later. `sys_datalog` is the source of
truth, never the queue.

## Adding a node

### 1. Create the server row on the master

There is no Server Services screen yet (it is specified in
`openspec/changes/complete-system-module/`), so create the row through the
API. Note the returned `server_id`.

```bash
TOKEN=$(curl -sk -X POST https://master:8080/api/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"…"}' | jq -r .session_id)

curl -sk -X POST https://master:8080/api/server \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{
        "server_name": "mail1.example.com",
        "mail_server": 1,
        "web_server": 0, "dns_server": 0, "db_server": 0,
        "firewall_server": 1,
        "active": 1
      }'
```

Set only the roles that node should apply. `server_name` **must** match the
node's OS hostname if you want the hostname fallback to work; setting
`server_id` explicitly in step 3 makes it irrelevant either way.

### 2. Let the node reach the master database

On the **master's** MariaDB, grant the ISPConfig user from the node's address
and make sure MariaDB listens on something other than the loopback:

```sql
CREATE USER 'ispconfig'@'10.0.0.20' IDENTIFIED BY '<the master's DB password>';
GRANT ALL PRIVILEGES ON dbispconfig.* TO 'ispconfig'@'10.0.0.20';
FLUSH PRIVILEGES;
```

The password is the one in the master's `/etc/go-ispconfig/config.toml` DSN.
Restrict the grant to the node's address — never `'%'` — and put the link on a
private network or a VPN. The datalog carries password hashes and DKIM keys.

### 3. Install and repoint the node

Install normally, then point it at the master:

```bash
go-ispconfig install --yes --hostname mail1.example.com --admin-email admin@example.com
```

Then edit `/etc/go-ispconfig/config.toml` on the node:

```toml
# The row created in step 1.
server_id = 3

[database]
dsn = "ispconfig:<master DB password>@tcp(10.0.0.10:3306)/dbispconfig?charset=utf8mb4&parseTime=True&loc=Local"

[queue]
# Point at the master's Redis, or leave it unreachable and rely on tick polling.
addr = "10.0.0.10:6379"
```

Restart the daemon and confirm it resolved the right row:

```bash
systemctl restart go-ispconfig-daemon
journalctl -u go-ispconfig-daemon -n 20 --no-pager
```

The startup line names the server id and the modules it loaded. If it refuses
to start, the message says why — almost always a `server_id` that does not
exist, is not `active`, or a hostname that matches nothing.

**The rough edge:** `install` always provisions a local MariaDB and seeds a
local `dbispconfig` with its own `server` row, because there is no
`--db-host`/`--join` flag yet. On a node that talk to the master's database
that local database is dead weight — you may drop it once the node is running
against the master, but check first that the node is not still pointed at it.

### 4. Turn the panel off on the node (optional)

The node does not need to serve the panel:

```bash
systemctl disable --now go-ispconfig-serve
```

Leaving it running is harmless — it is another panel against the same
database — but it is another exposed port to keep patched.

## Placing resources on a node

Every entity that belongs to a machine carries `server_id`. The panel forms
show a **Server** select on web domains, mail domains, DNS zones, databases
and firewall rules; picking a node there routes the datalog row to it. The
select is populated by `/api/meta/lookups/servers` and lists every active row,
so a node appears there as soon as step 1 is done.

`server_id` is deliberately **immutable after creation** on those entities:
moving a site between nodes would leave its files, its vhost and its database
behind on the old one. Migrating a resource means recreating it on the target
node and moving the data yourself.

## What is not supported yet

| | Status |
|---|---|
| **Mirror servers** (`mirror_server_id`) | The API validates the field and refuses illegal targets, but nothing replicates a mirror's configuration. Leave it at 0. |
| **Master/slave DB split** | ISPConfig3 gives a slave a local database plus a `dbmaster` connection. Here every node talks directly to the master's `dbispconfig`, so the master database is a hard dependency of every node. |
| **`install --join`** | Not implemented; steps 1–3 above are the manual equivalent. |
| **Server Services UI** | Role flags are editable through `/api/server` only. |
| **Server IPv4 mapping** (`server_ip_map`) | Table exists, nothing reads it. |
| **Per-node credential separation** | Every node uses the same `ispconfig` DB user. ISPConfig3 issues one `ispcsrv<N>` user per node with narrower grants. |

All of these are covered by `openspec/changes/add-multiserver-mgmt/`.

## Troubleshooting

**The daemon refuses to start.** Read the message: it names the resolution
failure. `server_id N configured but not found` means the row does not exist;
`server N is not active` means `active = 0`; `N active server rows and none
matches this hostname` means you left `server_id = 0` on a multi-server
install.

**A node applies nothing.** Check `server.updated` on its row against
`MAX(datalog_id)`:

```sql
SELECT server_id, server_name, updated FROM server;
SELECT MAX(datalog_id) FROM sys_datalog;
```

A cursor far behind the maximum with an idle daemon usually means the node is
resolving a *different* row than you think — the startup log line is the
answer.

**Changes apply on the wrong machine.** Two nodes resolved the same row.
Set `server_id` explicitly on both and restart.

**A role does nothing.** The module is gated on the role flag *and* on the
`disable_*_module` switches in `[daemon]`. Both must allow it.
