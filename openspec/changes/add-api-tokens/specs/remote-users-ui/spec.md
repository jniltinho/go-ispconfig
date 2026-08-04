# remote-users-ui

## ADDED Requirements

### Requirement: System → Remote Users lists the API tokens
The panel SHALL provide an admin-only `System → Remote Users` list showing, per token: label, owner, scopes, IP allow-list, expiry, last used and enabled state. It SHALL NOT show the secret or its digest. Port of `interface/web/admin/remote_user_list.php`.

#### Scenario: Admin sees the token inventory
- **WHEN** an admin opens System → Remote Users
- **THEN** every token is listed with its label, owner, scopes, expiry, last used and enabled state

#### Scenario: A never-used token is visibly so
- **WHEN** a token has never authenticated a request
- **THEN** its last-used cell reads as never used rather than blank

#### Scenario: Non-admin cannot reach the page
- **WHEN** a client session navigates to the Remote Users route
- **THEN** the page is not rendered and the API refuses the underlying request

### Requirement: Create form mints a token and shows it once
The create form SHALL take a label, an owner (`sys_user`), a scope selection, an optional IP allow-list and an optional expiry, and SHALL display the resulting token exactly once, with a copy affordance and an explicit warning that it cannot be shown again. Port of `interface/web/admin/remote_user_edit.php` and `form/remote_user.tform.php`.

#### Scenario: Token is displayed once after creation
- **WHEN** an admin submits the create form
- **THEN** the plaintext token is displayed with a warning that it will not be shown again

#### Scenario: Navigating away loses the secret for good
- **WHEN** the admin leaves the page after creating a token
- **THEN** no screen in the panel can display that secret again

#### Scenario: Scope selection is required
- **WHEN** the form is submitted with no scope selected
- **THEN** the submission is refused with a field error

### Requirement: Revoke and re-enable from the list
The list SHALL offer revoking an enabled token and re-enabling a revoked one, each taking effect on the next API request. Deleting a token SHALL also be possible and SHALL be irreversible.

#### Scenario: Revoke from the list
- **WHEN** an admin revokes a token
- **THEN** the row shows it as revoked and the next request carrying it is rejected

#### Scenario: Re-enable restores access
- **WHEN** an admin re-enables a revoked token
- **THEN** the next request carrying it succeeds

#### Scenario: Delete removes the row
- **WHEN** an admin deletes a token
- **THEN** the row is gone and the credential can never be re-enabled

### Requirement: Security policies gate the surface
Token management SHALL be gated by the `admin_allow_remote_users` security policy, and the whole token front door SHALL be disabled when `remote_api_allowed` is not `yes`.

#### Scenario: Non-superadmin admin is refused by default
- **WHEN** an admin other than `userid 1` opens Remote Users while `admin_allow_remote_users` is `superadmin`
- **THEN** the request is refused with 403

#### Scenario: Disabling the remote API refuses every token
- **WHEN** `remote_api_allowed` is set to `no`
- **THEN** every request authenticated by a token or JWT is rejected, and session authentication is unaffected

### Requirement: The panel documents what a token can do
The form SHALL describe each scope in the operator's language rather than as a bare string, and SHALL state the JWT expiry bound where the exchange feature is presented.

#### Scenario: Scopes are described
- **WHEN** an admin picks scopes
- **THEN** each option carries a human description of the endpoints it covers

#### Scenario: JWT revocation bound is stated
- **WHEN** the exchange feature is presented
- **THEN** the page states that a JWT issued by a token stays valid until its expiry even if the token is revoked
