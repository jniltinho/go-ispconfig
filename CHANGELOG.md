# Changelog

All notable changes to go-ispconfig are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

—

## [0.2.0] — 2026-08-03

**Native Debian and RPM packages.** `make deb` and `make rpm` build installable
packages, and the release workflow publishes both next to the tarball.

### Added

- `make deb` — Debian package (`go-ispconfig_<ver>_amd64.deb`) with the binary at
  `/usr/local/bin/go-ispconfig`, both systemd units, the example config under
  `/etc/go-ispconfig/`, and a `postinst` that creates the `go-ispconfig` system
  user, the `sshusers` group and `/etc/go-ispconfig/ssl`
- `make rpm` — RPM package (`go-ispconfig-<ver>-1.x86_64.rpm`) with the same
  layout and `%post` scriptlet
- Release workflow publishes the `.deb` and `.rpm` alongside the tarball

## [0.1.0] — 2026-08-03

First public release. ISPConfig3 panel re-implemented in Go + Vue 3. Apache 2.4 and nginx web servers, Bind and PowerDNS, MariaDB, Dovecot + Rspamd, pure-ftpd + shell users, fail2ban + getmail, multi-server management.

### ✨ New Features

- **12 modules**: mail, dns-bind, dns-powerdns, database, monitor, ftp-shell, cron, multi-server, apache2, fail2ban, getmail, rspamd
- **9 modules topnav** (Dashboard / Client / Sites / Email / DNS / Monitor / Help / Tools / System) matching legacy PHP order
- **Dashboard with Chart.js** — System Load, Memory, Network In/Out charts
- **Quota blocks** — Harddisk / Mailbox / Database with legacy orange/red bars
- **Account limits** dashlet per client (`limit_*`)
- **Mailbox form** with 5 tabs (Mailbox / Autoresponder / Mail Filter / Custom Rules / Backup)
- **Web domain form** with server/client/IP lookups
- **Multi-server registry** — server CRUD, role validation, node identity
- **server-config-sync** — per-server config read/write with datalog delivery
- **Apache2 plugin** — split ensureSite, vhost render, PHP-FPM pool, .htaccess
- **fail2ban module** — panel-managed jails, live ban view, unban action, prune orphans
- **getmail module** — fetch external POP3/IMAP via systemd timer
- **rspamd module** — policy API, global actions.conf, per-domain settings, DKIM
- **PowerDNS support** — GORM models, SQL zone sync, AXFR allow-list, pdnsutil
- **Install pipeline** — apt packages → mariadb → server-ip → panel user → config-toml → TLS cert → nginx-base → apache2 → bind-base → powerdns → pure-ftpd → fail2ban → rspamd → getmail → systemd units
- **Datalog → daemon → config apply architecture** with `nginx -t` / `named-checkzone` validation and rollback
- **Installer flags**: `--web-server apache2|nginx`, `--dns-backend bind|powerdns`, `--yes`, `--update`, `--write-credentials`
- **OpenSpec change archive** (21+ changes)

### 🔧 Improvements

- Split `internal/nginx/ensure.go` and `internal/apache2/ensure.go` into a shared `internal/site/` package
- Live PR + Cursor validation of every module
- Daemon datalog routing per `server_id`

### 🐛 Fixes

- `php_open_basedir` derivation missing in web_domain insert → pool FPM got minimal `open_basedir`
- `database-users` entity missing `client_group_id` → admin couldn't create DB for client
- Installer hardcoded `nginx` for fail2ban HTTP jail
- Installer missed `nginx.service` enable on apache2 path
- Rspamd `users.conf` write without `rspamd.conf` `.include` → config morto
- Echo v5 route miss returned 500 instead of 404
- `fail2ban` orphan drop-ins lingered when web server switched
- Apache2 plugin missing site file tree (ensureSite extraction)

### 🧪 Tests

- E2E validated on Ubuntu 24.04 (`.10`, `.22`) and Debian 13 (`.12`)
- 2 clients, 4 sites, 2 WordPress 6.6.2, 4 FTP, 2 shell users, 4 mailboxes — all PASS
- Lab `.20` (PHP legacy nginx) and `.21` (PHP legacy apache2) used as parity baseline
- CI: lint / test / frontend / swagger-check all green

### 📚 Documentation

- README.md with module status table, lab credentials, quickstart
- `docs/architecture.md` — package map and datalog flow
- `docs/research/ispconfig3-architecture.md` — ISPConfig3 original architecture
- `docs/research/ispconfig3-theme.md` — visual tokens
- `docs/monitor-module.md` — collectors + dashboard
- `docs/powerdns-module.md` — installer / ROADMAP
- `openspec/AGENTS.md` — proposal workflow
- `.hermes/prints/` — fail2ban-parity.md, module-parity.md, e2e-final-report.md, dashboard-* prints

### 📦 Package

- linux/amd64: `go-ispconfig_0.1.0_linux_amd64.tar.gz`
