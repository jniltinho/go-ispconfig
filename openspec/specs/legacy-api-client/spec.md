# legacy-api-client Specification

## Purpose
TBD - created by archiving change add-legacy-migration. Update Purpose after archive.
Go client for the remote API of a legacy PHP ISPConfig3 panel (JSON handler at `/remote/json.php`). Read-only: login/logout plus `*_get` calls.

## Requirements

### Requirement: JSON-handler transport
The client SHALL call legacy API methods as `POST <base_url>/remote/json.php?<method>` with a JSON object of named parameters, and SHALL decode responses of the form `{"code","message","response"}`. A response with `code != "ok"` SHALL be returned as a typed error exposing the legacy fault code and message. The client SHALL NOT implement any `*_add`, `*_update`, or `*_delete` legacy method.

#### Scenario: Successful call
- **WHEN** a method is invoked and the legacy panel responds with `code: "ok"`
- **THEN** the client returns the decoded `response` payload and no error

#### Scenario: Legacy fault
- **WHEN** the legacy panel responds with `code: "permission_denied"`
- **THEN** the client returns an error whose fault code is `permission_denied` and whose message includes the legacy message text

#### Scenario: Non-JSON or transport failure
- **WHEN** the endpoint is unreachable, returns non-2xx, or returns a body that is not valid JSON
- **THEN** the client returns an error that distinguishes transport failure from legacy fault

### Requirement: Session login and logout
The client SHALL authenticate with `login(username, password)` using remote_user credentials, store the returned `remote_session` id, send it as `session_id` on every subsequent call, and SHALL call `logout(session_id)` when the client is closed.

#### Scenario: Login success
- **WHEN** `login` returns a session id
- **THEN** subsequent calls include that id as `session_id`

#### Scenario: Login failure
- **WHEN** `login` fails (wrong credentials, login limit reached, maintenance mode)
- **THEN** the client returns the legacy fault code without retrying

#### Scenario: Logout on close
- **WHEN** the client is closed after a successful login
- **THEN** `logout` is called with the stored session id

### Requirement: Entity getters with pagination
The client SHALL provide typed getters used by the import engine: `client_get_all`, `client_get(id)`, `sites_web_domain_get(filter)`, `dns_zone_get(-1)`, `dns_rr_get_all_by_zone(zone_id)`, `server_get_all`, `server_get(id)`, and `get_function_list`. List getters that accept a filter SHALL support the legacy filter-object semantics including `#OFFSET#` and `#LIMIT#` keys, and SHALL iterate pages until a page returns fewer records than the limit.

#### Scenario: Paginated web domain fetch
- **WHEN** the legacy panel holds 1200 web domains and page size is 500
- **THEN** the client issues three paged `sites_web_domain_get` calls and returns 1200 records

#### Scenario: All DNS zones then records per zone
- **WHEN** zones are fetched
- **THEN** `dns_zone_get` is called with `primary_id = -1`, and for each returned zone id `dns_rr_get_all_by_zone` returns that zone's resource records

#### Scenario: Unknown fields tolerated
- **WHEN** a legacy record contains columns the client does not map (newer/older minor version)
- **THEN** unknown fields are ignored and mapped fields are still decoded

### Requirement: Required-grant preflight
The client SHALL expose a preflight check that calls `get_function_list` and verifies every function the import engine needs is granted to the remote_user, reporting the exact missing function names.

#### Scenario: Missing grant detected
- **WHEN** the remote_user lacks `dns_zone_get`
- **THEN** the preflight fails listing `dns_zone_get` as missing, before any data fetch

### Requirement: TLS verification with explicit insecure override
The client SHALL verify TLS certificates by default. Verification SHALL be disabled only by an explicit insecure option, and the client SHALL surface a warning flag when insecure mode or a plain `http://` URL is used.

#### Scenario: Self-signed certificate rejected by default
- **WHEN** the legacy panel presents an untrusted certificate and insecure mode is off
- **THEN** the connection fails with a certificate error

#### Scenario: Insecure override
- **WHEN** insecure mode is explicitly enabled
- **THEN** the connection succeeds and the client marks the session as insecure for reporting

### Requirement: Credential hygiene
The client SHALL keep credentials only in memory and SHALL never include the password or session id in log output or error messages.

#### Scenario: Error message redaction
- **WHEN** a login error is returned or logged
- **THEN** the output contains neither the password nor the session id
