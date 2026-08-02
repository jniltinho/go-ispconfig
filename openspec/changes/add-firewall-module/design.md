# Design: Firewall module (UFW)

## Context

ISPConfig3's firewall stack is thin and server-scoped:

1. `interface/web/admin/firewall_*.php` + `form/firewall.tform.php` + `list/firewall.list.php` — admin-only CRUD on the `firewall` table (one row per server: `tcp_port`, `udp_port`, `active`), gated by module permission `admin` and security policy `admin_allow_firewall_config`. Writes go through tform → `sys_datalog` with `{old,new}` JSON.
2. `server/mods-available/server_module.inc.php` — registers a table hook for `firewall` and raises `firewall_insert` / `firewall_update` / `firewall_delete`.
3. `server/plugins-available/firewall_plugin.inc.php` — on those events, when `server.config` `[server] firewall=ufw`, runs the UFW path (`ufw_update` / `ufw_delete` / `clean_ports`): normalize port lists, differential allow/delete of TCP/UDP ports, default deny in / allow out on first insert, enable/reload/disable based on `active`. Bastille is the other branch (legacy; not ported).

There is **no** `firewall_*` surface in `remote.d/admin.inc.php` (or any other remote class) in current ISPConfig3 — panel-only in PHP. go-ispconfig still exposes a REST CRUD surface (same as other modules) so the Vue panel and remote automation share one write path.

The foundation already provides: datalog consumer, Module/Plugin registries, command runner, riud scopes, security policies (`admin_allow_firewall_config` is already seeded in `internal/auth/policy.go`), declarative `Entity` CRUD framework (`RegisterEntity`), Vue `DataTable`/`TabbedForm`, and `server.firewall_server` on the GORM model. The `firewall` table exists byte-identical in `internal/database/ispconfig3.sql`; only the GORM model and logic are missing. The `iptables` table stays unused (schema compatibility only — non-goal).

## Goals / Non-Goals

**Goals:**
- Behavior-faithful port of the UFW path of `firewall_plugin` plus the admin panel form/list semantics.
- Admin-only REST + System UI CRUD on `firewall` (one record per `server_id`), with riud ownership stamps and `{old,new}` datalog rows targeted at `server_id`.
- Lock-out guard (Go hardening beyond PHP): panel listen port and SSH port are never closed by any apply/reset/delete cycle. Covered by a mandatory unit test.
- Installer step `configure_ufw_firewall` is listed for visibility; implementation lives in `add-installer-cli` (Modified Capability there).

**Non-Goals:**
- Bastille firewall branch (`bastille_update` / `bastille_delete`, `bastille-firewall.cfg.master`).
- Raw iptables/nftables rule management (`iptables_plugin.inc.php`, `iptables` table).
- fail2ban, per-client/per-site rules, IP-set/blocklist feeds, rate limiting, port knocking.
- No schema changes of any kind.

## Decisions

### D1 — Dedicated `internal/firewall` package (module + plugin)
PHP nests the table hook in `server_module` and the apply logic in `firewall_plugin`. In Go we follow the dns/web pattern: one package `internal/firewall` with two registrations —
- **Module**: announces `firewall_insert` / `firewall_update` / `firewall_delete`, hooks table `firewall`, maps datalog `i`/`u`/`d` → events (port of `server_module::process` case `firewall`).
- **Plugin**: subscribes to those three events and runs the UFW path only.

Keeping the two-level dispatch (hook → named event → plugin) preserves the foundation registry contract and leaves room for a future alternative backend (e.g. nftables) without rewiring the API.
*Alternative*: collapse hooks into the plugin — rejected: breaks the announced-events contract shared with other modules.

### D2 — UFW-only backend; ignore Bastille
`server.config` `[server] firewall` may still say `bastille` in migrated DBs. The plugin always takes the UFW path when the module is loaded (supported distros ship UFW; Bastille is a non-goal). If `ufw` is not installed or version `< 0.30`, the plugin logs a warning and returns without changing rules (PHP parity for the missing-binary path). No Bastille templates or init-script handling.

### D3 — Module enablement: `firewall_server` + config.toml
The module loads only when the daemon's local `server` row has `firewall_server = 1` **and** the module is enabled in `config.toml` (same gate style as `dns_server` for the dns module). Datalog rows for other servers are still journaled by the API; only the local daemon applies when its own server matches the row's `server_id` (foundation single-server scope today).

### D4 — Port list normalization is a pure function
Port of `firewall_plugin::clean_ports($portlist, $spacer)`:

- Split on `,`; for each token:
  - range `a:b` → both ends `intval` in `1..65535` and lower < higher → keep `a:b`;
  - single → `intval` in `1..65535` → keep;
  - otherwise drop.
- Re-join with the spacer (`,` for UFW path). Empty input → empty output.

API-side form validation ports the tform REGEX on both fields:

```
/^$|\d{1,5}(?::\d{1,5})?(?:,\d{1,5}(?::\d{1,5})?)*$/
```

(`tcp_ports_error_regex` / `udp_ports_error_regex`). Daemon-side `clean_ports` remains defense in depth for migrated/hand-edited rows.

### D5 — Apply pipeline mirrors `ufw_update` / `ufw_delete`
**Insert / update** (`ufw_update`):

1. Require `ufw` installed and version ≥ 0.30 (probe `ufw --version`); else warn + return.
2. On **insert only**: `ufw --force disable`, `ufw --force reset`, `ufw default deny incoming`, `ufw default allow outgoing` (PHP baseline).
3. Diff cleaned `tcp_port` / `udp_port` old vs new arrays; for each port present only in new and `> 0`: `ufw allow <port>/tcp|udp`; for each only in old: `ufw delete allow <port>/tcp|udp`. Sleeps between rules are **not** ported (PHP had `sleep(1)` for rate-limiting on slow hosts; unnecessary and slows the daemon).
4. If `new.active == 'y'`: when active was already `'y'`, `ufw reload`; when freshly enabled, `ufw --force enable` (Bastille stop/update-rc.d not ported). If inactive: `ufw disable`.

**Delete** (`ufw_delete`): `ufw --force reset` then `ufw disable`.

All shell-outs go through the foundation command runner (argv slices, no shell interpolation, fakeable in tests).

### D6 — Lock-out guard (mandatory, Go hardening)
PHP will happily open a default-deny UFW policy without SSH or the panel port if the admin omitted them — a common self-lockout. go-ispconfig never does that.

**Protected TCP ports** (always open while UFW is/will be enabled):

| Source | Key | Fallback |
|---|---|---|
| Panel listen port | `config.toml` → `server.port` (daemon reads the same config the panel uses) | `8080` (app default) |
| SSH port | `server.config` `[server] ssh_port` (Go addition; not present in stock ISPConfig INI) | `22` |

Rules:

1. Pure function `EffectivePorts(tcp, udp, protected []string) (tcpOut, udpOut)` unions protected TCP ports into the cleaned TCP list before any allow/delete/enable decision.
2. The apply path never issues `ufw delete allow <protected>/tcp` while the resulting state would leave UFW enabled without that port.
3. After any `ufw --force reset` (insert baseline and delete), the plugin re-allows every protected TCP port **before** `ufw --force enable` (insert) or leaves UFW disabled after delete (disabled = all open, so lock-out cannot occur on the delete path; protected re-allow is still applied if a future path re-enables immediately).
4. A unit test with a recording runner asserts: for every fixture (insert empty ports, update that removes 22/panel, delete, active toggle), the command sequence never ends in "UFW enabled without both protected ports allowed".

Protected ports are injected at **daemon apply time** so the live host is always safe. The API does **not** silently rewrite the stored `tcp_port` (admin sees and edits what they saved); the UI help text documents that panel and SSH ports stay open even if omitted.

### D7 — GORM model on the existing `firewall` table only
Columns (confirmed in `ispconfig3.sql`, no alterations):

| Column | Type | Notes |
|---|---|---|
| `firewall_id` | int unsigned PK AI | |
| `sys_userid` / `sys_groupid` | int unsigned | ownership |
| `sys_perm_user` / `sys_perm_group` / `sys_perm_other` | varchar(5) | riud strings; tform preset `riud`/`riud`/`''` |
| `server_id` | int unsigned | UNIQUE at application level (tform UNIQUE validator) |
| `tcp_port` | text | comma-separated ports/ranges |
| `udp_port` | text | comma-separated ports/ranges |
| `active` | enum('n','y') default 'y' | |

Model tags are explicit `gorm:"column:..."`. The unused `iptables` table gets **no** model in this change.

Defaults on create (from `firewall.tform.php`):

- `tcp_port`: `21,22,25,53,80,110,143,443,465,587,993,995,3306,4190,8080,8081,40110:40210`
- `udp_port`: `53`
- `active`: `y`

### D8 — Admin-only Entity CRUD + security policy
Use the foundation `RegisterEntity[model.Firewall]` pattern (same as `server_ip`):

- `AdminOnly: true` (PHP: `is_admin()` on list/edit/delete).
- `Policy: "admin_allow_firewall_config"` (default `superadmin` — only `sys_user.id = 1` unless the policy is relaxed).
- `server_id` UNIQUE (error key `firewall_error_unique`); on update, `server_id` is immutable (port of `firewall_edit.php::onBeforeUpdate` — "The Server can not be changed.").
- Create form's server SELECT lists only servers that do **not** already have a firewall row (PHP `onShowEnd`); API rejects duplicates with the uniqueness error.
- Mutations write `{old,new}` datalog rows with `dbtable=firewall`, `dbidx=firewall_id=<id>`, `server_id=<row.server_id>`, action `i`/`u`/`d`. Interface never touches UFW.

REST routes under `/api/firewall` (or entity default `/api/firewall` via `Name: "firewall"`): list/get/create/update/delete — conventional names `firewall_add/get/update/delete` in docs, even though PHP remote never exposed them.

### D9 — System UI section, not a top-level module
PHP places Firewall under System (`admin` module nav). Vue:

- `modules.ts` System sections gain **Firewall** (`adminOnly: true`) → `/system/firewall`.
- List: `DataTable` columns active / server / tcp_port / udp_port (port of `firewall.list.php`).
- Form: single-tab `TabbedForm` driven by entity metadata (server select, tcp_port, udp_port, active checkbox) — port of `firewall.tform.php` single tab.
- All strings via i18n `en.json` (labels from `en_firewall.lng` / `en_firewall_list.lng`).

### D10 — Installer visibility only
`configure_ufw_firewall` (copy `ufw.conf.master` → `/etc/ufw/ufw.conf` on fresh install) is implemented as a Modified Capability of `add-installer-cli`, not here. This change documents the dependency and expects `server.firewall_server=1` + UFW package when the installer enables the firewall service. No installer code lands in this change's tasks beyond a docs cross-link.

## Risks / Trade-offs

- [Self-lockout if lock-out guard regresses] → pure `EffectivePorts` + recording-runner tests that fail CI when protected ports are deleted or missing after enable; guard cannot be feature-flagged off.
- [Differential update drifts from a full reset policy] → same as PHP: first insert resets; later updates only diff. Document that manual `ufw` edits outside the panel are not reconciled until the next insert-equivalent resync (delete + recreate, or future resync action).
- [Migrated DB with `firewall=bastille`] → UFW path still runs; if UFW is absent, plugin no-ops with a clear log. Operators migrating from Bastille must install UFW (installer change).
- [Removing `sleep(1)` between ufw rules] → intentional; if a host shows racey ufw CLI failures under load, reintroduce a small delay behind a config knob — not observed as required on modern UFW.
- [Admin omits SSH from `tcp_port` and thinks it is closed] → UI help text + docs clarify injection-at-apply; optional future: surface "effective ports" read-only in the form (out of scope unless needed).

## Migration Plan

- Code only — no DDL, no forced data rewrite.
- Fresh installs: installer (separate change) may seed a default `firewall` row and set `firewall_server=1` / `server.config` `firewall=ufw` + `ssh_port=22`.
- Migrated ISPConfig3 databases: existing `firewall` rows work as-is; first datalog event after cutover applies UFW from DB state. Bastille configs on disk are left untouched (orphan).
- Rollback: disable the firewall module in `config.toml`; UFW rules on disk stay as last applied (same as PHP when the plugin is unlinked).

## Open Questions

- Should a future "resync" admin action force a full `ufw --force reset` + re-apply of the current row (insert path) for operators who edited UFW by hand? Not required for parity; revisit if support burden appears.
- Multi-server: when go-ispconfig gains multi-server daemons, each daemon already filters by local `server_id`; no design change expected.
