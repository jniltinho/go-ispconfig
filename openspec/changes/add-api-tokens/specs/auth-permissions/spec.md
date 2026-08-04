# auth-permissions

## MODIFIED Requirements

### Requirement: Panel authentication with server-side sessions
The system SHALL authenticate sys_user credentials (bcrypt hashes) and maintain server-side sessions in `sys_session`, delivered to the SPA via secure HTTP-only cookie and to API clients via bearer token. The `Authorization: Bearer` header SHALL additionally accept an API token (`goisp_<id>_<secret>`) or a JWT issued by one; whichever credential authenticates the request, the request SHALL execute under the identity of a `sys_user` row, so every downstream permission check is unchanged.

#### Scenario: Successful login
- **WHEN** valid credentials are posted to `/api/login`
- **THEN** a session is created in `sys_session` and a session cookie is returned

#### Scenario: Migrated ISPConfig3 user logs in with legacy hash
- **WHEN** a sys_user carries an ISPConfig3 crypt hash (SHA-512 `$6$` or MD5-crypt `$1$`) and posts valid credentials
- **THEN** login succeeds; the stored hash is upgraded to bcrypt only when `auth.rehash_legacy` is enabled (default off — PHP ISPConfig cannot verify bcrypt, so eager rehash would break cutover rollback)

#### Scenario: API token authenticates as its owner
- **WHEN** a request presents a valid, enabled, in-date API token
- **THEN** it executes with the owning `sys_user`'s identity, groups and default group, exactly as that user's session would

#### Scenario: Bearer value that is neither a session nor a token
- **WHEN** a request presents a bearer value matching no session and no token
- **THEN** the request is rejected with 401 without revealing which credential type was attempted

#### Scenario: Session credential keeps working unchanged
- **WHEN** the SPA authenticates with the `goisp_session` cookie
- **THEN** behaviour is identical to before this change

### Requirement: CSRF protection on mutating endpoints
Browser sessions SHALL use SameSite=Strict HTTP-only cookies and every mutating endpoint (POST/PUT/DELETE) SHALL require a valid per-session CSRF token; requests without it are rejected with 403. A request authenticated by an API token or a JWT SHALL be exempt from the CSRF token requirement, because an `Authorization` header is never attached ambiently by a browser and therefore cannot be forged cross-site. The exemption SHALL key off how the request actually authenticated, not off a header the caller supplies.

#### Scenario: Cross-site request rejected
- **WHEN** a mutating request arrives with a valid session cookie but no/invalid CSRF token
- **THEN** the API returns 403 and no change or datalog entry occurs

#### Scenario: Token request needs no CSRF token
- **WHEN** a mutating request authenticates with an API token and carries no `X-CSRF-Token`
- **THEN** the request proceeds

#### Scenario: Cookie request cannot borrow the exemption
- **WHEN** a mutating request carries a valid session cookie and an unrelated `Authorization` header, and no valid CSRF token
- **THEN** the request is rejected with 403
