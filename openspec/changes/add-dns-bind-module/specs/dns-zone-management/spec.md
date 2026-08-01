# dns-zone-management

## ADDED Requirements

### Requirement: Zone (SOA) validation
Zone create/update SHALL enforce the `dns_soa.tform.php` rules: `server_id` NOTEMPTY and a DNS-capable server; `origin` NOTEMPTY, UNIQUE, matching `^[a-zA-Z0-9.\-\/]{1,255}\.[a-zA-Z0-9\-]{2,63}\.?$`, lowercased and IDN-converted to ASCII on save; `ns` matching `^[a-zA-Z0-9.\-]{1,255}$`; `mbox` NOTEMPTY matching `^[a-zA-Z0-9.\-_+]{0,255}\.$`; `refresh`/`retry`/`expire`/`minimum`/`ttl` integers >= 60 (defaults 7200/540/604800/3600/3600); `xfer`/`also_notify` empty or comma-separated valid IPs; `update_acl` writable by admin only. `active`, `dnssec_wanted` are Y/N; `dnssec_algo` a subset of {NSEC3RSASHA1, ECDSAP256SHA256} (default ECDSAP256SHA256).

#### Scenario: Duplicate origin rejected
- **WHEN** a zone is created with an origin that already exists
- **THEN** the API returns a validation error naming the origin uniqueness rule and no rows are written

#### Scenario: IDN origin normalized
- **WHEN** a zone is created with origin `Bücher.example.` 
- **THEN** the stored origin is the lowercase punycode form `xn--bcher-kva.example.`

#### Scenario: Non-admin cannot set update_acl
- **WHEN** a client user submits a zone update including `update_acl`
- **THEN** the field is rejected/ignored and the stored value is unchanged

### Requirement: Record validation per type
Record create/update SHALL validate against declarative per-type metadata porting the `dns_<type>.tform.php` rules: `name` per-type regex (e.g., A: `^[a-zA-Z0-9.\-*]{0,64}$`, TXT allows `_` and leading `*.`), lowercased/IDN-normalized; `data` per type (A: NOTEMPTY+valid IPv4; AAAA: valid IPv6; MX/NS/CNAME/ALIAS/PTR: NOTEMPTY hostname regex `^[a-zA-Z0-9.\-]{1,255}$`; TXT: NOTEMPTY and MUST NOT contain `v=DKIM`, `v=DMARC1; ` or `v=spf` payloads (dedicated forms exist for those); SRV/NAPTR/CAA/DS/TLSA/SSHFP/HINFO/LOC/RP: type-specific formats); `aux` integer used by MX/SRV/NAPTR priority; `ttl` integer >= 60 (default 3600); `type` MUST be one of the supported set; `zone` MUST reference an accessible `dns_soa` row and `server_id` is inherited from the zone.

#### Scenario: A record with invalid IP rejected
- **WHEN** an A record is submitted with `data = 'not-an-ip'`
- **THEN** the API returns a validation error and no datalog row is written

#### Scenario: SPF payload rejected in plain TXT
- **WHEN** a TXT record is submitted with data starting `v=spf1 ...`
- **THEN** validation fails directing the user to the SPF record form

#### Scenario: MX stores priority in aux
- **WHEN** an MX record is created with priority 10 and data `mail.example.com.`
- **THEN** the stored row has `aux = 10` and passes hostname validation

### Requirement: SOA serial management
The system SHALL maintain ISPConfig-style serials `YYYYMMDDnn` (port of `remote.d/dns.inc.php::increase_serial`): if the serial's date part >= today, increment `nn` (rolling `nn>99` into date+1, nn=00); otherwise set `<today>01`. Every mutating zone or record operation through the API SHALL bump the parent SOA serial in the same database transaction as the data change and its datalog rows, locking the SOA row (`SELECT ... FOR UPDATE`). The REST API SHALL accept an `update_serial=false` flag to skip the bump (remote-API parity).

#### Scenario: Same-day second change increments counter
- **WHEN** a record is added to a zone whose serial is `2026080101` on 2026-08-01
- **THEN** the SOA serial becomes `2026080102`

#### Scenario: Stale serial jumps to today
- **WHEN** a record changes in a zone whose serial is `2025010199` on 2026-08-01
- **THEN** the SOA serial becomes `2026080101`

#### Scenario: Counter overflow rolls the date
- **WHEN** a change occurs on a zone with serial `2026080199` on 2026-08-01
- **THEN** the SOA serial becomes `2026080200`

### Requirement: Secondary zone management
`dns_slave` records SHALL be manageable with fields `origin` (same normalization/uniqueness rules per server as zones), `ns` (master IP list), `xfer`, `active`, `server_id`, with riud permissions and datalog rows, so the bind plugin can configure slave zones.

#### Scenario: Secondary zone creation reaches the daemon
- **WHEN** a secondary zone is created via API
- **THEN** a `dns_slave` insert datalog row is written for the target server

### Requirement: Zone templates and wizard expansion
`dns_template` records (name, `fields` CSV of DOMAIN/IP/IPV6/NS1/NS2/EMAIL/DKIM/DNSSEC, `template` text, visible) SHALL drive a zone wizard. Expansion (port of `dns_wizard.inc.php`/`dns_templatezone_add`) SHALL replace `{DOMAIN}`, `{IP}`, `{IPV6}`, `{NS1}`, `{NS2}`, `{EMAIL}` placeholders, parse the `[ZONE]` section into SOA fields and `[DNS_RECORDS]` lines `TYPE|name|data|aux|ttl` into records, apply the DNSSEC option by injecting `dnssec_wanted=Y`, set the initial serial to `<today>01`, and create the SOA plus all records plus datalog rows in one transaction owned by the target client. Legacy templates from migrated ISPConfig3 databases MUST expand unchanged.

#### Scenario: Default template creates a full zone
- **WHEN** the wizard runs the stock Default template with domain `example.com`, IP, NS1/NS2 and email
- **THEN** one `dns_soa` and its A/NS/MX/TXT records are created atomically with serial `<today>01` and datalog rows for each

#### Scenario: Placeholder left unfilled fails validation
- **WHEN** a required wizard field listed in the template's `fields` is empty
- **THEN** the API returns a validation error and nothing is created

### Requirement: riud permissions and datalog on all DNS mutations
All zone/record/slave/template operations SHALL go through the foundation permission scope (`sys_userid/sys_groupid/sys_perm_*`) and SHALL write `{old,new}` JSON datalog rows targeted at the zone's `server_id`. Zone status toggle and DNSSEC toggle operations SHALL be exposed (port of `dns_zone_set_status`, `dns_zone_set_dnssec`).

#### Scenario: Client cannot touch another client's zone
- **WHEN** client A updates a record in a zone owned by client B's group with empty `sys_perm_other`
- **THEN** the API returns 403 and no datalog row is written

#### Scenario: Deactivating a zone removes it from named.conf
- **WHEN** `dns_zone_set_status` sets a zone inactive
- **THEN** a `dns_soa` update datalog row is written and the subsequent daemon run drops the zone from `named.conf.local`
