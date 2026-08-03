# Proposal: add-installer-cli

## Why

go-ispconfig currently assumes a hand-prepared host (packages installed, MariaDB configured, config.toml written by hand). ISPConfig3 ships a full installer (`install/install.php` + `install/lib/installer_base.lib.php`, ~66 functions, with per-distro path/package tables in `install/dist/conf/*.conf.php` and config templates in `install/tpl/*.master`). Without a Go equivalent, nobody can stand up go-ispconfig on a clean Debian/Ubuntu host in one command — and there is no reproducible way to test that an install actually works.

## What Changes

- New Cobra subcommand **`go-ispconfig install`** — native Go port of the `install/` layer for the nginx+bind scope. Interactive (question/answer, port of `simple_query`/`free_query`) and non-interactive (`--yes` plus flags and/or an answers file) modes.
- **Distro detection** via `/etc/os-release`: Debian 11/12/13 and Ubuntu 22.04/24.04, each mapped to a static distro profile (paths, package names, service names, PHP-FPM version) — the Go port of `install/dist/conf/debian110|120|130.conf.php` and `ubuntu2204|2404.conf.php`. Unsupported distros abort with a clear error.
- **Install steps** (each idempotent, re-runnable):
  1. Install OS packages via apt (nginx, bind9, mariadb-server, plus `redis-server` or `valkey` per the distro profile — backing for the asynq task queue, foundation D12; php-fpm optional).
  2. Configure MariaDB: create `dbispconfig` using the **embedded original ISPConfig3 DDL** (reuses the foundation `migrate` machinery — the installer does not carry a second schema), create the `ispconfig` DB user with a generated password (port of `configure_database`).
  3. Detect host IPs and populate `server_ip` (port of `detect_ips`/`get_host_ips`).
  4. Create the `server` row with `web_server=1`, `dns_server=1` and default `server.config` INI (port of `add_database_server_record`).
  5. Write `/etc/go-ispconfig/config.toml`.
  6. Install and enable systemd units `go-ispconfig-serve` and `go-ispconfig-daemon` (embedded unit files). **No crontab entries** — the daemon's internal scheduler replaces all ISPConfig cron jobs (foundation decision D1b; `install_crontab` is deliberately not ported).
  7. Render base nginx and bind configs from embedded templates (subset of `install/tpl/`: `named.conf.options.master`, snippet/include dirs for the web module) — port of `configure_nginx`/`configure_bind`. **No nginx vhost is created for the panel**: the `serve` binary terminates TLS directly (see step 8).
  8. Generate a self-signed TLS certificate for the panel (port of `make_ispconfig_ssl_cert`, self-signed path only) into `/etc/go-ispconfig/ssl/`; the `go-ispconfig serve` binary terminates TLS itself with this cert — there is no nginx proxy vhost in front of the panel.
  9. Optional **`install-acme`** step: installs acme.sh (or certbot, when chosen) so the web module can issue Let's Encrypt certificates for hosted **sites**; the panel certificate remains self-signed regardless.
  10. Create the admin user and print the generated credentials **once**, at the end. With `--write-credentials` the summary is additionally written root-only to `/root/.go-ispconfig-credentials`; the default is print-only.
- **Update mode**: `go-ispconfig install --update` re-renders base configs and unit files, preserving the database, config.toml values and issued credentials (port of the `update.php` reconfigure-services path, minimal scope).
- **Uninstall**: `go-ispconfig uninstall` removes units, rendered configs and optionally (`--purge-db`) the database — port of `install/uninstall.php` scope.
- **Safety rules**: any existing config file the installer overwrites is backed up first (`*.bak-<timestamp>`); every step logs what it does; rendered nginx/bind configs are validated (`nginx -t`, `named-checkconf`) before services are reloaded.
- **Vagrant test rig**: `vagrant/` directory with a Vagrantfile (Ubuntu 24.04 box, e.g. `bento/ubuntu-24.04`; optional Debian 12 box), provisioning that copies the locally built binary and runs `go-ispconfig install --yes`, plus a smoke-test script (systemd units active, panel HTTPS response, site + DNS zone creation via REST API, `nginx -t`, `named-checkzone`). Makefile targets `vagrant-up`, `vagrant-test`, `vagrant-destroy`. All machines on a host-only private network `192.168.56.0/24` with fixed IPs.
- **Legacy comparison VM + parity validation**: opt-in `legacy` machine provisioned with the original PHP ISPConfig3 (nginx+bind, via the official ISPConfig auto-installer, using the existing ISPConfig auto-install scripts as step-sequence reference) at a fixed 192.168.56.x IP. An agent-browser E2E suite creates test clients, sites, DNS zones and email accounts on the legacy panel and the equivalent clients/sites/DNS zones on go-ispconfig, asserting parity (records on the shared schema, generated configs, served vhosts) and reporting divergences. Parity scope is limited to clients + sites + DNS while there is no mail module: the legacy email accounts serve only as a future baseline (and as migration-source data) and are **not** compared. The legacy VM doubles as the live migration source for `add-legacy-migration` testing. Vagrant boxes/images and test data are never committed.

Reference PHP sources: `install/install.php`, `install/lib/installer_base.lib.php`, `install/lib/install.lib.php`, `install/dist/conf/{debian110,debian120,debian130,ubuntu2204,ubuntu2404}.conf.php`, `install/tpl/*.master` (nginx/bind/mysql subset), `install/uninstall.php`, `install/update.php`. Step-sequence reference (not architecture): the existing ISPConfig auto-install scripts (community one-command installers for Debian/Ubuntu) — unlike those scripts, the Go installer embeds everything in the binary, no external payload extraction.

## Capabilities

### New Capabilities

- `installer-cli`: the `install`/`uninstall` commands — distro detection, interactive/non-interactive flows, package installation, database/server-record/config.toml/systemd/nginx/bind/TLS setup steps, update mode, idempotency and backup rules.
- `install-test-rig`: Vagrant-based E2E install testing — Vagrantfile, provisioning, smoke tests, Makefile targets, documented Debian 12 run.

### Modified Capabilities

(none — `core-cli` gains new subcommands but its existing requirements are unchanged; the new commands are specified in `installer-cli`.)

## Impact

- New code: `cmd/install.go`, `cmd/uninstall.go`, `internal/installer/` (steps, distro profiles, prompts), embedded assets (systemd units, nginx/bind base templates).
- Reuses: foundation `migrate` (embedded `ispconfig3.sql`), `internal/config` (config.toml writer), `.master` template renderer.
- New repo dirs: `vagrant/` (Vagrantfile, scripts), Makefile targets.
- Dependencies: no new Go libraries required (stdlib `os/exec` for apt/systemctl/openssl-free crypto via `crypto/x509` self-signed cert); Vagrant + VirtualBox/libvirt on developer machines only.
- Requires root when run for real; refuses to run as non-root except `--dry-run`.

## Non-goals

- Mail stack (postfix/dovecot/rspamd), FTP, jailkit, firewall, monitoring — not installed or configured. Note: PureFTPd/jailkit (from `add-ftp-shell-module`, now implemented for plugins/API/UI — see `docs/ftp-shell-module.md`) and UFW (from `add-firewall-module`) installer steps (`configure_pureftpd`, `configure_jailkit`, `pureftpd_mysql.conf.master`) remain a **Modified Capability** of this installer (`installer-cli`) — still out of scope for the archived installer change; tracked in `docs/install.md` and `docs/ROADMAP.md`.
- Multi-server / expert mode (master/slave DB grants, `grant_master_database_rights`) — single server only.
- CentOS/RHEL/Fedora/openSUSE/Gentoo support; Apache2 support; PowerDNS.
- Migration of an existing PHP ISPConfig3 host in place (covered by `add-legacy-migration`); the installer targets clean hosts and aborts if it detects an existing ISPConfig3 PHP install.
- Let's Encrypt certificate for the panel (the panel stays self-signed; the optional `install-acme` step only installs the acme client used by the web module for **site** certificates).
