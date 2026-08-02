# Proposal: add-dns-bind-module

## Why

The foundation change (`port-ispconfig3-to-go`) delivers the datalog engine, registries, `.master` template renderer, riud permissions, REST API core and panel skeleton — but no DNS functionality. This change ports the ISPConfig3 DNS/Bind module so go-ispconfig can manage authoritative BIND name servers end to end: zones, records, secondary zones, zone templates, DNSSEC and the panel UI/API that drive them.

Reference PHP sources being ported (read-only under `base/ispconfig3_install/`):
- `server/mods-available/dns_module.inc.php` — table hooks → named events, bind service registration
- `server/plugins-available/bind_plugin.inc.php` — zonefile/named.conf generation, DNSSEC, validation/rollback
- `server/conf/bind_pri.domain.master`, `bind_named.conf.local.master`, `bind_named.conf.local.slave` — templates (reused verbatim)
- `interface/web/dns/form/dns_soa.tform.php` + per-type `dns_<type>.tform.php` (A, AAAA, MX, TXT, CNAME, SRV, CAA, NS, PTR, …) — field/validator definitions
- `interface/web/dns/dns_wizard.php` + `interface/lib/classes/dns_wizard.inc.php` — zone wizard from `dns_template`
- `interface/lib/classes/remote.d/dns.inc.php` — remote API surface (`dns_zone_*`, `dns_rr_*`, per-type CRUD, `increase_serial`)

## What Changes

- **dns module (daemon side)**: Go `Module` registering table hooks for `dns_soa`, `dns_slave`, `dns_rr` and raising the nine named events (`dns_soa_insert/update/delete`, `dns_slave_*`, `dns_rr_*`); registers the `bind` service (restart/reload via systemd `bind9`/`named`).
- **bind plugin (daemon side)**: Go `Plugin` subscribing to those events:
  - Renders the whole zone file from `bind_pri.domain.master` on any SOA or RR change (RR events reload the parent SOA and regenerate the full zone, as the original does).
  - Record pre-processing: TTL `0` → empty, empty name → `@`, TXT data > 255 chars split into quoted 255-char chunks, CAA → `TYPE257` generic-hex encoding on BIND < 9.9.6 (including the implicit `issue ";"` companion record for issuewild-only zones).
  - Writes the zone file to `bind_zonefiles_dir/<masterprefix><origin>`, chowns to `bind_user`/`bind_group`, stores a copy in `dns_soa.rendered_zone`.
  - Validates with `named-checkzone`; on failure restores the previous zone file, quarantines the bad one as `.err` and reports via `datalogError`.
  - Rebuilds `named.conf.local` completely on every change: primary zones (`allow-transfer`/`also-notify`/`allow-update` from `xfer`/`also_notify`/`update_acl`, comma→semicolon) plus secondary zones from `dns_slave` (`masters`, `allow-transfer {none;}` default), from the two `.master` templates.
  - Delayed service action: `restart` when `update_acl` is set, otherwise `reload`; cleanup of old files on origin rename and zone delete.
- **DNSSEC** (phase 2 inside this change; DB columns already exist): key generation ZSK+KSK (`dnssec-keygen`, ECDSAP256SHA256 preferred, NSEC3RSASHA1 legacy), zone signing (`dnssec-signzone`, 16-day validity, `$INCLUDE` key files), DS/DNSKEY output stored in `dnssec_info`, periodic re-signing as a named job on the daemon's internal scheduler, key/signed-file cleanup on disable/delete.
- **SOA serial management**: ISPConfig-style `YYYYMMDDnn` serial bump on every zone/record mutation through the API (port of `increase_serial`).
- **Secondary zones**: `dns_slave` CRUD end to end (API, UI, named.conf, slave zonefile dir ownership).
- **Zone templates + wizard**: `dns_template` CRUD and a zone-creation wizard that expands `{DOMAIN}/{IP}/{IPV6}/{NS1}/{NS2}/{EMAIL}` placeholders in the `[ZONE]`/`[DNS_RECORDS]` template format into a SOA plus records in one transaction.
- **REST API**: port of `remote.d/dns.inc.php` surface — `dns_zone_add/update/delete/get/get_id/set_status/set_dnssec`, generic `dns_rr_*` per record type, `dns_rr_get_all_by_zone`, `dns_templatezone_get_all`, `dns_templatezone_add` — with swaggo annotations, riud permission scopes and datalog writes.
- **UI (Vue 3)**: DNS panel module — zone list, SOA form with tabs (Records / Zone settings / Zone rendering when enabled; `update_acl` admin-only), embedded record grid on the Records tab with per-type add/edit dialogs and per-type validation (A/AAAA/CNAME/MX/TXT/SRV/CAA/NS/PTR/ALIAS/DNAME/DS/HINFO/LOC/NAPTR/RP/SSHFP/TLSA/SPF/DKIM/DMARC), secondary zone list/form, zone wizard from templates, template admin.
- **Testing**: golden-file tests for zone file and `named.conf.local` rendering, unit tests for serial bump and TXT split/CAA encoding, integration tests against MariaDB for the datalog→event→file pipeline.

## Capabilities

### New Capabilities

- `dns-module-events`: daemon dns module — table hooks for dns_soa/dns_slave/dns_rr, named event dispatch, bind service registration with restart/reload.
- `bind-zone-generation`: zone file rendering from `bind_pri.domain.master` with record pre-processing, ownership, `rendered_zone` cache, `named-checkzone` validation with rollback and `.err` quarantine, and full `named.conf.local` reconstruction (primary + secondary zones).
- `bind-dnssec`: DNSSEC key generation, zone signing, DS/DNSKEY publication in `dnssec_info`, periodic re-signing job, cleanup on disable/delete.
- `dns-zone-management`: API-side zone/record/secondary/template domain logic — validation per record type, SOA serial management, zone wizard expansion, riud permissions and datalog integration.
- `dns-rest-api`: REST endpoints porting `remote.d/dns.inc.php` (zones, records per type, slaves, templates, status/dnssec toggles) with swagger docs.
- `dns-panel-ui`: Vue DNS module — zone list/form with tabs, embedded record editor grid, secondary zones, wizard, template admin.

### Modified Capabilities

(none — the foundation capabilities are consumed, not changed: `sys-datalog` registries, `master-templates` renderer, `rest-api-core` validation framework, `auth-permissions` scopes, `panel-skeleton` navigation)

## Impact

- New Go packages: `internal/modules/dns` (module + bind plugin), zone/record domain logic under the API layer, REST handlers, DNSSEC job on the daemon scheduler.
- Templates copied from ISPConfig: `bind_pri.domain.master`, `bind_named.conf.local.master`, `bind_named.conf.local.slave` (rendered by the foundation `.master` engine).
- DB: no schema changes — uses existing `dns_soa`, `dns_rr`, `dns_slave`, `dns_template` tables (byte-identical ISPConfig3 schema); adds GORM models for them.
- Server config consumed via getconf `dns` section: `bind_zonefiles_dir`, `bind_zonefiles_masterprefix`, `bind_zonefiles_slaveprefix`, `bind_keyfiles_dir`, `bind_user`, `bind_group`, `named_conf_local_path`, `disable_bind_log`.
- External binaries required on the DNS server: `named-checkzone`, `named`, `dnssec-keygen`, `dnssec-signzone`, systemd unit `bind9`/`named`.
- Frontend: new `dns` module in the Vue panel; new locale keys (en).
- Conventional commits per finished task; swaggo regeneration.

## Non-goals

- PowerDNS support (`restartPowerDNS`, `powerdns.sql`) — Bind only.
- Zone import via AXFR or zone-file upload (`dns_import.php`).
- Dynamic DNS (RFC 2136 client tooling) — `update_acl` passthrough to BIND only.
- Multi-server/mirror replication semantics (single-server, per foundation).
- Translations beyond English.
