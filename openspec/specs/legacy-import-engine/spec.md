# legacy-import-engine Specification

## Purpose
TBD - created by archiving change add-legacy-migration. Update Purpose after archive.
Maps legacy ISPConfig3 entities (fetched via legacy-api-client) into the local go-ispconfig database: clients, web domains, DNS zones/records. Plan/apply with dry-run, idempotent by natural key, datalog-emitting.

## Requirements

### Requirement: Inventory
The engine SHALL produce an inventory of the legacy panel: per-entity record counts for clients, web domains, web folders, web folder users, DNS zones, DNS records, DNS slave zones and DNS templates, plus the legacy server list. The inventory SHALL be computable without writing to the local database.

#### Scenario: Inventory counts
- **WHEN** the legacy panel holds 10 clients, 25 web domains, and 8 zones with 120 records
- **THEN** the inventory reports exactly those counts per entity

### Requirement: Two-phase plan and apply
The engine SHALL first build a plan classifying every fetched record as `create`, `update`, `skip-identical`, or `conflict` using only local reads. In dry-run mode the engine SHALL stop after the plan and SHALL perform no local writes. In apply mode the engine SHALL execute exactly the planned actions, skipping records planned as `conflict`.

#### Scenario: Dry-run writes nothing
- **WHEN** a dry-run is executed against a local database
- **THEN** no row in any local table is inserted, updated, or deleted, and a plan report is returned

#### Scenario: Apply follows the plan
- **WHEN** apply runs after a plan with 5 creates, 2 updates, 3 skips
- **THEN** exactly 5 rows are inserted and 2 updated for those entities

### Requirement: Idempotent upsert by natural key
The engine SHALL match existing local records by natural key — client by `username`; sys_user by `username` and sys_group by `name`; web_domain by `(domain, type)`; web_folder by `(parent_domain_id, path)`; web_folder_user by `(web_folder_id, username)`; dns_soa by `origin`; dns_slave by `origin`; dns_template by `name`; dns_rr by `(zone, name, type, data)` — and SHALL update-or-skip matches instead of inserting duplicates. Running the same import twice SHALL leave the local database with no duplicated records and classify all records as `skip-identical` on the second run.

#### Scenario: Re-run does not duplicate
- **WHEN** the same import is applied twice
- **THEN** local record counts are identical after the first and second run and the second plan contains only `skip-identical` entries

#### Scenario: Changed legacy record updates local
- **WHEN** a web domain was imported and its legacy `document_root` changed before a re-run
- **THEN** the re-run plans that domain as `update` and apply changes only that field set

### Requirement: ID remapping preserving ownership and permissions
The engine SHALL maintain a mapping `{entity, legacy_id} → local_id` and rewrite all foreign keys through it (client links, `web_domain.parent_domain_id`, `dns_rr.zone`, `server_id` mapped to the selected local server). Clients SHALL be imported in `parent_client_id` dependency order — resellers before the clients that reference them — with `parent_client_id` rewritten through the mapping so the reseller hierarchy is preserved. Each imported client SHALL get a local sys_user and sys_group (recreated from client data, as ISPConfig does), and imported records SHALL carry their legacy `sys_perm_user`, `sys_perm_group`, `sys_perm_other` riud strings verbatim with `sys_userid`/`sys_groupid` rewritten to the mapped local ids.

#### Scenario: Ownership follows the remap
- **WHEN** legacy client 7 (sys_groupid 8) owns a web domain and is imported as local client 3 (sys_groupid 4)
- **THEN** the imported web domain has sys_groupid 4 and its legacy riud strings unchanged

#### Scenario: DNS record follows its zone
- **WHEN** a dns_rr references legacy zone id 12 which maps to local dns_soa id 5
- **THEN** the imported dns_rr has `zone = 5`

#### Scenario: Reseller imported before its clients
- **WHEN** legacy client 9 has `parent_client_id = 4` (a reseller) and both are selected
- **THEN** the reseller is imported first and the imported client 9 references the reseller's new local id

### Requirement: SSL fields imported with re-issue caveat
The engine SHALL import `web_domain` SSL fields (`ssl`, `ssl_letsencrypt` and the certificate/key/bundle text fields) as returned by the API, and the plan and final report SHALL carry an explicit warning that certificates must be re-issued on the new host: legacy key/cert material and file paths are not assumed valid, and Let's Encrypt issuance is expected to run on the new host after site files are transferred.

#### Scenario: SSL site imported with warning
- **WHEN** a web domain with `ssl=y` and `ssl_letsencrypt=y` is imported
- **THEN** the fields are stored and the report warns that the certificate must be re-issued on the new host

### Requirement: Password hash import or reset flag
The engine SHALL store crypt password hashes (`$1$`, `$5$`, `$6$` prefixes) found in fetched records verbatim. Panel login hashes are never importable — the remote API does not expose `sys_user.passwort` — so every recreated panel sys_user SHALL be assigned an unusable placeholder and marked as requiring a password reset; the report SHALL list all such users, and the engine SHALL support generating one-time password-reset tokens for all flagged users in bulk (consumed by the wizard and CLI reset flows). The engine SHALL never store or log a plaintext password.

#### Scenario: Crypt hash imported verbatim
- **WHEN** a fetched record contains a `$6$...` hash in a password field
- **THEN** the local record stores the identical hash string

#### Scenario: Missing hash flags reset
- **WHEN** a client's panel login hash is not exposed by the API
- **THEN** the recreated sys_user cannot log in with any password and appears in the report's reset list

### Requirement: Conflict detection
The plan SHALL classify as `conflict` (and apply SHALL skip): a natural-key match owned by a different local owner, a record referencing a legacy server with no mapped local server, and a child record whose parent (web domain parent, DNS zone) is neither local nor part of the plan. Each conflict SHALL name the record, the natural key, and the reason.

#### Scenario: Same domain, different owner
- **WHEN** the local panel already has `example.com` owned by another client
- **THEN** the plan marks the legacy `example.com` as `conflict` with reason "owned by different user" and apply does not touch it

#### Scenario: Orphan DNS record
- **WHEN** a dns_rr's zone is excluded from the import selection and absent locally
- **THEN** the record is planned as `conflict` with an unmapped-zone reason

### Requirement: Datalog emission on apply
Every insert and update performed by apply SHALL be written through the datalog-aware writer, producing `sys_datalog` rows (JSON `{old,new}`, correct `dbtable`, `dbidx`, action `i`/`u`, mapped `server_id`) so the daemon materializes vhosts and zone files. Dry-run SHALL emit no datalog rows.

#### Scenario: Imported zone reaches the daemon
- **WHEN** a dns_soa and its dns_rr rows are applied
- **THEN** sys_datalog contains corresponding rows and processing them triggers zone file generation for the local DNS server

### Requirement: Entity selection with dependency order
The engine SHALL accept a selection subset of `clients`, `sites`, `dns` and SHALL import in dependency order (clients before sites/dns). When an owning client is excluded and absent locally, dependent records SHALL be planned as `conflict`, except that DNS zones MAY be assigned to admin (sys_userid 1) when explicitly requested.

#### Scenario: Sites without clients
- **WHEN** the selection is `sites` only and a domain's owner does not exist locally
- **THEN** that domain is planned as `conflict` with a missing-owner reason

### Requirement: Final report
Apply SHALL produce a report containing: per-entity created/updated/skipped/conflict counts, the password-reset user list, warnings (insecure TLS, unmapped servers, SSL re-issue), and a suggested `rsync` command per imported web domain for site files (file transfer itself is out of scope). The rsync suggestions SHALL include uid/gid remapping — `--usermap`/`--groupmap` options or an explicit per-site post-rsync `chown -R system_user:system_group` — because `system_user`/`system_group` ids differ on the new host. The report SHALL state the operational order: transfer site files first, then enable SSL/Let's Encrypt issuance (the webroot challenge fails on an empty docroot), then DNS cutover.

#### Scenario: Report after apply
- **WHEN** an apply finishes with 3 domains imported
- **THEN** the report lists counts per entity and 3 rsync command suggestions with the legacy host, each document_root, and uid/gid remapping for the site's local system user/group
