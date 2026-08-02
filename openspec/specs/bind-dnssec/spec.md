# bind-dnssec Specification

## Purpose
TBD - created by archiving change add-dns-bind-module. Update Purpose after archive.
## Requirements
### Requirement: DNSSEC key creation
When a zone requires initialization (`dnssec_wanted='Y'` and not initialized, algorithm changed, or origin changed with DNSSEC wanted) the plugin SHALL generate a ZSK and a KSK per configured algorithm in `bind_keyfiles_dir` via `dnssec-keygen`: ECDSAP256SHA256 (`-3 -a ECDSAP256SHA256`, alg 13) and/or NSEC3RSASHA1 (`-a NSEC3RSASHA1 -b 2048` ZSK / `-b 4096` KSK, alg 7), per the `dnssec_algo` SET column. Existing key files for the same algorithm SHALL NOT be overwritten (glob `K<domain>.+013*`/`+007*` guard); when a `dsset-<domain>.` file already exists with unchanged algorithm the plugin SHALL fall through to update/sign instead. Key creation SHALL be skipped when the zone file does not exist.

#### Scenario: Enabling DNSSEC generates keys and signs
- **WHEN** a SOA update changes `dnssec_wanted` from N to Y on a zone with a rendered zone file
- **THEN** ZSK+KSK are generated for each algorithm in `dnssec_algo`, the zone is signed, and `dnssec_initialized` becomes `Y`

#### Scenario: Existing keys are preserved
- **WHEN** DNSSEC create runs and alg-13 key files already exist for the domain with unchanged `dnssec_algo`
- **THEN** no `dnssec-keygen` is executed and the existing keys are reused for signing

### Requirement: Zone signing
Signing SHALL append missing `$INCLUDE <keyfile>` lines for each matching key to the zone file, run `dnssec-signzone -A -e +1382400 -3 - -N increment -o <domain> -K <keydir> -t <zonefile>` (16-day validity), and log a warning when the number of key files differs from 2 per algorithm. The zone file MUST pass `named-checkzone` before an update-triggered re-sign proceeds.

#### Scenario: Signed file produced
- **WHEN** signing runs for `example.com` with valid keys
- **THEN** `<zonefile>.signed` exists and includes NSEC3/RRSIG data valid for 16 days

#### Scenario: Broken zone blocks re-signing
- **WHEN** an update-triggered re-sign finds the zone file failing `named-checkzone`
- **THEN** signing is aborted and an error is logged

### Requirement: DS and DNSKEY publication in dnssec_info
After each successful signing the plugin SHALL write into `dns_soa.dnssec_info` a text block containing the DS records (content of `dsset-<domain>.`) and the DNSKEY records (content of the `.key` files), set `dnssec_initialized='Y'` and `dnssec_last_signed` to the current Unix time.

#### Scenario: Panel shows DS records after signing
- **WHEN** signing completes
- **THEN** `dnssec_info` starts with `DS-Records:` followed by the dsset content and contains a `DNSKEY-Records:` section

### Requirement: DNSSEC lifecycle transitions on SOA update
The plugin SHALL port the PHP decision tree: origin changed → delete old keys/signed/dsset then create when wanted; `dnssec_algo` changed → create; wanted turned on while uninitialized → create; wanted turned off while initialized → delete the `.signed` file (keys removed on zone delete); steady-state wanted → update (create-if-missing-dsset, check, re-sign).

#### Scenario: Disabling DNSSEC removes the signed file
- **WHEN** a SOA update changes `dnssec_wanted` from Y to N on an initialized zone
- **THEN** `<zonefile>.signed` is deleted and `named.conf.local` points back at the unsigned file

#### Scenario: Zone delete removes all DNSSEC materials
- **WHEN** `dns_soa_delete` fires for an initialized zone
- **THEN** all `K<domain>.+*` key files, the `.signed` file and `dsset-<domain>.` are deleted

### Requirement: Periodic re-signing scheduler job
The daemon SHALL run a named scheduler job `dns_resign` (daily) that re-signs every zone on this server with `dnssec_wanted='Y'`, `dnssec_initialized='Y'` and `dnssec_last_signed` older than a configurable threshold (default 5 days), then requests a delayed bind reload. Job runs and outcomes SHALL be observable via the daemon's job bookkeeping.

#### Scenario: Stale signature is refreshed
- **WHEN** the `dns_resign` job runs and a zone was last signed 6 days ago
- **THEN** the zone is re-signed, `dnssec_last_signed` is updated and bind is reloaded once

#### Scenario: Fresh signatures untouched
- **WHEN** the `dns_resign` job runs and all zones were signed within the threshold
- **THEN** no signing command is executed

