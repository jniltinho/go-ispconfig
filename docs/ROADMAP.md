# go-ispconfig — Roadmap

Every module is specified first: consolidated capabilities live in
`openspec/specs/`, and the change that produced each one is archived under
`openspec/changes/archive/`. Future modules carry a proposal now and get
design/specs/tasks when scheduled.

## Implemented (changes archived)

| Change | Scope |
|---|---|
| `port-ispconfig3-to-go` | Foundation: CLI, identical DB schema, sys_datalog engine, daemon with internal scheduler (no system cron), auth/riud permissions, REST API + Swagger, .master template engine, Vue panel |
| `add-web-nginx-module` | Sites: nginx vhosts, PHP-FPM pools, SSL/Let's Encrypt ([docs/nginx-module.md](nginx-module.md)) |
| `add-dns-bind-module` | DNS: Bind zones, records, templates, DNSSEC ([docs/dns-module.md](dns-module.md)) |
| `add-dns-powerdns-module` | PowerDNS as alternative DNS backend — same UI/API, gmysql zone sync, picked at install time ([docs/powerdns-module.md](powerdns-module.md)) |
| `add-mail-module` | Postfix/Dovecot/Rspamd: mail domains, mailboxes, forwarding, DKIM, spamfilter ([docs/mail-module.md](mail-module.md)) |
| `add-database-module` | Client MySQL databases and users ([docs/database-module.md](database-module.md)) |
| `add-ftp-shell-module` | FTP users (PureFTPd, virtual auth) and shell users incl. jailkit ([docs/ftp-shell-module.md](ftp-shell-module.md)); the installer provisions PureFTPd since the `pure-ftpd` step — `configure_jailkit` and FTPS remain Modified Capabilities of `add-installer-cli` |
| `add-cron-module` | Site cron jobs executed by the daemon scheduler ([docs/cron-module.md](cron-module.md)) |
| `add-firewall-module` | Per-server UFW rule sets ([docs/firewall-module.md](firewall-module.md)) |
| `add-client-module` | Client/reseller management, limits, templates, messaging ([docs/client-module.md](client-module.md)) |
| `add-monitor-module` | Server/service/quota monitoring, `monitor_data` history, dashboard dashlets ([docs/monitor-module.md](monitor-module.md)) |
| `add-installer-cli` | `go-ispconfig install` for Debian 11–13 / Ubuntu 22.04–24.04 + Vagrant test rig ([docs/install.md](install.md)) |
| `add-legacy-migration` | Import wizard/CLI from a running PHP ISPConfig3 via remote API ([docs/legacy-migration.md](legacy-migration.md)) |
| `add-legacy-test-lab` | Standing PHP ISPConfig3 lab VMs used as the parity baseline |
| `add-panel-ui-theme`, `ui-forms-tables-qa`, `ui-mail-login-aaa` | Panel theme and UI parity sweeps (Vue 3 + Tailwind v4, dark mode, dashlets) |

## Future modules (proposal when scheduled)

| Change | Scope (ISPConfig3 origin) |
|---|---|
| `add-xmpp-module` | XMPP domains/users (`xmpp_module`, `xmpp_plugin`) — future only, proposal when scheduled |
| `add-vm-module` | OpenVZ/VM management (`vm_module`, `openvz_plugin`) — future only, proposal when scheduled |
| `add-web-apache-module` | Apache2 as alternative web backend (`apache2_plugin`) — future only, proposal when scheduled; a real Apache2+PHP-FPM reference server exists for validation (see AGENTS.local.md) |
| `add-aps-module` | APS package installer (`aps_plugin`, `aps_*` tables) — future only, proposal when scheduled |
| `add-mailinglist-module` | Mailing lists (`mailman_plugin`, `mail_mailinglist`) — future only, proposal when scheduled |

## Not planned

MyDNS — legacy; tables remain in the schema for compatibility.
