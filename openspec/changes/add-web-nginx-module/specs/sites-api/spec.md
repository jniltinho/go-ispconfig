# sites-api

## ADDED Requirements

### Requirement: Web domain CRUD endpoints
The REST API SHALL expose CRUD endpoints for `web_domain` under `/api/sites/web-domains` (list, get, create, update, delete), functionally equivalent to `sites_web_domain_add/get/update/delete` in `remote.d/sites.inc.php`. Every write SHALL run the form validators, apply defaults (document_root, system_user/system_group derived as in ISPConfig when not provided), persist the record and write the `{old,new}` JSON diff to `sys_datalog` in the same transaction. Every read/write SHALL be scoped by the riud permission scope of the authenticated user. All endpoints SHALL carry swaggo annotations.

#### Scenario: Create a web domain
- **WHEN** an authorized client POSTs a valid vhost domain payload
- **THEN** the API returns 201 with the record, the `web_domain` row exists and one `sys_datalog` row with `action=i` and JSON `{old,new}` exists for it

#### Scenario: Validation failure
- **WHEN** a POST omits `domain` or supplies an invalid domain name
- **THEN** the API returns 422 with per-field validator errors and neither the record nor a datalog row is written

#### Scenario: Blacklisted nginx directive rejected at save
- **WHEN** a create/update payload's `nginx_directives` contains a line matching the embedded `security/nginx_directives.blacklist`
- **THEN** the API returns 422 with a per-field error naming the offending directive and neither the record nor a datalog row is written (the render-time strip of the daemon remains as defense in depth)

#### Scenario: Cross-client access denied
- **WHEN** a client-level user requests a domain owned by another client group
- **THEN** the API returns 404/403 according to the riud scope and no data leaks

### Requirement: Web folder and folder user endpoints
The REST API SHALL expose CRUD endpoints for `web_folder` and `web_folder_user` (protected folders and their users), with the same validation, riud scoping and datalog behavior; folder user passwords SHALL be stored crypted, never in plain text.

#### Scenario: Add a protected folder user
- **WHEN** an authorized client creates a folder user with a plain password
- **THEN** the stored password is a crypt hash and a datalog row triggers the auth-file rebuild on the daemon side

### Requirement: Form metadata endpoint for the sites form
The API SHALL expose the web-domain form descriptor (tabs Domain, Redirect, SSL, Statistics, Backup, Options with fields, types, options, defaults and validator hints, port of `web_vhost_domain.tform.php`) as JSON for the frontend form renderer, filtered by the user's access level (admin-only fields hidden for clients, matching ISPConfig behavior).

#### Scenario: Client-level metadata hides admin fields
- **WHEN** a client-level user fetches the web-domain form metadata
- **THEN** admin-only fields (e.g. server_id, ip_address at admin discretion) are absent while client-editable fields are present

### Requirement: Datalog error visibility
The API SHALL expose per-record pending/failed datalog state (port of ISPConfig's datalog error surface) so the UI can show that a change is still being applied or failed on the server side with the recorded error text.

#### Scenario: Failed nginx reload surfaces to the API
- **WHEN** the daemon recorded a datalog error for a domain's vhost update
- **THEN** a GET of that domain includes the error state and message
