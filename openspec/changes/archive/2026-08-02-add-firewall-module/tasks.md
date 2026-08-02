# Tasks: add-firewall-module

## 1. Models and domain validation

- [x] 1.1 Add GORM model `Firewall` for table `firewall` with explicit `gorm:"column:..."` tags for `firewall_id`, `sys_userid`, `sys_groupid`, `sys_perm_user`, `sys_perm_group`, `sys_perm_other`, `server_id`, `tcp_port`, `udp_port`, `active`; unit-test round-trip against MariaDB. Do not model `iptables`. Commit.
- [x] 1.2 Implement pure `CleanPorts` (port of `firewall_plugin::clean_ports`) and the tform port-list REGEX validator; table-driven tests for singles, ranges, invalid tokens, empty input. Commit.
- [x] 1.3 Implement firewall domain rules on the foundation validation/entity stack: `server_id` UNIQUE (`firewall_error_unique`), port regex error keys, create defaults (`tcp_port` / `udp_port` / `active` from `firewall.tform.php`), immutable `server_id` on update; unit tests. Commit.

## 2. Daemon module and UFW plugin

- [x] 2.1 Implement `internal/firewall` Module: announce `firewall_insert`/`firewall_update`/`firewall_delete`, register table hook for `firewall`, map datalog `i`/`u`/`d` → events; gate on `server.firewall_server=1` + config.toml enablement; unit tests with fake registries. Commit.
- [x] 2.2 Implement UFW version/install probe and `ufw_update` apply path (insert baseline reset + defaults, differential allow/delete for tcp/udp, active → enable/reload/disable) via the foundation command runner; skip when not local `server_id`; tests with recording runner. Commit.
- [x] 2.3 Implement `ufw_delete` (force reset + disable) and wire plugin handlers to the three events; tests. Commit.
- [x] 2.4 Implement lock-out guard: protected TCP ports from `config.toml` `server.port` (fallback 8080) and `server.config` `[server] ssh_port` (fallback 22); `EffectivePorts` union; never delete/omit protected ports while UFW ends enabled; mandatory unit tests covering insert-empty, update-removes-ssh, custom panel port, delete path. Commit.
- [x] 2.5 Register the firewall module/plugin in the daemon bootstrap next to web/dns; document the config.toml enablement key. Commit.

## 3. REST API

- [x] 3.1 Register declarative `RegisterEntity[model.Firewall]` under `/api/firewall` with `AdminOnly`, `Policy: admin_allow_firewall_config`, datalog writer, server select limited to servers without a row on create; handler tests for create/list/get/update/delete, 403 for client and non-policy admin, unique server_id, immutable server_id. Commit.
- [x] 3.2 Add swaggo annotations for all firewall routes; run `make swagger`; verify Swagger UI lists the endpoints; CI staleness check green. Commit.

## 4. Panel UI (Vue)

- [x] 4.1 Add System → Firewall nav (`adminOnly`) and routes for list + form; English i18n keys ported from `en_firewall.lng` / `en_firewall_list.lng` plus lock-out help text. Commit.
- [x] 4.2 Implement firewall list with `DataTable` (active, server, tcp_port, udp_port; add/edit/delete + confirm). Commit.
- [x] 4.3 Implement firewall form with metadata-driven `TabbedForm` (defaults, port validation, inline API errors, server immutable on edit). Commit.
- [x] 4.4 agent-browser E2E: admin create → edit ports → toggle active → delete; non-admin cannot open section; screenshots to `docs/prints/`. Commit.

## 5. Integration and docs

- [x] 5.1 End-to-end integration test against MariaDB: API create/update/delete → `sys_datalog` rows → daemon run with fake UFW runner → expected command sequences including protected ports. Commit.
- [x] 5.2 Module docs in `docs/` (firewall table fields, UFW apply pipeline, lock-out sources, config.toml + `ssh_port`, non-goals Bastille/iptables, cross-link installer `configure_ufw_firewall` in `add-installer-cli`). Commit.
