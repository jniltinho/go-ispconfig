# powerdns-dnssec

PowerDNS-native DNSSEC lifecycle for zones when `dns_backend=powerdns`. Port of `powerdns_plugin.inc.php` `handle_dnssec` / `soa_dnssec_create` / `soa_dnssec_disable` / `soa_dnssec_delete` / `rectifyZone` using `pdnsutil` (or legacy `pdnssec`). Diverges from Bind's `dnssec-keygen` / `dnssec-signzone` flow by design; panel fields on `dns_soa` remain the single UI/API surface.

## ADDED Requirements

### Requirement: DNSSEC only on supported PowerDNS major versions
DNSSEC create/disable/delete and `rectify-zone` SHALL run only when `pdns_control version` reports a major version starting with `3` or `4` (PHP `is_pdns_version_supported`). When the version is unsupported or `pdnsutil`/`pdnssec` is missing, DNSSEC steps SHALL no-op with a log and MUST NOT fail the surrounding SOA/RR event's SQL sync.

#### Scenario: PowerDNS 4 enables DNSSEC commands
- **WHEN** `pdns_control version` returns a string starting with `4` and `pdnsutil` is on PATH
- **THEN** DNSSEC create/disable/rectify commands are allowed to run

#### Scenario: Unsupported version skips DNSSEC
- **WHEN** version is missing or not major 3/4
- **THEN** `handle_dnssec` and `rectifyZone` perform no shell-outs and leave SQL changes intact

### Requirement: Enable DNSSEC on wanted transition
When `new.dnssec_wanted = 'Y'` and old is null or `'N'`, the plugin SHALL run (in order): `add-zone-key <zone> ksk active 2048 rsasha256`, `add-zone-key <zone> zsk active 1024 rsasha256`, `set-nsec3 <zone> "1 0 10 deadbeef"`, `show-zone <zone>`; parse active KSK/CSK and DS lines into a human-readable `dns_soa.dnssec_info` block (port of `format_dnssec_pubkeys`, including raw log section); and set `dns_soa.dnssec_initialized = 'Y'`. Zone name is `origin` without trailing dot. Algorithm selection ignores `dns_soa.dnssec_algo` (fixed rsasha256, PHP parity).

#### Scenario: Enabling DNSSEC populates dnssec_info
- **WHEN** a SOA update sets `dnssec_wanted` from N to Y on an active zone present in PowerDNS
- **THEN** KSK and ZSK keys are added, NSEC3 is set, `dnssec_initialized` becomes `Y`, and `dnssec_info` contains DNSKEY/DS material for the active KSK/CSK

### Requirement: Disable DNSSEC when unwanted
When `new.dnssec_wanted = 'N'` and `old.dnssec_wanted = 'Y'`, the plugin SHALL run `pdnsutil|pdnssec disable-dnssec <zone>` and set `dns_soa.dnssec_initialized = 'N'`. It SHALL NOT clear `dnssec_info` on disable (PHP leaves the field).

#### Scenario: Disabling DNSSEC clears initialized flag
- **WHEN** a SOA update sets `dnssec_wanted` from Y to N on an initialized zone
- **THEN** `disable-dnssec` is executed and `dnssec_initialized` is `N`

### Requirement: Origin change removes old DNSSEC material
When `old.origin` differs from `new.origin` and `old.dnssec_initialized = 'Y'` with a non-trivial old origin, the plugin SHALL run `disable-dnssec` against the **old** origin before any create on the new origin, and update `dnssec_info` / `dnssec_initialized` accordingly (port of `soa_dnssec_delete` + subsequent create if still wanted).

#### Scenario: Renamed DNSSEC zone re-keys under new origin
- **WHEN** a SOA update renames `old.com.` to `new.com.` with `dnssec_wanted = 'Y'` and old initialized
- **THEN** DNSSEC is disabled for `old.com` and created for `new.com` if wanted remains Y

### Requirement: Rectify after zone and record mutations
After successful SOA insert/update (active path) and RR insert/update (active path), the plugin SHALL run `rectify-zone` for the zone origin. For RR events without an origin field, origin SHALL be resolved via `powerdns.domains` joined from the RR's `domain_id` / `ispconfig_id` (PHP parity).

#### Scenario: RR insert triggers rectify
- **WHEN** `dns_rr_insert` successfully writes a PowerDNS record
- **THEN** `pdnsutil rectify-zone <origin>` (or `pdnssec`) is invoked once for that zone

### Requirement: No Bind-style periodic re-sign for PowerDNS
When `dns_backend=powerdns`, the daemon SHALL NOT register or run the Bind `dns_resign` scheduler job for that server. Online signing is owned by PowerDNS after keys exist.

#### Scenario: PowerDNS backend skips dns_resign
- **WHEN** the daemon starts with `dns_backend=powerdns`
- **THEN** no `dns_resign` job is registered for periodic Bind signing
