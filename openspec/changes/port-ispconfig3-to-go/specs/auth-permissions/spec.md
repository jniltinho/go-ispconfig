# auth-permissions

## ADDED Requirements

### Requirement: Panel authentication with server-side sessions
The system SHALL authenticate sys_user credentials (bcrypt hashes) and maintain server-side sessions in `sys_session`, delivered to the SPA via secure HTTP-only cookie and to API clients via bearer token.

#### Scenario: Successful login
- **WHEN** valid credentials are posted to `/api/login`
- **THEN** a session is created in `sys_session` and a session cookie is returned

#### Scenario: Migrated ISPConfig3 user logs in with legacy hash
- **WHEN** a sys_user carries an ISPConfig3 crypt hash (SHA-512 `$6$` or MD5-crypt `$1$`) and posts valid credentials
- **THEN** login succeeds; the stored hash is upgraded to bcrypt only when `auth.rehash_legacy` is enabled (default off — PHP ISPConfig cannot verify bcrypt, so eager rehash would break cutover rollback)

### Requirement: CSRF protection on mutating endpoints
Browser sessions SHALL use SameSite=Strict HTTP-only cookies and every mutating endpoint (POST/PUT/DELETE) SHALL require a valid per-session CSRF token; requests without it are rejected with 403.

#### Scenario: Cross-site request rejected
- **WHEN** a mutating request arrives with a valid session cookie but no/invalid CSRF token
- **THEN** the API returns 403 and no change or datalog entry occurs

### Requirement: Security policy flags
The security flags from ISPConfig's `security/security_settings.ini` (e.g. `allow_shell_user`, `admin_allow_server_config`, `remote_api_allowed`) SHALL be stored in `sys_config` and enforced by the API layer, with `superadmin` meaning sys_user id 1 only.

#### Scenario: Flag blocks operation
- **WHEN** `admin_allow_server_config` is set to `superadmin` and a non-id-1 admin edits server config
- **THEN** the API returns 403

#### Scenario: Brute force lockout
- **WHEN** repeated failed logins occur for the same user/IP
- **THEN** further attempts are delayed/blocked (port of `attempts_login` behavior)

### Requirement: riud record permission model
Data access SHALL enforce Unix-style record permissions: the requesting user matches `sys_userid` → `sys_perm_user` applies; user's group matches `sys_groupid` → `sys_perm_group` applies; otherwise `sys_perm_other`. Required flag per operation: read `r`, insert `i`, update `u`, delete `d`. All repository queries SHALL apply this filter — no unfiltered query path exists for user-data tables.

#### Scenario: Client cannot read other client's domain
- **WHEN** client A lists web domains and a domain belongs to client B with empty `sys_perm_other`
- **THEN** client B's domain is absent from the result

#### Scenario: Update denied without u flag
- **WHEN** a user attempts to update a record whose applicable permission string lacks `u`
- **THEN** the API returns 403 and no datalog entry is written

### Requirement: Access levels admin and reseller and client
The system SHALL support three access levels. Admin (sys_user id 1 semantics) bypasses record permissions; resellers manage their clients' groups via the full ISPConfig graph (`sys_user.groups` multi-group list, `default_group`, `client.parent_client_id`); clients see only their own group's records. A dedicated isolation test suite SHALL cover reseller→client-group access and cross-reseller isolation.

#### Scenario: Admin sees everything
- **WHEN** the admin lists any entity
- **THEN** all records are returned regardless of permission strings
