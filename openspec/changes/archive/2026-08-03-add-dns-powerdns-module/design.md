# Design: DNS/PowerDNS module

## Context

ISPConfig3's DNS stack already has a panel/API surface and `sys_datalog` events for zones and records. The applying backend is pluggable:

1. `interface/web/dns/` + `remote.d/dns.inc.php` — write `dns_soa`, `dns_rr`, `dns_slave`, `dns_template` with `{old,new}` datalog diffs (already ported by `add-dns-bind-module`).
2. `server/mods-available/dns_module.inc.php` — table hooks → nine named events; registers both `bind` and `powerdns` services.
3. `server/plugins-available/powerdns_plugin.inc.php` (~710 lines) — on those events, syncs zones/records into a separate **PowerDNS** MariaDB schema (`powerdns.domains` / `powerdns.records` from `install/sql/powerdns.sql`), drives `pdns_control` / `pdnsutil` (or legacy `pdnssec`), and manages DNSSEC via native PowerDNS commands rather than Bind key files.
4. `install/lib/installer_base.lib.php::configure_powerdns` — creates the `powerdns` database, grants the ISPConfig DB user, loads `powerdns.sql`, writes `pdns.local` gmysql config from `install/tpl/pdns.local.master`.

go-ispconfig already ships the shared half: GORM models for `dns_*`, REST API, Vue DNS UI, datalog → events (`internal/dns` module), and the Bind plugin. This change adds only the PowerDNS applying path, backend selection, service wiring, and installer support.

**Immutable ISPConfig schema (panel DB):** `dns_soa`, `dns_rr`, `dns_slave`, `dns_template` — never alter columns. The plugin may update daemon-managed DNSSEC fields already present on `dns_soa` (`dnssec_info`, `dnssec_initialized`, `dnssec_last_signed`, `dnssec_wanted`, `dnssec_algo`).

**Separate PowerDNS schema (not in `ispconfig3.sql`):** embedded `powerdns.sql` tables `domains`, `records`, `supermasters`, `domainmetadata` in database `powerdns`, with ISPConfig bridge columns `ispconfig_id` on `domains` and `records`.

## Goals / Non-Goals

**Goals:**
- Behavior-faithful port of `powerdns_plugin.inc.php` + `restartPowerDNS` + `configure_powerdns`.
- Same nine events, same panel/API/tables as Bind; only the daemon plugin and service differ.
- Per-server backend selection (`bind` | `powerdns`); exactly one DNS applying plugin active per server.
- DNSSEC via PowerDNS (`pdnsutil` / `pdnssec`) with `dns_soa.dnssec_*` panel fields kept current.
- Optional installer path for packages + `powerdns` DB + gmysql config on Debian 11–13 / Ubuntu 22.04–24.04.

**Non-Goals:**
- PowerDNS recursor; PowerDNS HTTP API as the sync mechanism (SQL/gmysql only, like ISPConfig).
- Mixed backends on the same server; MyDNS.
- New panel screens or REST endpoints for DNS entities (reuse `add-dns-bind-module` surface unchanged).
- Schema changes to `ispconfig3.sql` of any kind.

## Decisions

### D1 — Separate package, shared events
Put the PowerDNS plugin in `internal/powerdns` (Go package name; proposal's `internal/plugins/powerdns` path is the same idea, aligned with `internal/nginx` / `internal/dns` layout). It only implements `engine.Plugin`: subscribe to the nine events already announced by `internal/dns.Module`. Do **not** re-register table hooks.

The existing `dns` module keeps raising events; daemon bootstrap loads either the bind plugin or the PowerDNS plugin (D2), not both. Rationale: preserves the foundation registry contract and the Bind module's announced-events design that explicitly left a PowerDNS plugin slot open.

### D2 — Per-server backend selection via `server.config` `[dns]` section
Add getconf key `dns_backend` under the existing `[dns]` INI section of `server.config`:

| Value | Daemon loads |
|---|---|
| `bind` (default, empty, or missing) | `dns.Plugin` (Bind) + `bind` service |
| `powerdns` | `powerdns.Plugin` + `powerdns` service |

Exactly one applying plugin per server. The panel UI does not choose the backend per zone — operators set it once per server (mirrors ISPConfig's install-time "which DNS software is installed" exclusivity). `config.toml` may still gate the whole dns module; backend selection is server-row config, not a second enable flag.

*Alternative*: detect packages on disk at daemon start — rejected: non-deterministic on dual-package hosts and diverges from an explicit server config that survives migrations.

### D3 — Second database handle for PowerDNS SQL
PowerDNS data lives in database `powerdns` (name fixed like PHP; comment in the plugin). The daemon opens a dedicated GORM connection (DSN built from the main MariaDB host/user/password + database `powerdns`, or an optional `config.toml` `[powerdns]` DSN override for non-default deployments). Panel `dns_*` rows stay on the ISPConfig DSN; the plugin never writes zone files.

GORM models (explicit `gorm:"column:..."` tags, no AutoMigrate shape changes):

**`powerdns.domains`** (`Domain`): `id`, `name`, `master`, `last_check`, `type` (`MASTER`|`SLAVE`), `notified_serial`, `account`, `ispconfig_id` (bridge to `dns_soa.id` or `dns_slave.id`).

**`powerdns.records`** (`Record`): `id`, `domain_id`, `name`, `type`, `content`, `ttl`, `prio`, `change_date`, `disabled`, `auth`, `ispconfig_id` (bridge to `dns_rr.id` for RRs; SOA row uses the zone's `dns_soa.id` as `ispconfig_id` like PHP).

`supermasters` / `domainmetadata` are created by DDL for PowerDNS itself; the plugin does not manage them except as side-effects of `pdnsutil` DNSSEC.

Embedded `//go:embed powerdns.sql` ships the schema for the installer and tests. Idempotent ensure step (CREATE DATABASE IF NOT EXISTS + apply DDL) is installer-owned; the daemon assumes the schema exists when `dns_backend=powerdns`.

### D4 — Event → SQL mapping (PHP parity)
Port the PHP handlers verbatim as pure mapping + GORM writes:

**`dns_soa_insert`** (active `Y` only): strip trailing dot from `origin`; INSERT `domains` (`type=MASTER`, `notified_serial=dns_soa.serial`, `ispconfig_id=dns_soa.id`); INSERT SOA `records` with `content = "<ns> <hostmaster> <serial> <refresh> <retry> <expire> <minimum>"` where:
- `ns` = absolute (strip trailing `.`) or `ns + "." + origin` when relative; empty → origin
- `hostmaster` = `mbox` without trailing `.`
- `ttl` from `dns_soa.ttl`, `prio=0`, `change_date=UNIX_TIMESTAMP()`, `ispconfig_id=dns_soa.id`

Then: `pdns_control rediscover` → DNSSEC handle → `pdnsutil rectify-zone` → `pdns_control notify <origin>`.

**`dns_soa_update`**: if new inactive and old was active → delete path; if both inactive → no-op; if old active and PowerDNS domain exists → UPDATE the SOA `records` row (`ispconfig_id` + `type='SOA'`); else treat as insert and re-insert all active `dns_rr` for the zone. Then DNSSEC + rectify + notify.

**`dns_soa_delete`**: find `domains` where `ispconfig_id=old.id AND type='MASTER'`; DELETE all `records` for that `domain_id`; DELETE the domain row. (DNSSEC materials are PowerDNS-internal; also clear via disable path when wanted.)

**`dns_rr_insert`**: active `Y` only; skip if a PowerDNS record with same `ispconfig_id` already exists; skip if parent PowerDNS MASTER domain is missing (SOA not active yet — records land when SOA activates). Name/content rules:
- `name`: trailing `.` → absolute (strip); empty → origin; else `name + "." + origin`
- `content` for CNAME/MX/NS/ALIAS/PTR/SRV: same absolute/relative rule as name; HINFO: PHP quote/space-to-underscore transform; else raw `data`
- `prio` ← `dns_rr.aux`, `ttl` ← `dns_rr.ttl`, `type` ← `dns_rr.type`, `ispconfig_id` ← `dns_rr.id`
- Then rectify-zone (resolve origin from parent domain when RR payload has no origin)

**`dns_rr_update`**: inactive transition → delete; else update non-SOA row by `ispconfig_id` or insert if missing.

**`dns_rr_delete`**: DELETE `records` WHERE `ispconfig_id=old.id AND type != 'SOA'`.

**`dns_slave_insert`** (active `Y`): INSERT `domains` (`type=SLAVE`, `master=dns_slave.ns`, `name=origin without trailing dot`, `ispconfig_id=dns_slave.id`); `pdns_control retrieve <origin>`.

**`dns_slave_update`**: inactive → delete; active update → UPDATE domain name/master; DELETE `records` WHERE `domain_id=? AND ispconfig_id=0` (AXFR'd cache); retrieve again; inactive→active → insert path.

**`dns_slave_delete`**: DELETE records + domain for `ispconfig_id` + `type=SLAVE`.

Serial policy stays API-side (already ported): the daemon only copies `dns_soa.serial` into the PowerDNS SOA content / `notified_serial` on insert. No zone-file regeneration.

### D5 — Control plane: `pdns_control` / `pdnsutil` (not HTTP API)
Port tool discovery and commands:

| PHP helper | Behavior |
|---|---|
| `find_pdns_control` | `type -p pdns_control` |
| `find_pdns_pdnssec_or_pdnsutil` | prefer `pdnssec` (v3), else `pdnsutil` (v4+) |
| `get_pdns_version` / `is_pdns_version_supported` | major version starts with 3 or 4 → DNSSEC/rectify enabled; else no-op with log |
| `zoneRediscover` | `pdns_control rediscover` |
| `notifySlave` | `pdns_control notify <origin>` |
| `fetchFromMaster` | `pdns_control retrieve <origin>` |
| `rectifyZone` | `pdnsutil|pdnssec rectify-zone <origin>` |

All shell-outs go through the foundation `CommandRunner` (test-stubbable). Failures are logged and recorded via the datalog error path where appropriate; they MUST NOT corrupt the PowerDNS SQL state already written.

### D6 — DNSSEC via PowerDNS native tools (diverges from Bind by design)
Port `handle_dnssec` / `soa_dnssec_*` decision tree (not Bind's `dnssec-keygen` / `dnssec-signzone`):

1. **Origin changed** and old zone was initialized → `disable-dnssec` on old origin; clear `dnssec_initialized` / refresh `dnssec_info` log.
2. **Wanted N after Y** → `pdnsutil disable-dnssec <zone>`; set `dns_soa.dnssec_initialized='N'` (keys/info retained in panel field until next create, PHP parity).
3. **Wanted Y after N/null** → create:
   - `add-zone-key <zone> ksk active 2048 rsasha256`
   - `add-zone-key <zone> zsk active 1024 rsasha256`
   - `set-nsec3 <zone> "1 0 10 deadbeef"`
   - `show-zone <zone>` → parse active KSK/CSK + DS lines (`format_dnssec_pubkeys`) into `dns_soa.dnssec_info`; set `dnssec_initialized='Y'`
4. Steady-state wanted: rectify on RR/SOA paths keeps NSEC3/auth bits coherent; no Bind-style periodic re-sign job (PowerDNS online signing). **Do not** register `dns_resign` for this backend.

UI/API surface for DNSSEC remains the existing zone fields (`dnssec_wanted`, `dnssec_info`, …). Future cross-backend `dnssec-backend` abstraction (proposal note) is out of scope; this change only implements the PowerDNS half.

`dnssec_algo` on `dns_soa` is Bind-oriented (ECDSAP256SHA256 / NSEC3RSASHA1). PowerDNS create uses fixed rsasha256 like PHP — ignore algo set for the PowerDNS path (document the divergence).

### D7 — `powerdns` service + AXFR allow-list rewrite
Register service key `powerdns` in the delayed services registry. Unit resolution (PHP parity): systemd unit `powerdns` if present, else `pdns`.

`restartPowerDNS` semantics (on delayed **restart** of that service):
1. Collect distinct non-empty `xfer` values from active `dns_soa` and `dns_slave` for this server.
2. Build `allow-axfr-ips=127.0.0.1[,…unique IPs…]` (always include localhost).
3. Atomically write `/etc/powerdns/pdns.d/pdns.ispconfig-axfr` (path overridable via getconf `powerdns_axfr_conf`).
4. `systemctl restart <unit>`.

Queue a delayed `restart` after SOA/slave mutations that can change `xfer` or domain presence (PHP only rewrote AXFR when the service restart ran — we make the request explicit from those handlers so the allow-list stays correct). Record-only changes rely on SQL + rectify/notify and do not force a full pdns restart.

### D8 — Installer extension (optional PowerDNS path)
Extend `add-installer-cli` answers/flags:

- `--dns-backend bind|powerdns` (default `bind`); interactive prompt when DNS is enabled.
- When `powerdns`: install packages `pdns-server` + `pdns-backend-mysql` (profile package lists for Debian/Ubuntu), skip bind9 packages (or leave installed but unused — prefer not installing bind when backend is powerdns).
- `configure_powerdns` port: CREATE DATABASE `powerdns`, GRANT ALL to the `ispconfig` DB user@localhost, apply embedded `powerdns.sql`, render `pdns.local` from embedded `pdns.local.master` into `/etc/powerdns/pdns.d/pdns.local` (mode 0600, root-owned) with placeholders `gmysql-host/user/password/dbname/port`.
- Seed `server.config` `[dns] dns_backend=powerdns` (and keep bind defaults unused).
- Enable/restart `pdns` unit; smoke `pdns_control version` when available.
- Vagrant/test: opt-in machine or toggle for PowerDNS path + `dig` smoke (proposal).

Validation before activation: if gmysql config is unreadable or DB unreachable, fail the step and do not mark DNS ready.

### D9 — No API/UI changes; riud and datalog stay on panel tables
All authorization, validators, serial bumps, template wizard, and Vue screens remain those from `add-dns-bind-module`. PowerDNS never sees `sys_perm_*` — isolation is enforced at the panel DB layer before datalog is written. The plugin runs as root/daemon with full SQL access to `powerdns` and only processes events for its `server_id`.

`dns_soa.rendered_zone` is Bind-specific cache; PowerDNS path leaves it untouched (UI "Zone rendering" tab stays empty/stale on PowerDNS hosts — acceptable parity gap; document it).

### D10 — Testing strategy
- **Unit**: pure name/content/SOA content mappers; DNSSEC pubkey formatter; AXFR allow-list builder; backend selection matrix.
- **Golden / event-to-SQL**: fixture `{old,new}` payloads → expected `domains`/`records` rows (or SQL statements) matching PHP behavior (absolute/relative names, HINFO transform, inactive transitions, slave `ispconfig_id=0` purge).
- **Integration**: MariaDB with both `dbispconfig` + `powerdns` schemas; inject datalog rows or call plugin handlers; assert SQL state; stub `pdns_control`/`pdnsutil` binaries.
- **Optional docker**: real PowerDNS + gmysql + `dig` for SOA/A resolution after sync.
- **Vagrant**: opt-in PowerDNS install path smoke (packages, DB, one zone via API, dig).

## Risks / Trade-offs

- [Dual-DB drift / partial failure mid-event] → order writes like PHP (domain before records; delete records before domain); log and surface datalog errors; rectify/notify are best-effort after commit.
- [pdnsutil CLI differences across PowerDNS 4.x minor versions] → version gate major 3/4; integration-test on Ubuntu 24.04 package; stub parser tests for `show-zone` formats ("Active: 1" vs "Active (").
- [Operator installs both bind9 and pdns] → explicit `dns_backend` prevents double-apply; docs warn that only one is live.
- [AXFR allow-list is global (PHP limitation)] → same as ISPConfig: any IP in any zone's `xfer` may AXFR any master zone; documented.
- [ALIAS type] → PHP maps ALIAS like other name types into PowerDNS even though comments say MyDNS-only; keep mapping for fidelity; unsupported PowerDNS types surface as backend errors.
- [No `named-checkzone` equivalent before publish] → PowerDNS accepts SQL immediately; invalid records fail at query time. Mitigate with existing API validators; optional future `pdnsutil check-zone` not in PHP plugin scope.

## Migration Plan

- Code + installer only; no `ispconfig3.sql` change.
- Fresh install with `--dns-backend powerdns`: packages, `powerdns` DB, gmysql config, `dns_backend=powerdns`, daemon loads PowerDNS plugin.
- Fresh install default remains Bind.
- Migrated ISPConfig PHP hosts already on PowerDNS: reuse existing `powerdns` database (bridge via `ispconfig_id`); first datalog events after cutover re-sync rows (self-healing upserts by `ispconfig_id`). Document that dual-running PHP and Go daemons against the same PowerDNS DB is unsupported.
- Rollback: set `dns_backend=bind` (or disable dns module); PowerDNS SQL state remains until cleaned manually.

## Open Questions

- Should `config.toml` require an explicit PowerDNS DSN, or is same-host/`powerdns` database name always sufficient? (Default same-host; optional override.)
- Expose a read-only admin API to inspect PowerDNS `domains`/`records` for debugging? (Out of scope; use SQL/`pdnsutil` for now.)
- Align PowerDNS DNSSEC algorithm with `dns_soa.dnssec_algo` in a later change, or keep fixed rsasha256 forever for PHP parity?
