# go-ispconfig — Roadmap

Every module has an OpenSpec change under `openspec/changes/`. Phase 1 changes are
fully specified (proposal + design + specs + tasks); future modules carry a
proposal now and get design/specs/tasks when scheduled.

## Phase 1 — Foundation + initial modules (now)

| Change | Scope |
|---|---|
| `port-ispconfig3-to-go` | Foundation: CLI, identical DB schema, sys_datalog engine, daemon with internal scheduler (no system cron), auth/riud permissions, REST API + Swagger, .master template engine, Vue panel skeleton |
| `add-web-nginx-module` | Sites: nginx vhosts, PHP-FPM pools, SSL/Let's Encrypt |
| `add-dns-bind-module` | DNS: Bind zones, records, templates, DNSSEC |
| `add-installer-cli` | `go-ispconfig install` for Debian 11–13 / Ubuntu 22.04–24.04 + Vagrant test rig |
| `add-panel-ui-theme` | ISPConfig-derived theme, modernized, square corners, dark mode |
| `add-legacy-migration` | Import wizard/CLI from a running PHP ISPConfig3 via remote API |

## Phase 2 — Future modules (proposals ready, implementation later)

| Change | Scope (ISPConfig3 origin) |
|---|---|
| `add-mail-module` | Postfix/Dovecot/Rspamd: mail domains, mailboxes, forwarding, spamfilter (`mail_module`, `mail_plugin`, `rspamd_plugin`) |
| `add-client-module` | Client/reseller management, limits, templates, messaging (`client_module`, interface client module) |
| `add-database-module` | Client MySQL databases and users (`database_module`, `mysql_clientdb_plugin`) |
| `add-ftp-shell-module` | FTP users (PureFTPd) and shell users incl. jailkit (`ftpuser_base`, `shelluser_*` plugins) |
| `add-cron-module` | Client cron jobs (`cron_module`, `cron_plugin`) — executed by the go-ispconfig daemon scheduler |
| `add-firewall-module` | UFW/nftables management (`firewall_plugin`) |
| `add-monitor-module` | Server monitoring, logs, datalog history UI (`monitor_core_module`) |
| `add-dns-powerdns-module` | PowerDNS as alternative DNS backend — same DNS UI/API, SQL zone sync (`powerdns_plugin`, `powerdns.sql`) |
| `add-xmpp-module` | XMPP domains/users (`xmpp_module`, `xmpp_plugin`) — future only, proposal when scheduled |
| `add-vm-module` | OpenVZ/VM management (`vm_module`, `openvz_plugin`) — future only, proposal when scheduled |
| `add-web-apache-module` | Apache2 as alternative web backend (`apache2_plugin`) — future only, proposal when scheduled; a real Apache2+PHP-FPM reference server exists for validation (see AGENTS.local.md) |
| `add-aps-module` | APS package installer (`aps_plugin`, `aps_*` tables) — future only, proposal when scheduled |
| `add-mailinglist-module` | Mailing lists (`mailman_plugin`, `mail_mailinglist`) — future only, proposal when scheduled |

## Not planned

MyDNS — legacy; tables remain in the schema for compatibility.
