# dns-rest-api Specification

## Purpose
TBD - created by archiving change add-dns-bind-module. Update Purpose after archive.
## Requirements
### Requirement: Zone endpoints
The REST API SHALL expose zone operations porting `remote.d/dns.inc.php`: create, get by id, get id by origin, list by user/server, update, delete, set status (active/inactive) and set DNSSEC (wanted + algorithm) — `dns_zone_add/get/get_id/get_by_user/update/delete/set_status/set_dnssec` semantics — under `/api/dns/zones`, session/token authenticated, permission-scoped, returning JSON.

#### Scenario: Create zone
- **WHEN** `POST /api/dns/zones` is called with valid SOA fields by an authorized user
- **THEN** the zone is created with the caller's ownership fields, a datalog row is written and the new id returned

#### Scenario: Lookup id by origin
- **WHEN** the zone-id-by-origin endpoint is called with an existing origin (with or without trailing dot)
- **THEN** the zone id is returned; 404 when no accessible zone matches

### Requirement: Record endpoints for all supported types
The REST API SHALL expose record CRUD for every supported type (A, AAAA, ALIAS, CAA, CNAME, DNAME, DS, HINFO, LOC, MX, NAPTR, NS, PTR, RP, SRV, SSHFP, TLSA, TXT) — porting the per-type `dns_<type>_add/get/update/delete` wrappers over generic `dns_rr_*` — plus a list endpoint returning all records of a zone (`dns_rr_get_all_by_zone`). Mutations SHALL accept `update_serial` (default true) and bump the SOA serial accordingly. A single typed surface (e.g., `/api/dns/zones/:id/records` with a `type` discriminator) satisfies this requirement as long as every type is validated by its own rules.

#### Scenario: Add A record to a zone
- **WHEN** `POST /api/dns/zones/42/records` is called with `type=A`, valid name/data/ttl
- **THEN** the record is created inheriting the zone's `server_id`, the SOA serial is bumped and datalog rows are written for both

#### Scenario: List records of a zone
- **WHEN** the records list endpoint is called for an accessible zone
- **THEN** all `dns_rr` rows of that zone are returned ordered by type, name

#### Scenario: Record in inaccessible zone
- **WHEN** a record mutation targets a zone the caller cannot update
- **THEN** the API returns 403

### Requirement: Secondary zone and template endpoints
The REST API SHALL expose `dns_slave` CRUD (`dns_slave_add/get/update/delete` semantics), template listing (`dns_templatezone_get_all` — visible templates), template CRUD for admin, and wizard-based zone creation from a template (`dns_templatezone_add` semantics: template id + client + DOMAIN/IP/IPV6/NS1/NS2/EMAIL values).

#### Scenario: Create zone from template
- **WHEN** the wizard endpoint is called with a template id and complete field values
- **THEN** the zone and its records are created atomically and the new zone id is returned

#### Scenario: Hidden template not listed
- **WHEN** templates are listed by a non-admin
- **THEN** templates with `visible = 'n'` are absent

### Requirement: Swagger documentation for all DNS endpoints
Every DNS endpoint SHALL carry swaggo annotations (summary, params, request/response models, security, error codes) and appear in the embedded Swagger UI; CI SHALL fail when generated swagger output is stale.

#### Scenario: DNS endpoints visible in Swagger UI
- **WHEN** `/swagger/` is opened
- **THEN** the DNS zone, record, slave and template endpoints are listed with typed request/response schemas

