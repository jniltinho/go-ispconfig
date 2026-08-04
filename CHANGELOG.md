# Changelog

All notable changes to go-ispconfig are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed

- **`--acme-client certbot` no longer installs `python3-certbot-nginx` /
  `python3-certbot-apache`.** v0.5.0 pulled them in; nothing uses them. Site
  certificates are issued with `--authenticator webroot`, as the legacy does,
  and the panel's own certificate is served by `go-ispconfig-serve` with no
  vhost in front of it, so a webserver plugin has nothing to edit either way

### Documentation

- `docs/install.md` gained an ACME section: which client the installer puts on
  the box, why issuance is webroot-only, and how to replace the self-signed
  panel certificate
- `add-apache2-letsencrypt` proposal — Apache sites can renew a Let's Encrypt
  certificate but not issue one

## [0.5.0] — 2026-08-04

**Configuration parity, checked against a live legacy panel.** The Server
Config editor gained the tab the port was missing, and the panel-wide settings
that until now could only be changed with SQL got a screen. The defaults behind
them were taken from ISPConfig's own installer template rather than invented,
so a fresh install generates the same prefixed names the PHP panel does.

### Added

- **System → Main Config** — the panel-wide `sys_ini` INI editor
  (`system_config_edit.php`), three tabs (Sites, Mail, Misc) rendering only the
  keys this port actually reads. A save merges its section back into the full
  document, so keys the panel does not render survive the round trip, and the
  change is journalled like the legacy `datalogUpdate('sys_ini', …)`
- **Server tab in System → Server Config** — the per-server tab the generated
  form was missing: `server` section keys (`ip_address`, `ssh_port`, timezone,
  auto-update and monitoring settings), decoded through a typed
  `getconf.ServerSection` instead of raw map indexing

### Fixed

- **Generated SELECT fields stored their labels, not their keys** — the
  form generator read PHP `value` maps as lists, so `loglevel` saved `Warnings`
  where the daemon expects `1`, and `backup_time` offered 96 options for 4
  real values. The rule is the field's formtype: CHECKBOX is a list, everything
  else is a key/label map. Two tests pin it
- **Password and name prefixes were empty on a fresh install** — the defaults
  came from `system_config.tform.php`, which ships blanks and
  `min_password_length=5`. They now come from `install/tpl/system.ini.master`
  byte for byte (`c[CLIENTID]`, `[CLIENTNAME]`, `8`, `3`) and are seeded, so a
  database user created as `shopuser` is stored as `c1shopuser` as it is in
  ISPConfig
- **`sys_ini` was only filled on a fresh database** — `Seed()` runs on install,
  so every existing install upgraded into an empty panel configuration.
  `migrate` now fills it on both paths
- **A from-zero install aborted in the MariaDB step** — it wrote
  `mysql_clientdb.conf` into `/etc/go-ispconfig` before the config step created
  that directory. Every fresh install on a clean host hit this

### Documentation

- `add-multiserver-mgmt` re-scoped against the shipped tree, with tasks
- `refine-system-config-parity` proposal covering the gaps found against the
  legacy panel at 192.168.56.20
- `acme-as-go` proposal and decision record — issue certificates natively over
  `golang.org/x/crypto/acme`, already a direct dependency, rather than porting
  acme.sh or adding `go-acme/lego` for a DNS provider catalogue this panel does
  not need (it *is* the DNS server), with the legacy's actual ACME behaviour
  documented
- `docs/multi-server.md` — what to install on each node (the `--web`/`--dns`
  answers gate the web and DNS steps, the row's `mail_server` flag gates the
  mail steps), per-role package tables, and how to run a real BIND secondary
  across two DNS nodes
- brand gopher icons regenerated from the 1024px source, transparency preserved

## [0.4.0] — 2026-08-04

**The System module, and an API you can automate against.** Two System entries
that pointed at a placeholder are now real screens — Server Config and CP Users
— and the REST API gained a machine credential, so a script no longer has to
store a panel admin's password.

### Added

- **System → Server Config** — per-server `server.config` INI editor, one tab
  per section (web, dns, mail, getmail, jailkit), 125 fields generated from the
  legacy `server_config.tform.php` and narrowed to the keys `internal/getconf`
  actually decodes. Saving PUTs only the sections that changed, so one edit
  produces one `sys_datalog` row
- **System → CP Users** — panel login accounts in `sys_user`, with the rules
  that live in the legacy `users_edit.php` rather than its form definition:
  creates are admins only, `admin_allow_new_admin` gates promotion, a
  client-owned login can never become admin, and a rename or password change
  propagates to the `client` row and the client's `sys_group` inside the same
  transaction
- **API tokens** (`System → Remote Users`) — `goisp_<id>_<secret>` presented on
  the same `Authorization: Bearer` header the panel uses, stored only as a
  SHA-256 digest, shown once, scoped per resource, optionally IP-restricted and
  expiring, revocable in one row update, with `last_used_at`. Scopes intersect
  with the owner's own permissions, so a token can only ever do less than its
  owner
- **JWT exchange** (`POST /api/tokens/exchange`) — a short-lived HS256 token
  for callers that want a self-expiring credential, clamped to one hour and
  never beyond the issuing token's own expiry
- `go-ispconfig token create|list|revoke` — mint a credential on an unattended
  install, or revoke one without reaching the panel
- **Themed confirm dialog and toasts** — the eighteen `window.confirm` call
  sites now use a `<dialog>`-based component that follows the theme in both
  light and dark, with a red confirm button for destructive actions
- `[auth] jwt_secret` / `jwt_ttl`, generated by both `install` and `init`

### Changed

- `docs/api-tokens.md`, `docs/server-config-module.md`,
  `docs/cp-users-module.md` and `docs/multi-server.md` document the new
  surfaces, each recording what is deliberately **not** implemented and why
- `AGENTS.md` carries a standing rule: every API addition or change updates the
  Swagger spec in the same commit
- README leads with the project mascot, a four-shot gallery of the panel and an
  automation example

### Fixed

- **Delete buttons were missing from every list** — a Boolean prop with no
  value passed is `false` in Vue, so the `deletable !== false` guard hid the
  button everywhere, System → Server IP addresses included. The flag is now the
  opt-out `noDelete`
- `ARCHITECTURE.md` claimed the daemon refuses to start on multi-server
  databases via a `GuardServer` that does not exist; `engine.ResolveServer` has
  supported multi-server for a while, and `docs/multi-server.md` now documents
  the setup and its remaining gaps
- `config.toml.example` did not document `[auth] jwt_secret`/`jwt_ttl` or the
  whole `[mail]` SMTP section — settings `go-ispconfig init` users could not
  discover without reading the Go source. A test now asserts every default key
  appears in the example
- Four upstream `server_config.tform.php` SELECT defaults that are not one of
  their own options (`maildir_format => '20'` and friends) are replaced by the
  `getconf` default

## [0.3.0] — 2026-08-04

**A complete mail server, provisioned by the installer.** `go-ispconfig install`
now brings up Postfix and Dovecot on `mail_server` nodes, so a fresh host goes
from bare Debian/Ubuntu to accepting and delivering mail in one command. The
data layer is complete too: all 78 ISPConfig3 tables are mapped to GORM structs.

### Added

- **Postfix provisioning** (`postfix` install step) — converges via
  `postconf -e/-M/-P` instead of rewriting `main.cf`, so hand edits survive:
  virtual maps against `dbispconfig`, submission (587) and submissions (465),
  Dovecot LMTP and SASL auth sockets inside the Postfix chroot
- **Dovecot provisioning** (`dovecot` install step) — IMAP/POP3/LMTP with
  runtime dialect detection: Dovecot 2.3 and 2.4 have incompatible config
  syntax, and the correct one is rendered per host. SQL auth pulls uid/gid from
  `mail_user`, and auth is verified at the end of the step
- `vmail` install step — creates the `vmail` system user and group before any
  maildir is written
- `[log]` config section with a `level` key, plus a `LOG_LEVEL` environment
  override that needs no rebuild
- `[swagger]` config section — `disabled`, `public` and `path` knobs for the
  Swagger route
- `GET /healthz` — dependency-free liveness probe for load balancers
- `GET /api/health?full=1` — probes database, task queue, TLS certificate expiry
  and the daemon datalog backlog, reporting `degraded` with `200` for everything
  short of an unreachable database
- Extended technical documentation in `docs/README.md`

### Changed

- All **78 ISPConfig3 tables** are now covered by GORM structs with 1:1 schema
  parity, including the natural primary keys of `sys_cron` and `remote_session`
- Top-level `README.md` rewritten for hosting operators rather than developers
- Mail install steps share a `mailRole()` gate, so they run only where the
  server row has `mail_server = 1`

### Fixed

- **Mail Domain form parity** with the PHP panel: client select, spamfilter
  policy lookup, Save/Cancel buttons, collapsible DKIM fieldset, field order
  and sidebar labels all reconciled against `mail_domain_edit.htm`
- DKIM DNS-Record is now byte-exact with the legacy form, and appears
  immediately after a key is generated
- Per-domain relay fields are hidden unless the legacy gates
  (`show_per_domain_relay_options`, `limit_relayhost`) allow them
- Maildirs are no longer created root-owned
- The panel can reach the root-only fail2ban socket again
- Integration tests probe MariaDB readiness over TCP rather than the unix socket

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
