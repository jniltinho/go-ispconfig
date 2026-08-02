# client-rest-api

## ADDED Requirements

### Requirement: Client endpoints
The REST API SHALL expose client operations porting `remote.d/client.inc.php` under `/api/clients` (session/token authenticated, permission-scoped, JSON): create, get by id, list (paginated, filterable), update, delete, delete-everything, get-by-username, get-by-customer-no, get-by-groupid, get client id from sys_userid / groupid helpers, change-password, and status helpers for locked/canceled where not folded into update. Create/update accept the client field set from `client.tform.php` including limits and `parent_client_id`.

#### Scenario: Create client
- **WHEN** `POST /api/clients` is called with valid contact, username, password and limits by an authorized admin
- **THEN** the client and linked sys_user/sys_group are created, a datalog row is written, and the new `client_id` is returned

#### Scenario: Lookup by username
- **WHEN** the get-by-username endpoint is called with an existing accessible username
- **THEN** the client record is returned without password fields; 404 when not found or not accessible

#### Scenario: Change password
- **WHEN** the change-password endpoint is called with a new password meeting policy
- **THEN** `client.password` and `sys_user.passwort` are updated to the new hash and `last_password_change` is set

### Requirement: Reseller endpoints
The API SHALL expose reseller list/create/update/delete semantics (either under `/api/resellers` or as filtered `/api/clients` with `limit_client != 0` — swagger MUST document one clear surface). Reseller create forces reseller rules (`limit_client != 0`, modules include `client`). Nested reseller under reseller is rejected.

#### Scenario: List resellers
- **WHEN** an admin lists resellers
- **THEN** only rows with `limit_client != 0` are returned

#### Scenario: Nested reseller rejected
- **WHEN** a reseller create sets `parent_client_id` to another reseller
- **THEN** the API returns a validation/business error and nothing is created

### Requirement: Template and assignment endpoints
The API SHALL expose `client_template` CRUD and list (`client_templates_get_all` semantics), plus additional-template get/add/delete for a client (`client_template_additional_get/add/delete`). Assigning or changing master/additional templates SHALL trigger materialization onto the client limits in the same transaction.

#### Scenario: List templates
- **WHEN** templates are listed by an authorized user
- **THEN** accessible `client_template` rows are returned with type and limits

#### Scenario: Add additional template to client
- **WHEN** the additional-template add endpoint is called with a valid template id
- **THEN** a `client_template_assigned` row is created and client limits are re-materialized

### Requirement: Messaging endpoints
The API SHALL expose message-template CRUD under a documented path (e.g. `/api/client-message-templates`) and a send-message endpoint porting `client_message.php` (recipients, subject, body, optional template id). Countries SHALL be available read-only for forms (`GET` list of `country`).

#### Scenario: Send message to one client
- **WHEN** send-message is called with a client id, subject and body and SMTP is configured
- **THEN** one email is submitted for that client's address

#### Scenario: Countries list
- **WHEN** `GET` countries is called by an authenticated user
- **THEN** iso/printable_name pairs from `country` are returned ordered by printable name

### Requirement: Swagger documentation for all client endpoints
Every client-module endpoint SHALL carry swaggo annotations (summary, params, request/response models, security, error codes) and appear in the embedded Swagger UI; CI SHALL fail when generated swagger output is stale.

#### Scenario: Client endpoints visible in Swagger UI
- **WHEN** `/swagger/` is opened
- **THEN** client, reseller, template, message-template and send-message endpoints are listed with typed request/response schemas

### Requirement: Limit errors and validation errors use standard envelopes
Validation failures SHALL return HTTP 422 with per-field i18n keys. Limit vetoes from the registered hook SHALL return HTTP 403 with `error.limit_*` keys. Permission failures SHALL return 403 with `error.permission_denied` (or the foundation equivalent).

#### Scenario: Validation envelope on bad username
- **WHEN** create is called with an empty username
- **THEN** the response is 422 and `error.fields.username` contains an i18n key
