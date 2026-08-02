# legacy-migration-wizard Specification

## Purpose
TBD - created by archiving change add-legacy-migration. Update Purpose after archive.
Web UI wizard (System → Tools → "Migrate from ISPConfig3") over the legacy import engine, with REST endpoints under `/api/system/migration/*`.

## Requirements

### Requirement: Admin-only wizard entry
The wizard and all `/api/system/migration/*` endpoints SHALL be accessible only to admin-level panel users.

#### Scenario: Non-admin blocked
- **WHEN** a client-level user calls a migration endpoint or opens the wizard route
- **THEN** the request is rejected with a permission error

### Requirement: Connection step
The wizard SHALL collect legacy URL, remote_user name, password, and an explicit "skip TLS verification" checkbox, and provide a "Test connection" action that performs login plus the grant preflight, showing success (with legacy panel info) or the failure cause (fault code, missing grants, certificate error).

#### Scenario: Successful test
- **WHEN** valid credentials are tested
- **THEN** the wizard shows the connection as verified and enables the next step

#### Scenario: Missing grants shown
- **WHEN** the remote_user lacks required functions
- **THEN** the wizard lists the exact missing function names

### Requirement: Credentials only in session
Legacy credentials SHALL be held only in the authenticated server-side session for the duration of the wizard and SHALL never be persisted to the database, config files, or logs, nor echoed by any API response.

#### Scenario: No persistence
- **WHEN** a wizard run completes or the session ends
- **THEN** no table or file contains the legacy password, and status/progress responses never include it

### Requirement: Inventory and selection step
After a verified connection the wizard SHALL display per-entity counts (clients, web domains, DNS zones, DNS records) and legacy servers, and SHALL let the operator select which entity groups (clients, sites, dns) to import and the target local server mapping.

#### Scenario: Inventory display
- **WHEN** the inventory step loads
- **THEN** counts per entity are shown and each group can be toggled for import

#### Scenario: Multi-server legacy blocked
- **WHEN** the legacy panel reports more than one active server
- **THEN** the wizard blocks progression, states that multi-server is not supported, and only continues after the operator explicitly confirms mapping everything onto the single local server

### Requirement: Dry-run step
The wizard SHALL run a dry-run of the selection and display the plan: per-entity create/update/skip/conflict counts and a conflict list with reasons. Proceeding to execution SHALL require an explicit operator action; conflicting records SHALL be shown as excluded from the apply.

#### Scenario: Conflict report shown
- **WHEN** the dry-run finds a domain owned by a different local user
- **THEN** the conflict appears with record, key, and reason, and the execute button states that conflicts will be skipped

### Requirement: Execution with live progress
Execution SHALL run server-side (surviving page reloads) with progress exposed both as an SSE stream (`GET /api/system/migration/progress`) emitting per-entity done/total/error events and as a pollable status endpoint returning the current snapshot. Only one migration run SHALL be active at a time; starting a second SHALL be rejected while one is running.

#### Scenario: SSE progress
- **WHEN** an import of 25 domains runs
- **THEN** the SSE stream emits progress events up to 25/25 for the sites entity followed by a completion event

#### Scenario: Reload resilience
- **WHEN** the operator reloads the page mid-import
- **THEN** the wizard reattaches via the status endpoint and continues showing progress

#### Scenario: Concurrent run rejected
- **WHEN** a run is active and a second start is requested
- **THEN** the API rejects it with an already-running error

### Requirement: Final report step
On completion the wizard SHALL display the engine's final report: per-entity counts, password-reset user list, warnings (including insecure TLS and SSL re-issue), the operational order (rsync site files with uid/gid remap first, then SSL/Let's Encrypt, then DNS cutover), and the suggested rsync commands for site files, with a note that file transfer is the operator's responsibility. The report step SHALL offer a prominent **bulk password-reset action** for all reset-required users, generating one-time reset tokens/links (optionally sent by e-mail when delivery is configured).

#### Scenario: Report rendering
- **WHEN** the run finishes
- **THEN** the report shows counts, the reset-required users, and one rsync suggestion per imported domain

#### Scenario: Bulk reset from the report
- **WHEN** the operator triggers the bulk password-reset action for the listed users
- **THEN** one-time reset tokens/links are generated for every reset-required user without exposing any plaintext password
