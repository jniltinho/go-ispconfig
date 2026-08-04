# client-limits

## ADDED Requirements

### Requirement: limit_directive_snippets bounds snippet attachment
The client limit hook SHALL enforce `limit_directive_snippets` when a non-admin attaches a directive snippet to a site, using the same semantics as every other limit (`< 0` allow, `== 0` veto, `> 0` veto at or above the count). Admin identities SHALL bypass it.

#### Scenario: Client at its snippet limit is vetoed
- **WHEN** a client whose `limit_directive_snippets` is 2 already has snippets attached to two sites and attaches a third
- **THEN** the request is vetoed with 403 and the limit error key

#### Scenario: Zero forbids attaching any snippet
- **WHEN** a client with `limit_directive_snippets = 0` attaches a snippet
- **THEN** the request is vetoed

#### Scenario: Negative means unlimited
- **WHEN** a client with `limit_directive_snippets = -1` attaches a snippet
- **THEN** the request succeeds

#### Scenario: Admin bypasses the limit
- **WHEN** an admin attaches a snippet to a site owned by a client already at its limit
- **THEN** the request succeeds
