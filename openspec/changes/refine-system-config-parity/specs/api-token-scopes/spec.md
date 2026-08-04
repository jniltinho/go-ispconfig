# api-token-scopes

## ADDED Requirements

### Requirement: Legacy function names map onto scopes
The scope layer SHALL carry a table mapping every ISPConfig3 remote function
group onto the scopes it implies, and the metadata parser SHALL use that table
to translate a `remote_functions` value written by the PHP panel. A value
already in the scope format SHALL be parsed unchanged.

#### Scenario: A legacy CSV becomes scopes
- **WHEN** `remote_functions` holds `sites_web_domain_get,sites_web_domain_add`
- **THEN** it parses as the scopes those functions map to

#### Scenario: The scope format is unaffected
- **WHEN** `remote_functions` holds `scopes=sites:read;expires=…`
- **THEN** it parses exactly as it does today, with no translation

#### Scenario: An unknown function name contributes nothing
- **WHEN** a legacy CSV contains a name that maps to no scope
- **THEN** that entry is ignored and only the mappable ones grant access

#### Scenario: Every mapped scope is a valid scope
- **WHEN** the mapping table is enumerated
- **THEN** every scope it produces passes scope validation, and any that does not fails the build
