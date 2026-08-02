# dns-panel-ui Specification

## Purpose
TBD - created by archiving change add-dns-bind-module. Update Purpose after archive.
## Requirements
### Requirement: DNS module navigation and zone list
The panel SHALL show a DNS module (visible per user permissions) with a zone list (origin, NS, active status, server; search on origin/ns/mbox), a secondary-zones list, and entry points for "Add zone" (wizard) and "Add zone manually". All strings SHALL go through the i18n layer (en first).

#### Scenario: Zone list shows only accessible zones
- **WHEN** a client opens the DNS zone list
- **THEN** only zones readable under the riud scope are listed with their status

### Requirement: Zone form with tabs
The zone form SHALL mirror `dns_soa.tform.php`: tab **Records** (default) with the embedded record grid; tab **Zone settings** with SOA fields (server, origin, ns, mbox, serial read-only, refresh/retry/expire/minimum/ttl, xfer, also_notify, active, DNSSEC wanted/algorithm, DNSSEC info read-only) where `update_acl` is rendered for admins only; tab **Zone rendering** showing read-only `rendered_zone` when the global zone-export option is enabled. Client-side validation SHALL mirror the API rules and API validation errors SHALL be displayed per field.

#### Scenario: update_acl hidden from clients
- **WHEN** a non-admin opens Zone settings
- **THEN** no `update_acl` input is rendered

#### Scenario: DNSSEC info after signing
- **WHEN** an admin opens a DNSSEC-signed zone
- **THEN** the DS/DNSKEY text from `dnssec_info` is shown read-only

### Requirement: Embedded record editor grid
The Records tab SHALL show all records of the zone in a grid ordered by type then name (columns: name, type, data, aux/priority where relevant, TTL, active), with add/edit via a dialog whose fields, placeholders and validation are driven by the per-type metadata exported by the API (A, AAAA, ALIAS, CAA, CNAME, DNAME, DS, HINFO, LOC, MX, NAPTR, NS, PTR, RP, SRV, SSHFP, TLSA, TXT and the TXT-derived SPF/DKIM/DMARC helpers), plus delete and active-toggle actions. Every mutation SHALL refresh the grid and reflect the bumped SOA serial.

#### Scenario: Add MX record via dialog
- **WHEN** the user adds an MX record with priority and mail server
- **THEN** the dialog shows type-specific fields (priority as aux), validates the hostname, and the grid shows the new row after save

#### Scenario: Type-specific validation error shown inline
- **WHEN** the user submits an A record with an invalid IPv4
- **THEN** the dialog shows the field error and no request-level state is lost

### Requirement: Zone wizard from templates
The "Add zone" flow SHALL list visible `dns_template` entries and render inputs only for the placeholders declared in the template's `fields` (DOMAIN, IP, IPV6, NS1, NS2, EMAIL, plus DKIM/DNSSEC toggles), then create the zone through the wizard endpoint and navigate to the new zone's form.

#### Scenario: Wizard creates and opens zone
- **WHEN** the user completes the wizard with the Default template
- **THEN** the zone is created and the zone form opens on the Records tab showing the template's records

### Requirement: Secondary zone and template management screens
The panel SHALL provide a secondary zone form (server, origin, master NS IPs, xfer, active) and, for admins, a template management screen (name, fields selection, template text, visible).

#### Scenario: Client creates a secondary zone
- **WHEN** an authorized user saves a valid secondary zone
- **THEN** it appears in the secondary-zones list as active

### Requirement: E2E coverage of the DNS UI
agent-browser E2E tests SHALL cover: zone creation via wizard, manual zone creation, adding/editing/deleting an A, MX and TXT record through the grid, secondary zone creation, and the admin-only visibility of `update_acl`.

#### Scenario: E2E suite passes against a seeded panel
- **WHEN** the DNS E2E suite runs against a dev server with seeded data
- **THEN** all listed flows complete without errors

