# api-token-jwt

## ADDED Requirements

### Requirement: Token is exchanged for a short-lived JWT
The system SHALL expose `POST /api/tokens/exchange`, authenticated by an API token, returning an HS256 JWT whose claims carry `sub` (the owner `sys_user` id), `tid` (the issuing token id), `scope` (the issuing token's scopes), `jti` (a random id) and `exp`. The JWT SHALL NOT be obtainable with a session credential.

#### Scenario: Exchange returns a signed JWT
- **WHEN** a valid, in-scope token posts to `/api/tokens/exchange`
- **THEN** the response carries a JWT whose claims match the token's owner and scopes, plus its expiry

#### Scenario: Session cannot exchange
- **WHEN** a browser session posts to `/api/tokens/exchange`
- **THEN** the request is rejected

#### Scenario: Revoked token cannot exchange
- **WHEN** a revoked or expired token posts to `/api/tokens/exchange`
- **THEN** the request is rejected with 401 and no `remote_session` row is written

### Requirement: JWT lifetime is bounded
The JWT expiry SHALL be `auth.jwt_ttl` (default 15 minutes), SHALL be capped at one hour regardless of configuration, and SHALL never exceed the issuing token's own expiry.

#### Scenario: Default TTL applies
- **WHEN** `auth.jwt_ttl` is unset and a JWT is issued
- **THEN** its `exp` is 15 minutes ahead

#### Scenario: Configured TTL above the cap is clamped
- **WHEN** `auth.jwt_ttl` is set to 24h and a JWT is issued
- **THEN** its `exp` is one hour ahead

#### Scenario: Token expiry wins when it is sooner
- **WHEN** the issuing token expires in 5 minutes and the TTL is 15 minutes
- **THEN** the JWT `exp` is 5 minutes ahead

### Requirement: JWT verification is stateless in the common path
A request presenting a JWT SHALL be authenticated by verifying the signature and `exp` without a database read, and SHALL then execute under the same owner identity and scope rules as the issuing token.

#### Scenario: Valid JWT authenticates without a token lookup
- **WHEN** an unexpired, correctly signed JWT is presented
- **THEN** the request is authenticated and no `remote_user` row is read for verification

#### Scenario: Tampered JWT is rejected
- **WHEN** a JWT with a modified `scope` claim is presented
- **THEN** the request is rejected with 401

#### Scenario: Expired JWT is rejected
- **WHEN** a JWT past its `exp` is presented
- **THEN** the request is rejected with 401

#### Scenario: Scope rules apply identically
- **WHEN** a JWT carrying `dns:read` calls a mail endpoint
- **THEN** the request is rejected as out of scope, exactly as the issuing token would be

### Requirement: Issued JWT ids are recorded and pruned
Each issued JWT SHALL be recorded in `remote_session` with its `jti`, issuing token id, caller IP and expiry, and expired rows SHALL be removed by the daemon's scheduled session sweep.

#### Scenario: Exchange records the jti
- **WHEN** a JWT is issued
- **THEN** a `remote_session` row exists carrying its `jti`, the issuing token id and the caller IP

#### Scenario: Expired rows are swept
- **WHEN** the scheduled session sweep runs
- **THEN** `remote_session` rows whose expiry has passed are deleted

### Requirement: Revocation of a token bounds outstanding JWTs
Revoking a token SHALL prevent further exchanges immediately. Outstanding JWTs issued by that token SHALL remain valid no longer than their `exp`, and the panel SHALL state this bound where the exchange setting is presented.

#### Scenario: No new JWT after revocation
- **WHEN** a token is revoked and an exchange is attempted
- **THEN** the exchange is rejected

#### Scenario: Outstanding JWT expires on its own
- **WHEN** a token is revoked while one of its JWTs is unexpired
- **THEN** that JWT stops working at its `exp` and no later

### Requirement: Signing key lifecycle
The HS256 signing key SHALL be generated at install time into `config.toml` as `[auth] jwt_secret` with `0600` permissions. When it is absent, the exchange endpoint SHALL fail with an actionable error while token authentication continues to work. Changing the key SHALL invalidate every outstanding JWT.

#### Scenario: Install generates the key
- **WHEN** `go-ispconfig install` completes
- **THEN** `config.toml` carries a random `[auth] jwt_secret` and the file is mode 0600

#### Scenario: Missing key disables only the exchange
- **WHEN** an upgraded install has no `jwt_secret` and a client calls exchange
- **THEN** the response names the missing setting, and requests authenticated by token itself still succeed

#### Scenario: Rotating the key invalidates outstanding JWTs
- **WHEN** `jwt_secret` is replaced and the panel restarts
- **THEN** every previously issued JWT is rejected with 401
