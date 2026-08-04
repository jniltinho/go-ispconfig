# api-tokens

## ADDED Requirements

### Requirement: Token format and one-time display
An API token SHALL be minted as `goisp_<remote_userid>_<secret>`, where `<secret>` is 32 bytes from `crypto/rand` encoded base64url without padding. The plaintext SHALL be returned exactly once, in the response of the create call, and SHALL NOT be stored, logged, or returned by any subsequent read.

#### Scenario: Create returns the plaintext once
- **WHEN** an admin creates a token
- **THEN** the 201 response body carries the full `goisp_<id>_<secret>` string

#### Scenario: Reading the token afterwards never reveals the secret
- **WHEN** the same token is fetched by list or by id
- **THEN** no field of the response contains the secret or its digest

#### Scenario: The secret never reaches the log
- **WHEN** a token is created and the request is logged
- **THEN** no log line contains the secret

### Requirement: Digest-only storage
The system SHALL store only the hex-encoded SHA-256 digest of the secret in `remote_user.remote_password`, and SHALL verify a presented token by primary-key lookup on the embedded id followed by a constant-time comparison of digests.

#### Scenario: Stored value is a digest
- **WHEN** a token has been created
- **THEN** `remote_user.remote_password` holds a 64-character hex digest and not the secret

#### Scenario: Wrong secret is rejected
- **WHEN** a request presents a valid token id with an incorrect secret
- **THEN** the request is rejected with 401 and the response does not distinguish it from an unknown id

#### Scenario: Unknown id is rejected identically
- **WHEN** a request presents a token id that does not exist
- **THEN** the request is rejected with 401 with the same body and timing class as a wrong secret

### Requirement: Token metadata is parsed tolerantly from remote_functions
The system SHALL encode scopes, expiry and last-used in `remote_user.remote_functions` as `scopes=<csv>;expires=<RFC3339>;last_used=<RFC3339>`, and SHALL parse a value that carries none of those keys as a bare scope CSV with no expiry.

#### Scenario: Structured value round trips
- **WHEN** a token with scopes and an expiry is saved and read back
- **THEN** the parsed scopes and expiry equal what was written

#### Scenario: Legacy bare CSV is accepted
- **WHEN** `remote_functions` holds `sites:read,mail:*` with no metadata keys
- **THEN** it parses as those two scopes with no expiry and the token is usable

#### Scenario: Unparseable metadata does not grant access
- **WHEN** `remote_functions` cannot be parsed into at least one scope
- **THEN** every request authenticated by that token is rejected with 403

### Requirement: Revocation, expiry and enablement
A token SHALL be refused when `remote_user.remote_access` is not `y`, or when its parsed `expires` is in the past. Revocation SHALL take effect on the next request without restarting the panel.

#### Scenario: Revoked token stops working immediately
- **WHEN** an admin revokes a token and the token is presented on the next request
- **THEN** the request is rejected with 401

#### Scenario: Expired token is refused
- **WHEN** a token whose `expires` has passed is presented
- **THEN** the request is rejected with 401 and the token is reported as expired in the panel

#### Scenario: Token without an expiry keeps working
- **WHEN** a token was created with no expiry
- **THEN** it authenticates until it is revoked

### Requirement: IP allow-list
When `remote_user.remote_ips` is non-empty, the system SHALL accept the token only from a caller IP matching one of its comma-separated IP or CIDR entries, resolving the caller IP through the same trusted-proxy chain the request logger uses. An empty list SHALL mean any IP.

#### Scenario: Caller outside the allow-list is refused
- **WHEN** a token restricted to `10.0.0.0/8` is presented from `203.0.113.5`
- **THEN** the request is rejected with 401

#### Scenario: Caller behind a trusted proxy is matched on its real address
- **WHEN** the panel runs behind a configured trusted proxy and the forwarded client IP is inside the allow-list
- **THEN** the request is accepted

#### Scenario: Empty allow-list accepts any address
- **WHEN** a token has an empty `remote_ips`
- **THEN** it authenticates from any caller IP

### Requirement: Last-used tracking is rate limited
The system SHALL record the time of the most recent successful authentication of each token, and SHALL write that value at most once per minute per token.

#### Scenario: First use records the timestamp
- **WHEN** a freshly created token authenticates a request
- **THEN** its `last_used` becomes that request's time

#### Scenario: Rapid reuse does not write on every request
- **WHEN** a token authenticates 100 requests inside one minute
- **THEN** at most one `last_used` write is issued

### Requirement: Token is only accepted from the Authorization header
The system SHALL accept an API token only as `Authorization: Bearer <token>`, and SHALL NOT accept it as a query parameter, form field, or cookie.

#### Scenario: Query parameter is not a credential
- **WHEN** a request carries a valid token as `?token=` and no Authorization header
- **THEN** the request is treated as unauthenticated

### Requirement: Failed token attempts are throttled
The system SHALL count failed token verifications per caller IP and SHALL throttle further attempts from that IP once a threshold is exceeded, without affecting requests authenticated by a session.

#### Scenario: Brute force is slowed
- **WHEN** an IP submits repeated invalid tokens past the threshold
- **THEN** subsequent token attempts from that IP are rejected with 429 until the window elapses

#### Scenario: Panel login is unaffected
- **WHEN** an IP is throttled for token attempts
- **THEN** a browser session login from that IP still succeeds

### Requirement: CLI manages tokens without a browser
The system SHALL provide `go-ispconfig token create`, `token list` and `token revoke`, reading the local `config.toml` for the database, so an unattended install can mint the first automation credential and an operator locked out of the panel can revoke one.

#### Scenario: Create prints the token once
- **WHEN** `go-ispconfig token create --owner admin --scopes sites:read` runs
- **THEN** the plaintext token is printed to stdout and the row is stored with its digest

#### Scenario: List never prints secrets
- **WHEN** `go-ispconfig token list` runs
- **THEN** it prints id, label, owner, scopes, expiry, last used and enabled state, and no secret or digest

#### Scenario: Revoke disables the token
- **WHEN** `go-ispconfig token revoke <id>` runs
- **THEN** `remote_access` becomes `n` and the next request with that token is rejected
