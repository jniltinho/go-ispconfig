# rest-api-core

## MODIFIED Requirements

### Requirement: Echo v5 REST API with JSON
The system SHALL expose a REST API under `/api` (Echo v5, JSON request/response) providing the CRUD framework used by all modules, replacing `interface/lib/classes/remoting.inc.php` and `remote.d/*`. Every registered route SHALL resolve to exactly one `(resource, action)` scope pair, declared once per route group at registration, so that a machine credential can be granted a subset of the surface without per-handler annotation.

#### Scenario: CRUD round trip
- **WHEN** a client POSTs a valid entity, GETs it, PUTs a change and DELETEs it
- **THEN** each operation returns the canonical JSON representation and appropriate 2xx status

#### Scenario: Every route is scoped
- **WHEN** the route table is enumerated
- **THEN** each route resolves to one resource and one action, and any route that does not fails the coverage test

#### Scenario: Session clients are unaffected by scopes
- **WHEN** a request authenticates with a session rather than a token
- **THEN** no scope check is applied and behaviour is identical to before this change

## ADDED Requirements

### Requirement: Out-of-scope requests are distinguishable
When a request carries a valid credential but the credential's scopes do not cover the route, the API SHALL respond 403 with an error key distinct from both the unauthenticated key and the record-permission-denied key.

#### Scenario: Caller can tell insufficient grant from wrong credential
- **WHEN** an enabled, in-date token calls an endpoint outside its scopes
- **THEN** the 403 body carries an error key identifying the missing scope, not the generic permission-denied key

#### Scenario: Record permission denial keeps its own key
- **WHEN** an in-scope token requests a record its owner may not read
- **THEN** the response is the existing permission-denied result, unchanged

### Requirement: Swagger documents the credential and the scope
The generated OpenAPI document SHALL state that `BearerAuth` accepts either a session id or an API token, and each endpoint SHALL carry the scope required to call it.

#### Scenario: Security definition describes both credentials
- **WHEN** the OpenAPI document is generated
- **THEN** the `BearerAuth` description names both the session id and the API token forms

#### Scenario: Endpoint documents its scope
- **WHEN** an endpoint is rendered in the Swagger UI
- **THEN** the scope required to call it with a token is stated
