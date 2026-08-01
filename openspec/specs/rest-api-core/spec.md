# rest-api-core Specification

## Purpose
TBD - created by archiving change port-ispconfig3-to-go. Update Purpose after archive.
## Requirements
### Requirement: Echo v5 REST API with JSON
The system SHALL expose a REST API under `/api` (Echo v5, JSON request/response) providing the CRUD framework used by all modules, replacing `interface/lib/classes/remoting.inc.php` and `remote.d/*`.

#### Scenario: CRUD round trip
- **WHEN** a client POSTs a valid entity, GETs it, PUTs a change and DELETEs it
- **THEN** each operation returns the canonical JSON representation and appropriate 2xx status

### Requirement: Fully documented API with embedded Swagger UI
Every API endpoint SHALL carry swaggo annotations (@Summary, @Param, @Success, @Failure, @Router, @Security); the generated OpenAPI spec and Swagger UI SHALL be embedded in the binary and served at `/swagger/`, allowing the entire API to be exercised interactively. All exported Go identifiers SHALL have godoc comments (lint-enforced). CI SHALL fail when the generated swagger spec is stale.

#### Scenario: Test an endpoint from Swagger UI
- **WHEN** an operator opens `/swagger/`, authenticates, and executes any documented endpoint
- **THEN** the request succeeds against the live API and the documented schema matches the actual response

#### Scenario: Undocumented endpoint blocked
- **WHEN** a handler is added without swaggo annotations or an exported symbol lacks a godoc comment
- **THEN** the lint/CI pipeline fails

### Requirement: Declarative validation mirroring tform validators
Entity definitions SHALL declare per-field validators from the ported set: REGEX, UNIQUE, NOTEMPTY, ISEMAIL, ISINT, ISPOSITIVE, ISIPV4, ISIPV6, ISIP, CUSTOM. Validation failures SHALL return 422 with a per-field error map using i18n message keys.

#### Scenario: Invalid IP rejected
- **WHEN** a record is submitted with `ip_address = "999.1.1.1"` on a field validated ISIPV4
- **THEN** the API returns 422 with an error keyed to that field

#### Scenario: UNIQUE enforced
- **WHEN** a second record with the same value is submitted for a field validated UNIQUE
- **THEN** the API returns 422 and the record is not created

### Requirement: Client limit hook point
The CRUD framework SHALL expose a limit-check hook invoked before every create operation, receiving the entity type and the requesting user's client context. In this change the hook is a registered no-op; `add-client-module` later plugs `limit_*` enforcement without modifying module endpoints.

#### Scenario: Hook invoked on create
- **WHEN** any entity create passes validation
- **THEN** the limit hook runs before the record is written and can veto the operation with a 403 and an i18n error key

### Requirement: Form metadata endpoint
The API SHALL expose the field/validator/tab metadata of each entity as JSON so the SPA renders forms from the same source of truth used for validation.

#### Scenario: Frontend fetches form definition
- **WHEN** the SPA requests `/api/meta/forms/dns_soa`
- **THEN** it receives tabs, fields, types, defaults and validator hints for rendering

