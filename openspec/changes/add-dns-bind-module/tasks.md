# Tasks: add-dns-bind-module

## 1. Models and foundations wiring

- [x] 1.1 Add GORM models for `dns_soa`, `dns_rr`, `dns_slave`, `dns_template` with explicit `gorm:"column:..."` tags matching the ISPConfig3 schema; unit-test round-trip against MariaDB (commit: `feat(dns): add DNS GORM models`)
- [x] 1.2 Add the `dns` getconf section struct (`bind_zonefiles_dir`, `bind_zonefiles_masterprefix`, `bind_zonefiles_slaveprefix`, `bind_keyfiles_dir`, `bind_user`, `bind_group`, `named_conf_local_path`, `disable_bind_log`) with defaults and tests (commit: `feat(dns): dns server config section`)
- [x] 1.3 Copy `bind_pri.domain.master`, `bind_named.conf.local.master`, `bind_named.conf.local.slave` from `base/ispconfig3_install/server/conf/` into the embedded templates dir (commit: `feat(dns): embed bind master templates`)

## 2. dns module (daemon events)

- [x] 2.1 Implement `internal/modules/dns` Module: announce the 9 events, register table hooks for `dns_soa`/`dns_slave`/`dns_rr`, map datalog actions i/u/d to events; gate on `server.dns_server=1` + config.toml enablement; unit tests with fake registries (commit: `feat(dns): dns module table hooks and events`)
- [x] 2.2 Register the `bind` service (systemd unit `bind9` fallback `named`) with restart/reload actions in the services registry; test unit resolution (commit: `feat(dns): bind service registration`)

## 3. bind plugin — zone generation

- [x] 3.1 Implement pure `RenderZone(soa, records, caaSupported)`: record pre-processing (TTL 0→empty, empty name→`@`, TXT>255 split, CAA→TYPE257 hex with synthetic `issue ";"`), `.master` render; unit tests for each transform (commit: `feat(dns): zone renderer with record pre-processing`)
- [x] 3.2 Produce golden files with the PHP `tpl.inc.php`/bind_plugin logic for fixtures covering all record types, TXT split, CAA modern/legacy, empty name/TTL; add byte-identical golden tests (commit: `test(dns): zone rendering golden files`)
- [x] 3.3 Implement BIND version probe (`named -v`, CAA >= 9.9.6) cached per daemon run (commit: `feat(dns): bind version detection`)
- [x] 3.4 Implement zone file writer: path from origin (strip trailing dot, `/`→`_`), chown bind user/group, cache into `dns_soa.rendered_zone`; skip recordless zones (commit: `feat(dns): zone file writer`)
- [x] 3.5 Implement `named-checkzone` validation with rollback: remove stale `.err`, on failure restore previous content, quarantine render as `.err`, log per `disable_bind_log`, record datalogError; tests with a stub checker binary (commit: `feat(dns): zone validation with rollback and quarantine`)
- [x] 3.6 Implement `writeNamedConf`: primary zones (active, this server, file exists, `.signed` when dnssec_wanted, options from xfer/also_notify/update_acl with comma→`;`) + secondary zones (masters from ns, allow-transfer none default), atomic write; golden tests for `named.conf.local` (commit: `feat(dns): named.conf.local reconstruction`)
- [x] 3.7 Implement soa_insert/soa_update handlers: render→write→validate→named.conf→old-origin cleanup (file/.err/.signed)→delayed restart (update_acl set) or reload (commit: `feat(dns): soa event handlers`)
- [x] 3.8 Implement rr_insert/rr_update/rr_delete: load parent SOA (`new.zone`/`old.zone`), no-op when missing/inactive, delegate to soa_update; integration test datalog row → zone file rewritten (commit: `feat(dns): rr event handlers`)
- [x] 3.9 Implement soa_delete and slave handlers: named.conf rebuild, file cleanup, slave dir creation 0770 + chown, reloads; tests (commit: `feat(dns): delete and slave event handlers`)

## 4. Zone management (API-side domain logic)

- [ ] 4.1 Implement `NextSerial` (YYYYMMDDnn) with unit tests for same-day increment, overflow date-roll, stale-date reset (commit: `feat(dns): SOA serial management`)
- [ ] 4.2 Implement SOA validation (origin FQDN regex+UNIQUE+IDN/lowercase, ns/mbox regexes, ranges >=60, xfer/also_notify IP lists, update_acl admin-only, dnssec_algo set) on the foundation validation engine; tests (commit: `feat(dns): zone validation rules`)
- [ ] 4.3 Implement declarative record-type metadata (18 types + SPF/DKIM/DMARC helpers): name/data/aux/ttl validators per `dns_<type>.tform.php`, TXT negative regexes; JSON export endpoint for the UI; table-driven tests (commit: `feat(dns): record type metadata and validation`)
- [ ] 4.4 Implement zone/record/slave repositories with riud scopes, datalog `{old,new}` writes, and transactional serial bump with `SELECT ... FOR UPDATE`; permission tests (client/reseller/admin) (commit: `feat(dns): dns repositories with permissions and datalog`)
- [ ] 4.5 Implement template expansion (placeholders `{DOMAIN}/{IP}/{IPV6}/{NS1}/{NS2}/{EMAIL}`, `[ZONE]`/`[DNS_RECORDS]` parsing, `TYPE|name|data|aux|ttl`, DNSSEC injection, initial serial `<today>01`, single transaction); test against the stock Default template row (commit: `feat(dns): zone template expansion`)

## 5. REST API

- [ ] 5.1 Zone endpoints: create/get/get-id-by-origin/list/update/delete/set-status/set-dnssec with swaggo annotations; handler tests (commit: `feat(api): dns zone endpoints`)
- [ ] 5.2 Record endpoints: typed CRUD + list-by-zone with `update_serial` flag; swaggo; tests incl. 403 on foreign zone (commit: `feat(api): dns record endpoints`)
- [ ] 5.3 Secondary zone + template endpoints (slave CRUD, template list/CRUD, wizard create-from-template); swaggo; tests (commit: `feat(api): dns slave and template endpoints`)
- [ ] 5.4 Regenerate swagger (`swag init`), verify Swagger UI shows all DNS endpoints, CI staleness check green (commit: `docs(api): regenerate swagger for dns`)

## 6. DNSSEC (phase 2)

- [x] 6.1 Implement key creation (`dnssec-keygen` alg 13/7, ZSK+KSK, glob overwrite guards, dsset fall-through, skip when zone file missing); tests with stubbed exec (commit: `feat(dns): dnssec key creation`)
- [x] 6.2 Implement signing (`$INCLUDE` injection, `dnssec-signzone -A -e +1382400 -3 - -N increment`, keycount warning, checkzone gate) and `dnssec_info` DS/DNSKEY publication + `dnssec_last_signed`/`dnssec_initialized` update (commit: `feat(dns): dnssec zone signing`)
- [x] 6.3 Wire the lifecycle decision tree into soa_update (origin change, algo change, enable, disable, steady-state) and cleanup into soa_delete; tests per transition (commit: `feat(dns): dnssec lifecycle transitions`)
- [x] 6.4 Add `dns_resign` daily scheduler job (threshold 5 days, reload after) with job bookkeeping; tests (commit: `feat(dns): periodic dnssec re-signing job`)
- [ ] 6.5 Integration-test DNSSEC end to end on Ubuntu 24.04 Vagrant with real BIND tools (commit: `test(dns): dnssec integration on vagrant`)

## 7. Panel UI (Vue)

- [ ] 7.1 DNS module navigation + zone list (search, status) and secondary-zones list; en locale keys (commit: `feat(ui): dns zone lists`)
- [ ] 7.2 Zone form with tabs Records / Zone settings / Zone rendering; `update_acl` admin-only; serial read-only; DNSSEC fields + info (commit: `feat(ui): dns zone form`)
- [ ] 7.3 Record grid (ordered type,name) + metadata-driven add/edit dialog with per-type fields/validation, delete and active toggle (commit: `feat(ui): dns record editor grid`)
- [ ] 7.4 Zone wizard from templates (inputs from template `fields`, create + navigate) and secondary zone form (commit: `feat(ui): dns wizard and secondary zone form`)
- [ ] 7.5 Admin template management screen (commit: `feat(ui): dns template admin`)
- [ ] 7.6 agent-browser E2E: wizard creation, manual zone, A/MX/TXT record flows, secondary zone, update_acl visibility; screenshots to docs/prints (commit: `test(ui): dns e2e suite`)

## 8. Integration and docs

- [ ] 8.1 End-to-end integration test against MariaDB: API zone+record create → datalog → daemon run → zone file + named.conf.local rendered, validated, reload queued (commit: `test(dns): datalog-to-bind pipeline integration`)
- [ ] 8.2 Module docs in `docs/` (dns config section, file layout, DNSSEC ops, migration notes: self-healing regeneration after cutover) (commit: `docs(dns): dns module documentation`)
