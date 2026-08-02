# Design: DNS/Bind module

## Context

ISPConfig3's DNS stack is three pieces glued by `sys_datalog`:

1. `interface/web/dns/` — 25+ tform forms writing `dns_soa`, `dns_rr`, `dns_slave`, `dns_template` with `{old,new}` datalog diffs; `remote.d/dns.inc.php` exposes the same operations over the remote API and bumps the SOA serial.
2. `server/mods-available/dns_module.inc.php` — registers table hooks for the three tables, translates datalog actions into nine named events, registers the `bind` service.
3. `server/plugins-available/bind_plugin.inc.php` (642 lines) — on any event regenerates the whole zone file (`bind_pri.domain.master`), validates with `named-checkzone` (rollback + `.err` quarantine on failure), rebuilds `named.conf.local` from all active zones + slaves, handles DNSSEC key/sign lifecycle, and schedules a delayed bind reload/restart.

The foundation change already provides everything this module plugs into: datalog consumer with table-hook/event registries, `.master` renderer, getconf, delayed service restarts, riud GORM scopes, validation engine, REST core, panel skeleton. The DB tables exist (byte-identical ISPConfig3 schema); only GORM models are missing.

## Goals / Non-Goals

**Goals:**
- Behavior-faithful port of `dns_module` + `bind_plugin`: same events, same file outputs, same safety nets, reusing the original three `.master` templates verbatim.
- API/UI parity with the ISPConfig DNS module for zones, records (all types the templates support), secondary zones, templates/wizard.
- DNSSEC lifecycle including periodic re-signing via the daemon scheduler (phase 2 within this change).
- Golden-file test coverage proving rendered output matches the PHP original.

**Non-Goals:**
- PowerDNS, AXFR/zone-file import, dynamic DNS tooling, mirror replication, non-English locales (see proposal Non-goals).
- No schema changes of any kind.

## Decisions

### D1 — One Go package, two registrations (module + plugin)
`internal/modules/dns` contains both the `Module` (table hooks → events, port of `dns_module.inc.php`) and the `Plugin` (`bindPlugin`, port of `bind_plugin.inc.php`), wired explicitly in the daemon bootstrap. Keeping the two-level dispatch (hook → named event → plugin) instead of collapsing it preserves the foundation's registry architecture and keeps parity with the nginx module change.
*Alternative*: handle table hooks directly in the plugin — rejected: breaks the announced-events contract shared with future plugins (e.g., a PowerDNS plugin later).

### D2 — Full-zone regeneration on every RR change
`rr_insert/update/delete` load the parent `dns_soa` row and call the same `soaUpdate` path, regenerating the entire zone file — exactly like the PHP original. No incremental record patching.
Rationale: the zone file is small, regeneration is idempotent and self-healing, and it is the behavior the templates and `named-checkzone` flow were designed around. `rr_delete` uses `old.zone` (the SOA may already be gone; then it is a no-op like in PHP).

### D3 — Zone rendering pipeline (pure function + I/O wrapper)
Rendering is split into a pure function `RenderZone(soa, records, bindCAASupported) (string, error)` — applies record pre-processing (TTL 0 → empty, empty name → `@`, TXT >255 split into `" "`-joined 255-byte chunks, CAA→TYPE257 hex encoding incl. the synthetic `issue ";"` record for issuewild-only sets) and feeds the `.master` renderer — and an I/O wrapper that writes/chowns the file, caches `rendered_zone`, runs `named-checkzone`, and rolls back on failure. Rationale: the pure function is golden-file testable without a filesystem or BIND installed.
BIND version detection (`named -v`, CAA supported ≥ 9.9.6) is probed once per daemon run and cached; on Debian 11+/Ubuntu 22.04+ BIND is always ≥ 9.16, so the TYPE257 path exists only for fidelity and is covered by tests, not expected in production.

### D4 — named.conf.local reconstruction
`writeNamedConf` is a full rebuild on every SOA/slave event, port of the PHP function: query active `dns_soa` for this server (skip zones whose zone file does not exist yet — e.g., zones without records), build options from `xfer`/`also_notify`/`update_acl` (comma→`;`), point DNSSEC-wanted zones at `<file>.signed`; then active `dns_slave` rows with `masters {ns}` and `allow-transfer {none;}` default; render `bind_named.conf.local.master` + `.slave` and concatenate. Written atomically (temp file + rename) — a small improvement over PHP's direct `file_put_contents`, safe because BIND only reads the file on reload.

### D5 — Delayed restart vs reload
Port of the PHP rule: after a SOA insert/update, if `new.update_acl` is non-empty request `restart`, else `reload`; slave and delete paths always `reload`. Requests go through the foundation services registry which dedups per service at end of the datalog run (restart wins over reload). The `bind` service resolves the systemd unit at runtime: `bind9` if present, else `named`.

### D6 — DNSSEC as sequenced sub-steps of soa_update (phase 2)
Port the PHP decision tree verbatim inside `soaUpdate`: origin changed → delete old keys + create; algo changed or `dnssec_wanted` freshly enabled → create; disabled → remove `.signed`; steady-state wanted → re-sign. `create` shells out to `dnssec-keygen` (alg 13 ECDSAP256SHA256 preferred, alg 7 NSEC3RSASHA1 legacy; ZSK + KSK each), `sign` appends missing `$INCLUDE` lines and runs `dnssec-signzone -A -e +1382400 -3 - -N increment`, then writes DS + DNSKEY text into `dnssec_info` and stamps `dnssec_last_signed`/`dnssec_initialized`.
Periodic re-signing: a named scheduler job (`dns_resign`, daily) re-signs zones where `dnssec_wanted='Y'` and `dnssec_last_signed` is older than a threshold (default 5 days, well inside the 16-day validity) — replaces ISPConfig's cron-driven re-sign.
The entropy check (`/proc/sys/kernel/random/entropy_avail < 200`) is dropped: kernels ≥ 5.6 (all target OS) have a non-blocking, always-seeded CSPRNG, so the check is dead code on Debian 11+.
Phasing: tasks isolate DNSSEC so the module ships zone/record management first; the columns already exist so no migration is needed.

### D7 — Serial management on the API side
Port `increase_serial` from `remote.d/dns.inc.php` as a pure function `NextSerial(current uint32, today time.Time) uint32` (`YYYYMMDDnn`; same date → nn+1, nn>99 → date+1/nn=00; older date → today+`01`). Every mutating zone/record API operation bumps the SOA serial in the same transaction as the record write and the datalog rows — matching ISPConfig's `update_serial` default behavior for panel operations (the REST API also exposes an explicit `update_serial` flag for parity with the remote API).
Rationale: the daemon renders whatever serial is in the DB (the template just prints it); serial policy is interface-side in ISPConfig and stays there.

### D8 — Record type definitions as declarative Go metadata
One table of record-type descriptors (A, AAAA, ALIAS, CAA, CNAME, DNAME, DS, HINFO, LOC, MX, NAPTR, NS, PTR, RP, SRV, SSHFP, TLSA, TXT + TXT-derived SPF/DKIM/DMARC forms) replacing the 20+ `dns_<type>.tform.php` files: per type — name regex, data validators (e.g., A: NOTEMPTY+ISIPV4; MX: NOTEMPTY + hostname regex + aux; TXT: NOTEMPTY + "no DKIM/DMARC/SPF payload in plain TXT" negative regexes), aux usage, TTL range (`>=60`), defaults. Consumed by the API validators (foundation validation engine) and exported as JSON metadata to the Vue record editor — one grid + one dialog driven by metadata instead of 20 hand-written forms.
Zone-level validation ports `dns_soa.tform.php`: origin NOTEMPTY+UNIQUE+FQDN regex, ns/mbox regexes with IDN→ASCII and lowercase filters, refresh/retry/expire/minimum/ttl ranges (`>=60`), xfer/also_notify comma-separated IP lists, `update_acl` admin-only.

### D9 — Wizard/template expansion server-side
`dns_template.template` keeps ISPConfig's `[ZONE]`/`[DNS_RECORDS]` INI-ish format with `{DOMAIN} {IP} {IPV6} {NS1} {NS2} {EMAIL}` placeholders and `TYPE|name|data|aux|ttl` record lines (port of `dns_wizard.inc.php` + `dns_templatezone_add`). Expansion happens in one API call → one DB transaction creating the SOA + all records + datalog rows + initial serial. The `fields` column drives which wizard inputs the UI shows; DNSSEC checkbox injects `dnssec_wanted=Y`. Keeping the legacy format means existing ISPConfig templates in migrated databases keep working.

### D10 — UI shape mirrors dns_soa.tform.php
Zone form tabs: **Records** (embedded grid over `dns_rr` ordered by type,name — port of the `plugin_listview` tab), **Zone settings** (SOA fields; `update_acl` rendered only for admin), **Zone rendering** (read-only `rendered_zone`, shown when the global config flag enables zone export). Record add/edit is a dialog inside the Records tab (replaces the per-type `dns_<type>_edit.php` pages). Secondary zones and templates are separate lists. All strings through i18n (en.json).

## Risks / Trade-offs

- [Zone render output drifts from PHP → subtle BIND behavior changes] → golden files: fixtures rendered by the original `tpl.inc.php`+`bind_plugin` logic committed as expected output; Go renderer must match byte-for-byte (incl. TXT split and CAA hex cases).
- [Bad zone data bricks the name server] → same safety net as PHP, enforced by spec: `named-checkzone` before activation, previous file restored, `.err` quarantine, `datalogError` recorded; named.conf only references zone files that exist.
- [DNSSEC shell-outs differ across BIND 9.16/9.18 (`dnssec-keygen` output names, deprecation warnings)] → integration-test on Ubuntu 24.04 Vagrant; key files matched by glob (`K<domain>.+013*`) exactly like PHP, which is version-tolerant.
- [Full named.conf rebuild wipes manual edits] → same as ISPConfig: `named_conf_local_path` is owned by the panel; documented in module docs.
- [Record grid genericity hides type quirks (SRV aux+data layout, CAA flag/tag/value)] → per-type field hints in the metadata (input placeholders/help), E2E tests for MX/SRV/CAA dialogs.
- [Serial bump races under concurrent API writes] → serial update executes inside the same DB transaction with `SELECT ... FOR UPDATE` on the SOA row.

## Migration Plan

- Ships as code only — no schema change, no config-file change beyond documenting the `dns` getconf section (already present in migrated `server.config`).
- Fresh installs: installer change (`add-installer-cli`) seeds the `dns` server config section and installs BIND; until then the section defaults documented in this module are used.
- Migrated ISPConfig3 databases: existing zones/records/templates work as-is; first datalog event after cutover regenerates zone files and `named.conf.local` from DB state (self-healing). DNSSEC-enabled zones keep their key files on disk untouched.
- Rollback: disable the dns module in `config.toml`; files on disk stay as last rendered.

## Open Questions

- Should `rendered_zone` also store the `.signed` content for DNSSEC zones for export? (PHP stores only the unsigned render — keep parity for now.)
- `dns_ssl_ca` table (CAA helper UI in newer ISPConfig) — out of scope here; revisit if the CAA dialog needs preset CAs.
