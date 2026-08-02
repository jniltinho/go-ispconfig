# bind-zone-generation

## ADDED Requirements

### Requirement: Zone file rendered from bind_pri.domain.master
On `dns_soa_insert`/`dns_soa_update` the bind plugin SHALL render the zone from the original `bind_pri.domain.master` template using the SOA fields (`ttl`, `ns`, `mbox`, `serial`, `refresh`, `retry`, `expire`, `minimum`) and all `dns_rr` rows with `zone = soa.id AND active = 'Y'`, and write it to `<bind_zonefiles_dir>/<bind_zonefiles_masterprefix><origin-without-trailing-dot>` (with `/` in the origin replaced by `_`), owned by `bind_user`:`bind_group`. If the zone has no active records, no file SHALL be written and the event completes with a debug log (PHP parity).

#### Scenario: Zone with records is rendered and written
- **WHEN** `dns_soa_update` fires for an active zone `example.com.` with active A/NS/MX records
- **THEN** `pri.example.com` is written under `bind_zonefiles_dir` with SOA header and one line per record, owned by the bind user/group

#### Scenario: Zone without records is skipped
- **WHEN** `dns_soa_insert` fires for a zone that has no active `dns_rr` rows
- **THEN** no zone file is written and no error is raised

### Requirement: Record pre-processing before rendering
The renderer SHALL apply, per record (port of `bind_plugin::soa_update`): TTL `0` rendered as empty; empty `name` rendered as `@`; TXT `data` longer than 255 bytes split into 255-byte chunks joined with `" "`; on BIND < 9.9.6 CAA records converted to `TYPE257` generic encoding (`\# <len> <hex>` with tag-length prefixes `0005` for issue, `0009` for issuewild) including a synthetic `issue ";"` record when only issuewild records exist. BIND version SHALL be probed once per daemon run (`named -v`).

#### Scenario: Long TXT record is split
- **WHEN** a TXT record has 600 bytes of data
- **THEN** the rendered line contains three quoted chunks of 255/255/90 bytes joined by `" "`

#### Scenario: Empty name becomes origin shorthand
- **WHEN** a record has `name = ''` and `ttl = 0`
- **THEN** the rendered line starts with `@` and has an empty TTL column

#### Scenario: CAA passthrough on modern BIND
- **WHEN** BIND >= 9.9.6 is detected and a CAA record exists
- **THEN** the record renders as type `CAA` with its data unchanged

#### Scenario: CAA hex encoding on legacy BIND
- **WHEN** BIND < 9.9.6 is detected and the zone has only an issuewild CAA record
- **THEN** the record renders as `TYPE257` with `0009`-prefixed hex data plus a synthetic `TYPE257` record encoding `issue ";"`

### Requirement: Rendered zone cached in dns_soa.rendered_zone
After rendering, the plugin SHALL store the exact rendered zone text in `dns_soa.rendered_zone` for export/UI display.

#### Scenario: rendered_zone matches file content
- **WHEN** a zone is successfully rendered
- **THEN** `dns_soa.rendered_zone` equals the bytes written to the zone file

### Requirement: Zone validation with rollback and quarantine
After writing, the plugin SHALL run `named-checkzone <origin> <file>`. On failure it SHALL rename the bad file to `<file>.err`, restore the previous zone file content (when one existed) with correct ownership, log the checker output (level WARN, or DEBUG when `disable_bind_log = 'y'`) and record the output via the datalog error mechanism. A pre-existing `<file>.err` SHALL be removed before validation.

#### Scenario: Invalid zone is rolled back
- **WHEN** the rendered zone fails `named-checkzone` and a previous zone file existed
- **THEN** the previous content is restored, the invalid render is kept as `.err`, and the datalog row records the checker output

#### Scenario: Invalid first render leaves only quarantine
- **WHEN** the rendered zone fails validation and no previous zone file existed
- **THEN** only `<file>.err` remains and no zone file is active

### Requirement: RR events regenerate the whole zone
`dns_rr_insert`/`dns_rr_update`/`dns_rr_delete` SHALL load the parent `dns_soa` row (`new.zone`, or `old.zone` on delete) and run the full SOA update path, regenerating the entire zone file. Insert and delete SHALL be no-ops when the parent SOA is missing or not active (PHP parity).

#### Scenario: Editing one record rewrites the zone file
- **WHEN** `dns_rr_update` fires for a record of an active zone
- **THEN** the zone file is fully re-rendered including all other active records and the current SOA serial

#### Scenario: RR delete after zone deletion is a no-op
- **WHEN** `dns_rr_delete` fires and the parent `dns_soa` row no longer exists
- **THEN** no file is written and no error is raised

### Requirement: named.conf.local full reconstruction
On every SOA or slave event the plugin SHALL rebuild `named_conf_local_path` from the templates `bind_named.conf.local.master` and `bind_named.conf.local.slave`: all `dns_soa WHERE active='Y' AND server_id=<this>` as master zones (only those whose zone file exists on disk; file path suffixed `.signed` when `dnssec_wanted='Y'`; options `allow-transfer`/`also-notify`/`allow-update` built from non-empty `xfer`/`also_notify`/`update_acl` with commas replaced by `;`), followed by all `dns_slave WHERE active='Y' AND server_id=<this>` as slave zones (`masters {ns;}` and `allow-transfer` from `xfer` or `{none;}` when empty). The write SHALL be atomic (temp file + rename).

#### Scenario: Master zone with transfer ACL
- **WHEN** an active zone has `xfer = '1.2.3.4,5.6.7.8'`
- **THEN** its `named.conf.local` block contains `allow-transfer {1.2.3.4;5.6.7.8;};`

#### Scenario: DNSSEC zone references signed file
- **WHEN** an active zone has `dnssec_wanted = 'Y'`
- **THEN** its block's `file` path ends in `.signed`

#### Scenario: Recordless zone excluded
- **WHEN** an active zone has no zone file on disk (no records yet)
- **THEN** it does not appear in `named.conf.local`

#### Scenario: Slave zone defaults
- **WHEN** an active `dns_slave` row has empty `xfer`
- **THEN** its block has `type slave`, `masters` from `ns`, and `allow-transfer {none;};`

### Requirement: Cleanup on origin rename and zone delete
When `old.origin` differs from `new.origin` the plugin SHALL delete the old zone's file plus its `.err` and `.signed` variants. On `dns_soa_delete` it SHALL rebuild `named.conf.local`, delete the zone file and `.err`, and remove DNSSEC materials. On `dns_slave_delete` it SHALL rebuild `named.conf.local` and delete the slave zone file.

#### Scenario: Renamed zone leaves no stale files
- **WHEN** a zone origin changes from `old.com.` to `new.com.`
- **THEN** `pri.old.com`, `pri.old.com.err` and `pri.old.com.signed` are removed and `pri.new.com` is written

#### Scenario: Deleted zone removed from named.conf
- **WHEN** `dns_soa_delete` fires
- **THEN** the zone file is deleted and the rebuilt `named.conf.local` no longer references the zone

### Requirement: Delayed bind reload or restart
After handling SOA insert/update the plugin SHALL request a delayed `restart` of the `bind` service when `new.update_acl` is non-empty, otherwise a delayed `reload`. Slave events and deletes SHALL request `reload`.

#### Scenario: update_acl forces restart
- **WHEN** a SOA update sets `update_acl = '1.2.3.4'`
- **THEN** a delayed `restart` is queued for the `bind` service

#### Scenario: Plain record change reloads
- **WHEN** a record change triggers a SOA regeneration and `update_acl` is empty
- **THEN** a delayed `reload` is queued

### Requirement: Slave zone directory ownership
On `dns_slave_insert`/`dns_slave_update` the plugin SHALL ensure the slave zonefile directory (derived from `bind_zonefiles_slaveprefix`, or `bind_zonefiles_dir` when the prefix is empty) exists with mode 0770 and is owned by `bind_user`:`bind_group`, so named can write transferred zones.

#### Scenario: Slave dir created on first secondary zone
- **WHEN** the first `dns_slave_insert` fires and the slave directory does not exist
- **THEN** the directory is created 0770 and chowned to the bind user/group

### Requirement: Golden-file fidelity with the PHP renderer
Zone file and `named.conf.local` rendering SHALL be covered by golden-file tests whose expected outputs were produced by the original PHP `tpl.inc.php`/`bind_plugin` logic for the same fixture data, and the Go output MUST match byte-for-byte. Fixtures SHALL cover at minimum: all record types in `bind_pri.domain.master`, TTL 0/empty-name, TXT split, CAA modern and TYPE257 legacy, xfer/also_notify/update_acl option building, dnssec `.signed` path, and slave zones.

#### Scenario: Golden zone file matches
- **WHEN** the fixture zone is rendered by the Go implementation
- **THEN** the output is byte-identical to the committed PHP-produced golden file
