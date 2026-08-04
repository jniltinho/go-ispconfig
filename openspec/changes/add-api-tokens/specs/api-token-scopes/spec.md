# api-token-scopes

## ADDED Requirements

### Requirement: Scope grammar
A scope SHALL be `<resource>:<action>` where `<action>` is `read`, `write` or `*`, and `<resource>` is one of the API resource groups (`sites`, `mail`, `dns`, `clients`, `system`, `monitor`, `server`) or `*`. `write` SHALL imply `read`. A token SHALL carry at least one scope.

#### Scenario: Read scope covers GET only
- **WHEN** a token holding `sites:read` issues `GET /api/sites/web-domains`
- **THEN** the request is in scope

#### Scenario: Read scope does not cover a mutation
- **WHEN** the same token issues `POST /api/sites/web-domains`
- **THEN** the request is rejected as out of scope

#### Scenario: Write implies read
- **WHEN** a token holding only `mail:write` issues `GET /api/mail/domains`
- **THEN** the request is in scope

#### Scenario: Wildcards match
- **WHEN** a token holding `*:read` issues a GET against any resource group
- **THEN** the request is in scope

#### Scenario: Empty scope list is refused at creation
- **WHEN** a token is created with no scopes
- **THEN** the creation fails with a validation error

#### Scenario: Unknown scope string is refused at creation
- **WHEN** a token is created with `sites:delete` or `nosuch:read`
- **THEN** the creation fails with a validation error naming the offending scope

### Requirement: Every route resolves to a scope
Each route group SHALL declare the resource its endpoints belong to at registration, so that every registered route resolves to exactly one `(resource, action)` pair without per-handler annotation.

#### Scenario: A new endpoint inherits its group's scope
- **WHEN** a new endpoint is added to an already-scoped route group
- **THEN** it is covered by that group's resource with the action derived from its HTTP method

#### Scenario: Unscoped route fails the build
- **WHEN** a route group is registered without declaring a resource
- **THEN** the route-coverage test fails naming the unscoped routes

### Requirement: Scopes attenuate, never widen
A request authenticated by token SHALL execute with the owner's identity, and the owner's row permissions, `sys_user.typ`, entity `AdminOnly` gating and every security policy SHALL apply unchanged. A scope SHALL only be able to deny an operation the owner could otherwise perform.

#### Scenario: Token cannot exceed a non-admin owner
- **WHEN** a token owned by a client user holds `system:*` and calls an admin-only endpoint
- **THEN** the request is rejected with 403 exactly as the owner's own session would be

#### Scenario: Token narrows an admin owner
- **WHEN** a token owned by an admin holds only `dns:read` and calls `DELETE /api/clients/5`
- **THEN** the request is rejected as out of scope even though the owner could perform it

#### Scenario: Row permissions still apply
- **WHEN** a token owned by a client holds `sites:read` and lists web domains
- **THEN** it sees exactly the rows the owner's session would see

#### Scenario: Disabled owner disables the token
- **WHEN** the owning `sys_user` has `active = 0`
- **THEN** every request authenticated by its tokens is rejected with 401

### Requirement: Out-of-scope is distinguishable from unauthenticated
When a request carries a valid credential but lacks the required scope, the system SHALL respond 403 with an error key distinct from the unauthenticated and the permission-denied keys, so a caller can tell "wrong credential" from "insufficient grant".

#### Scenario: Missing scope reports its own key
- **WHEN** an in-date, enabled token calls an endpoint outside its scopes
- **THEN** the response is 403 with an error key identifying the missing scope

#### Scenario: Missing credential reports unauthenticated
- **WHEN** a request carries no credential at all
- **THEN** the response is 401
