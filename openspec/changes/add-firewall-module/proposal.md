# Proposal: Firewall Module (UFW)

> Roadmap phase 2 — proposal only for now; design/specs/tasks will be authored when the module is scheduled. Depends on the foundation change `port-ispconfig3-to-go`.

## Why

A hosting server exposes many services; admins need a simple, panel-managed way to open only the required TCP/UDP ports per server. This ports the ISPConfig3 firewall plugin, targeting **UFW** — the native firewall frontend on our supported Debian/Ubuntu targets.

## What Changes

- **Firewall records**: CRUD on the existing `firewall` table (server_id, tcp_port list, udp_port list, active) — one record per server, admin-only (as in ISPConfig3).
- **Daemon plugin**: Go port of the UFW path of `server/plugins-available/firewall_plugin.inc.php` (events `firewall_insert/update/delete`, functions `ufw_update`/`ufw_delete`/`clean_ports`): normalize/validate port lists (comma-separated, ranges `a:b`), reset and re-apply UFW rules (`ufw allow <port>/tcp|udp`), enable/disable UFW, default deny incoming / allow outgoing, and always keep the panel and SSH ports reachable (lock-out guard). The canonical sources of the protected ports are the panel port from `config.toml` and the SSH port from `server.config`; a mandatory test asserts that no reset/apply cycle can ever close these ports.
- **UI**: System module gains a **Firewall** section (list + edit form with tcp/udp port fields and active flag), port of `interface/web/admin/firewall_*.php`.
- **REST API**: firewall_add/get/update/delete endpoints mirroring `remote.d/admin` firewall functions, Swagger-documented, admin access level only.
- **Installer**: `add-installer-cli` gains the `configure_ufw_firewall` step (port from `install/lib/installer_base.lib.php`) — tracked there, listed here for visibility.

## Capabilities

### New Capabilities

- `firewall-module-events`: daemon firewall module — table hook for `firewall`, named event dispatch (`firewall_insert/update/delete`), enablement gated on `server.firewall_server` + config.toml.
- `firewall-ufw-plugin`: UFW apply path (`clean_ports`, `ufw_update`, `ufw_delete`) with panel/SSH lock-out protection.
- `firewall-record-management`: API-side domain logic — validation, one-row-per-server, admin + `admin_allow_firewall_config` policy, riud stamps, datalog writes.
- `firewall-rest-api`: REST CRUD endpoints for `firewall` (Swagger, admin-only), panel/remote shared write path.
- `firewall-panel-ui`: System → Firewall list + form (Vue DataTable/TabbedForm, i18n).

### Modified Capabilities

(none — new plugin on the existing datalog/event engine; installer `configure_ufw_firewall` is a Modified Capability of `add-installer-cli`, tracked there)

## Impact

- Reference PHP sources: `server/plugins-available/firewall_plugin.inc.php` (UFW branch), `interface/web/admin/firewall_*.php`, and `iptables_plugin.inc.php` (reviewed for the raw-rules variant; not ported — see Non-goals).
- Tables: `firewall` (used), `iptables` (stays unused, schema compatibility only).
- System package: ufw (Debian/Ubuntu). Requires `server.firewall_server = 1` flag.
- Interacts with nothing else at runtime; other modules do not open ports automatically (same as ISPConfig3).

## Non-goals

- Bastille firewall support (the plugin's other branch — legacy, Debian dropped it).
- Raw iptables/nftables rule management (`iptables_plugin.inc.php`, `iptables` table) — UFW abstraction only.
- fail2ban installation or jail management (monitoring shows its status only).
- Per-client or per-site firewall rules (server-level, admin-only, as upstream).
- IP-set/blocklist feeds, rate limiting, port knocking.
