# Design: add-installer-cli

## Context

ISPConfig3's installer is a PHP wizard (`install/install.php`) driving `installer_base.lib.php`: detect distro → ask questions → install/configure services → create DB → write configs from `install/tpl/*.master` → install crontabs. Distro differences live in data files (`install/dist/conf/*.conf.php`) plus per-distro method overrides (`install/dist/lib/*.lib.php`).

go-ispconfig already has (foundation change): a single binary, `migrate` with the embedded original `ispconfig3.sql`, config.toml via Viper, the `.master` template renderer, and a daemon whose internal scheduler replaces every ISPConfig cron. The installer is a thin orchestration layer on top of those pieces plus apt/systemctl/filesystem operations. Scope: web (nginx) + dns (bind) on Debian 11/12/13 and Ubuntu 22.04/24.04, single server.

## Goals / Non-Goals

**Goals:**
- One command from clean host to running panel: `go-ispconfig install --yes`.
- Deterministic, idempotent, logged steps; safe re-run after a mid-install failure.
- Data-driven distro support (adding a distro = adding a profile entry, not code).
- A reproducible E2E proof that installation works: Vagrant rig + smoke tests wired into the Makefile.

**Non-Goals:**
- Mail/FTP/firewall/jailkit stacks, Apache, PowerDNS, multiserver, CentOS-family distros.
- In-place takeover of an existing PHP ISPConfig3 host (separate change `add-legacy-migration`).
- Let's Encrypt for the panel cert (self-signed only).

## Decisions

### D1 — Step pipeline with idempotent steps, not a monolithic script
The installer is an ordered list of `Step` values (`Name() string`, `Run(*State) error`), executed sequentially with progress logging: detect-distro → preflight → packages → mariadb → schema (delegates to `migrate`) → server-record+ips → config.toml → tls-cert → nginx-base → bind-base → install-acme (optional) → systemd-units → admin-seed → summary. Each step checks current state first and skips or converges (e.g. package already installed, DB already exists with our schema, unit file identical). Re-running `install` on a half-installed host completes the missing steps.
*Alternative considered*: transactional install with rollback on failure — rejected: apt/DB side effects are not practically reversible; converging re-run is simpler and matches ISPConfig behavior.

### D2 — Distro profiles as static Go data (port of dist/conf/*.conf.php)
A `Profile` struct holds package lists, service names, paths (`named.conf.*`, zonefiles dir, nginx conf dirs, php-fpm version/paths, init/service names) keyed by distro id (`debian11|debian12|debian13|ubuntu22.04|ubuntu24.04`). Detection reads `/etc/os-release` (`ID` + `VERSION_ID`). The five supported profiles differ only in PHP-FPM version and a few paths (verified against `debian120.conf.php` vs `ubuntu2404.conf.php` — the diff is essentially the PHP version 8.2 vs 8.3), so profiles are one base + small per-distro overrides. No per-distro code overrides (the PHP `dist/lib/*.lib.php` layer collapses into data).
*Alternative considered*: embed the PHP conf files and parse them — rejected: they are PHP code, and only a small subset applies to the nginx+bind scope.

### D3 — Answers model: flags > answers file > interactive prompt > default
One `Answers` struct (hostname, DB root access, DB name/user, panel port, enable web, enable dns, install php-fpm, admin email). Population order: CLI flags, then `--answers file.toml`, then interactive prompts (port of `simple_query`/`free_query`), then defaults. `--yes` suppresses prompts entirely (defaults + provided values must be sufficient; missing required answers abort with the flag name). This gives the CI/Vagrant path (`install --yes`) and the human path (guided) with one code path.
*Alternative considered*: separate `install` and `install --unattended` flows — rejected: two flows drift apart.

### D4 — Schema creation delegates to the existing `migrate` machinery
The installer never carries DDL. It creates the empty `dbispconfig` database and the `ispconfig` MariaDB user (random password, written into config.toml), then calls the same code path as `go-ispconfig migrate` (embedded original `ispconfig3.sql`, D9 of the foundation). Existing-schema detection (`server.dbversion`) also comes for free, making the schema step idempotent.

### D5 — Root MariaDB access via unix_socket, no root DB password prompt by default
Debian/Ubuntu MariaDB ships root@localhost with `unix_socket` auth; the installer (running as root) connects over the socket without a password — no `mysql` CLI dependency, plain `database/sql`. A `--db-root-password` flag covers hosts where socket auth was disabled. This removes ISPConfig's most error-prone question.

### D6 — Config files: embedded templates, backup before overwrite, validate before reload
Base nginx (snippet/include dirs for the web module — **no panel vhost**, see D8) and bind (`named.conf.options`, local include) files are rendered from templates embedded in the binary via the foundation `.master` renderer — the bind/nginx-relevant subset of `install/tpl/` (`named.conf.options.master`). Before writing over an existing file the installer copies it to `<file>.bak-<unix-ts>` (only when content differs — identical content = no-op, no backup churn). After writing, `nginx -t` / `named-checkconf` must pass before `systemctl reload`; on validation failure the original file is restored and the step fails loudly.

### D7 — systemd units embedded; no crontab (foundation D1b)
`go-ispconfig-serve.service` and `go-ispconfig-daemon.service` are embedded assets written to `/etc/systemd/system/`, then `daemon-reload` + `enable --now`. `install_crontab` from PHP is intentionally not ported: the daemon's internal scheduler owns all periodic work. The installer MUST NOT touch any crontab.

### D8 — Panel TLS: self-signed via crypto/x509
Self-signed cert (RSA 4096 or ECDSA P-256, 10-year, CN + SAN = hostname) generated with Go stdlib `crypto/x509` into `/etc/go-ispconfig/ssl/` — port of the self-signed path of `make_ispconfig_ssl_cert` without shelling out to `openssl`. Regenerated only if missing or expired; `--update` keeps existing certs. Panel TLS topology: the `go-ispconfig serve` binary terminates TLS **directly** with this cert — the installer creates no nginx vhost or proxy for the panel; nginx serves only hosted sites.

### D8b — Optional acme client install for site certificates
An optional `install-acme` step (answer/flag, default off) installs acme.sh (or certbot when chosen) so the web module (`add-web-nginx-module`) can issue Let's Encrypt certificates for hosted **sites** — the web plugin itself only detects and uses an existing client, never installs one. The panel certificate remains self-signed (D8) regardless of this step.

### D9 — `--update` mode = re-render subset of the pipeline
`install --update` runs only: detect-distro → preflight → config-render steps (nginx-base, bind-base, systemd-units) with the backup rule of D6, then restarts units. It never touches the database, config.toml credentials, certs, or admin user. This is the minimal port of `update.php`'s "reconfigure services" path.

### D10 — `uninstall` as an explicit command with confirmations
`go-ispconfig uninstall` stops/disables/removes the two units, removes rendered go-ispconfig-owned nginx/bind config files and `/etc/go-ispconfig/` (config kept with `--keep-config`), and only drops the database/DB user with `--purge-db`. Packages are never removed (apt state belongs to the admin). Requires typed confirmation unless `--yes`.

### D11 — Vagrant rig: one Vagrantfile, binary built on host, smoke test as a shell script
`vagrant/Vagrantfile` defines machine `ubuntu` (`bento/ubuntu-24.04`, default) and optional `debian` (`bento/debian-12`, started explicitly). Provisioning: sync the host-built linux/amd64 binary (Makefile builds it first), run `go-ispconfig install --yes` with fixed answers, then run `vagrant/smoke-test.sh` inside the guest: units active, `curl -k` panel over HTTPS, REST API login + create site + create DNS zone, `nginx -t`, `named-checkzone` on the generated zone file. Exit code of the smoke test = test result, so `make vagrant-test` is CI-shaped. Second `install` run in the smoke test asserts idempotency (exit 0, services still active).
*Alternative considered*: Docker-based test — rejected: systemd + real services (bind, nginx, mariadb) need a full init system; a VM is the honest environment for an installer.

## Risks / Trade-offs

- [apt output/locks make the packages step flaky (unattended-upgrades holding dpkg lock)] → wait-loop on the dpkg lock with timeout; `DEBIAN_FRONTEND=noninteractive`; `-o Dpkg::Options::=--force-confold`.
- [Half-failed install leaves services misconfigured] → idempotent converging steps (D1) + validate-before-reload (D6); re-run is the documented recovery.
- [Distro profile paths drift across releases (e.g. Debian 13 PHP version)] → profile data covered by a unit test per distro id; Vagrant rig catches real-host drift for Ubuntu 24.04/Debian 12.
- [Printing admin credentials once means losing them is permanent] → documented reset path (`go-ispconfig` admin password reset lives in foundation seed logic); opt-in `--write-credentials` additionally writes the summary root-only to `/root/.go-ispconfig-credentials` with a warning to delete it — default is print-only, nothing persisted.
- [Vagrant not available in CI runners] → `vagrant-test` documented as a developer/E2E target, not a required CI gate; unit tests cover distro detection, answers resolution and template rendering without a VM.

## Migration Plan

Greenfield feature — no data migration. Deploy = ship the new subcommands in the next release. Rollback = don't use the commands; `uninstall` provides clean removal for hosts installed with it.

## Open Questions

- Debian 13 (trixie) default PHP version for the optional php-fpm package — confirm at implementation time (profile data change only).
- Should `install --update` also re-run `migrate` for incremental schema updates? Leaning yes once incremental DDL exists; out of scope while the schema is static.
