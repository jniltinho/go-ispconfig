# firewall-rest-api

## ADDED Requirements

### Requirement: Firewall CRUD endpoints
The REST API SHALL expose firewall record operations under `/api/firewall` (declarative `RegisterEntity` surface, conventional names `firewall_add` / `firewall_get` / `firewall_update` / `firewall_delete` in docs): list (paginated, filterable), get by id, create, update, delete. Session/token authenticated, admin-only, gated by `admin_allow_firewall_config`, returning JSON. There is no ISPConfig3 remote.d firewall surface; this API is the shared write path for the panel (and any future remote client).

#### Scenario: Create firewall record
- **WHEN** `POST /api/firewall` is called by an authorized admin with valid `server_id`, port lists and `active`
- **THEN** the row is created with ownership stamps, a datalog insert is written, and the new `firewall_id` is returned

#### Scenario: List firewall records
- **WHEN** `GET /api/firewall` is called by an authorized admin
- **THEN** a paginated list of firewall rows is returned (columns include `server_id`, `tcp_port`, `udp_port`, `active`)

#### Scenario: Get by id
- **WHEN** `GET /api/firewall/{id}` is called for an existing id by an authorized admin
- **THEN** the full record is returned; 404/403 when not found or not permitted

#### Scenario: Update ports
- **WHEN** `PUT /api/firewall/{id}` changes `tcp_port` / `udp_port` / `active`
- **THEN** the row is updated, a datalog update `{old,new}` is written, and validation rules are enforced

#### Scenario: Delete record
- **WHEN** `DELETE /api/firewall/{id}` is called by an authorized admin
- **THEN** the row is removed and a datalog delete is written for that `server_id`

### Requirement: Entity form metadata for the UI
The API SHALL expose form metadata for the firewall entity (tabs/fields/validators/defaults/options) so the Vue `TabbedForm` can render without hard-coded field lists — port of the single `firewall` tab in `firewall.tform.php`. Create metadata SHALL present `server_id` options limited to servers that do not already have a firewall row (port of `firewall_edit.php::onShowEnd`).

#### Scenario: Metadata lists tform fields
- **WHEN** the firewall form metadata endpoint is requested by an authorized admin
- **THEN** it includes `server_id`, `tcp_port`, `udp_port`, `active` with the tform defaults and port regex validators

#### Scenario: Server already used is absent from create options
- **WHEN** server 1 already has a firewall row and create metadata is requested
- **THEN** server 1 is not offered as a selectable `server_id` for create

### Requirement: Swagger documentation for firewall endpoints
Every firewall endpoint SHALL carry swaggo annotations (summary, params, request/response models, security, error codes) and appear in the embedded Swagger UI; CI SHALL fail when generated swagger output is stale.

#### Scenario: Firewall endpoints visible in Swagger UI
- **WHEN** `/swagger/` is opened by an authorized admin
- **THEN** the firewall list/get/create/update/delete endpoints are listed with typed schemas

#### Scenario: Unauthorized swagger access still respects auth defaults
- **WHEN** Swagger is not public and an unauthenticated client requests the firewall operations
- **THEN** access is denied consistently with the rest of the admin API
