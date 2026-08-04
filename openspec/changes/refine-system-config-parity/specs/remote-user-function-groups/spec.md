# remote-user-function-groups

## ADDED Requirements

### Requirement: Remote Users renders the legacy function groups
The token form SHALL present the grant list as the function groups of the PHP
panel — the labels assembled from each module's `lib/remote.conf.php`, such as
"Mail domain functions" and "Sites cron functions" — instead of raw scope
strings. Each group SHALL map onto the scopes it implies.

#### Scenario: An ISPConfig3 admin recognises the list
- **WHEN** an admin opens the token create form
- **THEN** the grants are offered as named function groups, grouped by module

#### Scenario: Ticking a group grants its scopes
- **WHEN** "Mail domain functions" is ticked and the token is created
- **THEN** the stored token carries the scopes that group maps to

#### Scenario: Several groups union their scopes
- **WHEN** two groups mapping to different scopes are ticked
- **THEN** the stored token carries both scopes, without duplicates

### Requirement: The resulting scopes are visible, not hidden
The form SHALL display the scope list a selection produces, so the mapping is
inspectable rather than implicit.

#### Scenario: The scope list updates with the selection
- **WHEN** groups are ticked and unticked
- **THEN** the displayed scope list reflects the current selection

#### Scenario: A collapsed mapping is visible as such
- **WHEN** two groups that map to the same scope are both ticked
- **THEN** the displayed scope list shows that scope once

### Requirement: Checked state is derived from the stored scopes
When an existing token is opened, a function group SHALL show as checked when
the token's scopes cover every scope that group maps to.

#### Scenario: Reopening a token shows a consistent selection
- **WHEN** a token created by ticking one group is reopened
- **THEN** that group is checked

#### Scenario: Groups sharing a scope check together
- **WHEN** two groups map to the same scope and a token carries that scope
- **THEN** both groups show as checked

#### Scenario: A token created outside the form still renders
- **WHEN** a token minted by the CLI with raw scopes is opened in the form
- **THEN** the groups its scopes cover are checked and its scope list is shown

### Requirement: The mapping is served, not duplicated in the frontend
The function-group table SHALL be exposed by the API, so the form and the
compatibility parser read the same source of truth.

#### Scenario: The mapping is retrievable
- **WHEN** the token form loads
- **THEN** it obtains the group labels and their scopes from the API

### Requirement: A legacy remote_functions value maps onto scopes
The system SHALL translate a `remote_user` row whose `remote_functions` is a
bare CSV of ISPConfig3 function names through the group table into the
equivalent scopes, rather than treat it as unknown grants. The translation
SHALL be one-way: the panel never writes legacy function names back.

#### Scenario: A PHP-written remote user keeps working
- **WHEN** a token authenticates against a row carrying `mail_domain_get,mail_domain_add`
- **THEN** the request is authorised with the scopes those functions map to

#### Scenario: An unmappable value grants nothing
- **WHEN** no entry in the CSV maps to a known scope
- **THEN** every request authenticated by that token is refused

#### Scenario: Saving converts the row to the new format
- **WHEN** such a token is saved from the panel
- **THEN** `remote_functions` is rewritten in the scope format

#### Scenario: A scope-format value is untouched by the compatibility path
- **WHEN** `remote_functions` already carries `scopes=…`
- **THEN** it parses exactly as before, with no translation applied
