# Proposal: add-dns-powerdns-module

## Why

ISPConfig3 supports PowerDNS as an alternative authoritative DNS backend (`server/plugins-available/powerdns_plugin.inc.php`, schema in `install/sql/powerdns.sql`). Bind covers the initial go-ispconfig release, but operators running PowerDNS (native SQL zones, API-driven, better fit for large record counts and database replication) need the same first-class support to migrate from ISPConfig without changing their DNS stack.

## What Changes

- New PowerDNS plugin for the go-ispconfig daemon, subscribed to the same `dns_soa_*` / `dns_rr_*` / `dns_slave_*` events already announced by the DNS module from `add-dns-bind-module` — the panel UI, REST API and `dns_*` tables are shared; only the applying plugin differs.
- Instead of rendering zone files, the plugin writes zones/records into the PowerDNS SQL schema (embedded `powerdns.sql`, generic MySQL backend `gmysql`) — port of `powerdns_plugin.inc.php` (zone sync, record mapping, SOA serial handling, TTL/priority mapping, slave zones as PowerDNS `SLAVE` type).
- Per-server backend selection: the `server.config` DNS section chooses `bind` or `powerdns` (mirrors ISPConfig behavior); exactly one DNS plugin is active per server.
- Installer support: optional PowerDNS package installation and `powerdns` database creation on Debian 11–13 / Ubuntu 22.04–24.04 (extends `add-installer-cli` distro profiles).
- Service management: pdns reload/restart via the delayed services registry; config validation before activation.
- DNSSEC via PowerDNS native (`pdnsutil secure-zone`) instead of manual key management — replaces the Bind-style signing flow for this backend. Note: DNSSEC diverges between backends (Bind: `dnssec-keygen`/`dnssec-signzone` flow; PowerDNS: `pdnsutil`) — the future design defines a `dnssec-backend` abstraction with per-backend flags, keeping a single UI/API surface across backends.

Reference PHP sources: `server/plugins-available/powerdns_plugin.inc.php`, `install/sql/powerdns.sql`, `server/mods-available/dns_module.inc.php` (events), `install/lib/installer_base.lib.php::configure_powerdns`.

## Capabilities

### New Capabilities

- `powerdns-backend`: PowerDNS plugin — SQL zone/record sync from `dns_soa`/`dns_rr`, slave zones, serial handling, pdnsutil-based DNSSEC, service reload, backend selection per server.

### Modified Capabilities

- `installer-cli`: optional PowerDNS install step (packages, `powerdns` DB from embedded `powerdns.sql`, gmysql config) selected via answers/flags.

## Impact

- Depends on: `port-ispconfig3-to-go` (foundation) and `add-dns-bind-module` (DNS module events, UI, API — all reused unchanged).
- New code: `internal/plugins/powerdns/`; embedded `powerdns.sql`; installer step.
- No new panel UI: the DNS module screens work identically regardless of backend.
- Tests: integration against a real PowerDNS + MariaDB (docker), event-to-SQL golden tests, `dig` smoke checks in the Vagrant rig (opt-in machine or toggle).

## Non-goals

- PowerDNS recursor (authoritative only).
- PowerDNS HTTP API as the sync mechanism (SQL backend like ISPConfig; API may be evaluated later).
- Mixed backends on the same server (one DNS backend per server).
- MyDNS (`configure_mydns` is legacy and stays unported).
