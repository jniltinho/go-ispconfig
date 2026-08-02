# Firewall module

Port of the ISPConfig3 firewall (`firewall_plugin.inc.php` +
`admin/form/firewall.tform.php`): a per-server UFW rule set driven from
the database through `sys_datalog`. REST API under `/api/firewall`,
panel UI under **System → Firewall**, daemon module + UFW plugin on
servers with `firewall_server = 1`.

Scope is **UFW only**. Bastille (the PHP plugin's second backend) and
raw `iptables` are non-goals (design D2/D7): the `iptables` table is a
schema-compatibility no-op and is deliberately not modelled.

## Data model (`firewall` table)

One row per server (schema identical to ISPConfig3):

| Column | Meaning |
|--------|---------|
| `firewall_id` | primary key |
| `server_id` | owning server — **UNIQUE** (one firewall row per server) and immutable after create |
| `tcp_port` | open TCP ports: comma-separated single ports or `lower:higher` ranges |
| `udp_port` | open UDP ports, same syntax |
| `active` | `y`/`n` — whether UFW ends up enabled |
| `sys_userid`/`sys_groupid`/`sys_perm_*` | standard riud ownership/permission columns |

Port lists accept digits, colons (ranges) and commas; the API validator
regex is byte-identical to the tform (`tcp_ports_error_regex` /
`udp_ports_error_regex`). Range bounds and the `1..65535` limit are
enforced by `CleanPorts` at apply time (a faithful port of
`clean_ports`), so migrated or hand-edited rows are defended in depth.

## REST API

`RegisterEntity[model.Firewall]` mounts `/api/firewall` (list, get,
create, update, delete). Every route is **admin-only** and further gated
by the `admin_allow_firewall_config` security policy (superadmin — user
id 1 — by default). `server_id` is UNIQUE (`firewall_error_unique`) and
immutable on update (`firewall_error_server_immutable`); a full-object
`PUT` that re-sends the unchanged `server_id` is accepted, only a real
change is rejected. Swagger lists all five routes.

## UFW apply pipeline (daemon)

The daemon runs on servers where `server.firewall_server = 1` and
`disable_firewall_module` is not set in `config.toml`. The module hooks
the `firewall` table and raises `firewall_insert` / `firewall_update` /
`firewall_delete`; the UFW plugin applies them (mirrored-server events
and non-local `server_id` are skipped):

1. **Probe** — `ufw --version` must be ≥ 0.30, else the apply is logged
   and skipped (PHP parity).
2. **Insert baseline** (first save only): `ufw --force disable`,
   `ufw --force reset`, `ufw default deny incoming`,
   `ufw default allow outgoing`.
3. **Differential ports**: clean old/new TCP and UDP lists, then
   `ufw allow <port>/<proto>` for ports newly present and
   `ufw delete allow <port>/<proto>` for ports removed.
4. **Activate**: `active = y` → `ufw --force enable` (or `ufw reload`
   when active was already `y`); `active = n` → `ufw disable`.
5. **Delete** (`firewall_delete`): `ufw --force reset` then
   `ufw disable`.

## Lock-out protection

Whenever UFW will end up enabled, the effective TCP allow-list is the
cleaned `tcp_port` set **unioned with the protected ports**, so a rule
set can never lock the operator out of the box (design D6). Protected
ports are never deleted and are force-added even if omitted from the
record:

- **Panel port** — `config.toml` `[server] port` (fallback `8080`).
- **SSH port** — `server.config`'s `[server] ssh_port` INI value
  (fallback `22`), read live via getconf.

The pure helper `firewall.EffectivePorts` computes this union and is
covered by unit tests (insert-empty, update-removes-ssh, custom panel
port, delete path) plus the end-to-end integration test.

## Configuration

| Source | Key | Default | Effect |
|--------|-----|---------|--------|
| `config.toml` | `[daemon] disable_firewall_module` | `false` | force-off the module/plugin even on a firewall server |
| `config.toml` | `[server] port` | `8080` | protected panel port |
| `server.config` | `[server] ssh_port` | `22` | protected SSH port |
| `server` row | `firewall_server` | `0` | the daemon loads the module only when `1` |

## Testing

- Unit: `CleanPorts`, the port regex, `EffectivePorts` lock-out union,
  the module event mapping, and the UFW command builder against a
  recording runner (`internal/firewall`).
- API integration: CRUD, datalog journaling, UNIQUE + immutable
  `server_id`, and 403 for a non-superadmin admin and a client
  (`TestFirewallAPI`).
- End-to-end: API → `sys_datalog` → daemon cycle → recorded UFW command
  sequence including the protected ports (`TestFirewallEndToEndFlow`).
- Panel E2E: `e2e/panel-firewall.sh` (`make e2e-firewall`) — create,
  edit ports, toggle active, delete, and a non-admin lock-out.

## Installation

Installing and enabling UFW itself is the installer's job
(`configure_ufw_firewall`, `openspec/changes/archive/…-add-installer-cli`);
this module manages the rules, not the package. A server opts in by
setting `firewall_server = 1` on its `server` row.
