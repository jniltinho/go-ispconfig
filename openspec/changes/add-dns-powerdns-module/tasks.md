# Tasks: add-dns-powerdns-module

## 1. Models, config, and embedded assets

- [x] 1.1 Embed `base/ispconfig3_install/install/sql/powerdns.sql` and `install/tpl/pdns.local.master` into the binary (installer + tests); unit-test that the embedded SQL creates `domains`, `records`, `supermasters`, `domainmetadata` with `ispconfig_id` on domains/records. Commit.
- [x] 1.2 Add GORM models for PowerDNS tables `domains` and `records` (and stubs if needed for metadata) with explicit `gorm:"column:..."`, table names matching `powerdns.sql`; unit-test round-trip against a MariaDB `powerdns` database. Commit.
- [ ] 1.3 Add `[dns] dns_backend` (`bind`|`powerdns`, default bind) and `powerdns_axfr_conf` to the dns getconf section; optional PowerDNS DSN override in `config.toml`; tests for defaults and parsing. Commit.
- [ ] 1.4 Implement PowerDNS DB open helper (same-host MariaDB credentials + database `powerdns`, or override DSN); fail clearly when backend is powerdns and DB is unreachable. Commit.

## 2. PowerDNS plugin — zone/record/slave SQL sync

- [ ] 2.1 Scaffold `internal/powerdns` Plugin: subscribe to the nine dns events, gate on server_id, inject PowerDNS DB + CommandRunner + logger; unit tests with fake registry. Commit.
- [ ] 2.2 Implement pure mappers: origin strip, ns/hostmaster SOA content, RR name absolute/relative, content rules (CNAME/MX/NS/ALIAS/PTR/SRV, HINFO quote transform, default raw data), prio←aux; table-driven tests vs PHP fixtures. Commit.
- [ ] 2.3 Implement `soa_insert` / `soa_update` / `soa_delete` handlers (active/inactive transitions, re-import active `dns_rr` on activate, MASTER domain + SOA record lifecycle). Commit.
- [ ] 2.4 Implement `rr_insert` / `rr_update` / `rr_delete` (skip missing parent domain, skip duplicate ispconfig_id, never delete SOA via RR path). Commit.
- [ ] 2.5 Implement `slave_insert` / `slave_update` / `slave_delete` (SLAVE domains, purge `ispconfig_id=0` cache records on update). Commit.
- [ ] 2.6 Event-to-SQL integration tests against MariaDB: datalog-like payloads produce expected `powerdns.domains` / `powerdns.records` rows for master, RR, and slave scenarios. Commit.

## 3. Control commands, service, and backend selection

- [ ] 3.1 Implement `pdns_control` / `pdnsutil|pdnssec` discovery and wrappers: rediscover, notify, retrieve, rectify-zone, version probe; stubbed CommandRunner tests. Commit.
- [ ] 3.2 Wire rediscover/notify after active SOA insert/update; retrieve after active slave insert/update; rectify after active SOA/RR mutations; missing binary is non-fatal. Commit.
- [ ] 3.3 Register `powerdns` service; unit resolution `powerdns` else `pdns`; on restart rewrite `allow-axfr-ips` from active `dns_soa.xfer` ∪ `dns_slave.xfer` (always include 127.0.0.1) to `powerdns_axfr_conf`; tests for unique IP merge and localhost default. Commit.
- [ ] 3.4 Queue delayed `powerdns` restart from SOA/slave handlers (not pure RR); integration test dedup at end of run. Commit.
- [ ] 3.5 Daemon bootstrap: when `server.dns_server=1` and `dns_backend=powerdns`, load PowerDNS plugin + service only; when bind/default, load Bind only; tests or daemon wiring table. Commit.

## 4. DNSSEC (pdnsutil)

- [ ] 4.1 Port DNSSEC version gate (major 3/4) and `format_dnssec_pubkeys` parser for `show-zone` lines (Active: 1 / Active ( ); KSK/CSK/DS); unit tests with canned output. Commit.
- [ ] 4.2 Implement create path: add-zone-key KSK/ZSK rsasha256, set-nsec3, show-zone → write `dns_soa.dnssec_info`, set `dnssec_initialized=Y`; stubbed exec tests. Commit.
- [ ] 4.3 Implement disable and origin-change delete paths; update `dnssec_initialized`/`dnssec_info` per PHP; wire `handle_dnssec` into SOA update. Commit.
- [ ] 4.4 Ensure Bind `dns_resign` job is not registered when backend is powerdns; test bootstrap matrix. Commit.

## 5. Installer CLI extension

- [ ] 5.1 Add `--dns-backend` / answers-file / interactive prompt (default bind); write `[dns] dns_backend` into server.config; tests for answer resolution. Commit.
- [ ] 5.2 Distro package profiles: when powerdns, install `pdns-server` + `pdns-backend-mysql` (Debian 11–13, Ubuntu 22.04–24.04); bind path unchanged; package-list unit tests. Commit.
- [ ] 5.3 Implement configure_powerdns step: CREATE DATABASE, GRANT, apply embedded powerdns.sql, render pdns.local.master → `/etc/powerdns/pdns.d/pdns.local` mode 0600, enable/restart pdns; connectivity validation; dry-run/unit tests with fakes. Commit.
- [ ] 5.4 Optional Vagrant/toggle or documented smoke: install with powerdns backend, assert pdns active; link from install docs. Commit.

## 6. Shared surface verification (no new API/UI features)

- [ ] 6.1 Confirm existing DNS REST API + Vue panel need no code changes for PowerDNS; if any read-only backend indicator is required for ops, keep it out of scope unless a one-line server-info field already exists — document backend is server-config only. Commit only if a tiny doc-facing fix is needed; otherwise skip commit.
- [ ] 6.2 agent-browser smoke against a PowerDNS-backed daemon (docker or local): login, create zone via wizard, add A/MX/TXT, toggle DNSSEC wanted, verify panel still works (SQL side asserted in Go tests). Screenshots to `docs/prints/`. Commit.

## 7. Integration tests and docs

- [ ] 7.1 End-to-end integration: API zone+record create → sys_datalog → daemon with PowerDNS plugin → expected rows in `powerdns.domains`/`records` + stubbed notify/rectify; dig smoke optional with real pdns container. Commit.
- [ ] 7.2 Write `docs/powerdns-module.md` (or extend `docs/dns-module.md`): backend selection, dual-DB layout, AXFR global ACL, DNSSEC differences vs Bind, migration notes, installer flags. Commit.
- [ ] 7.3 Update `docs/ROADMAP.md` / `docs/install.md` references for optional PowerDNS path; cross-link from dns-module docs. Commit.
