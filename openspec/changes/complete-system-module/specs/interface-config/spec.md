# interface-config

## ADDED Requirements

### Requirement: Interface Config edits the sys_ini blob section by section
The panel SHALL provide `System → Interface Config`, editing the panel-wide INI stored in `sys_ini` row 1, one tab per section, and SHALL merge a submitted section into the stored document so keys the panel does not render survive the round trip. Port of `interface/web/admin/system_config_edit.php`.

#### Scenario: A section is saved without touching the others
- **WHEN** an admin changes one key on one tab and saves
- **THEN** only that section is rewritten and every other section is byte-identical

#### Scenario: An unrendered key survives a save
- **WHEN** the stored INI carries a key the form does not render and the section is saved
- **THEN** that key is still present afterwards with its original value

#### Scenario: The change is journalled
- **WHEN** a section is saved
- **THEN** a datalog row records the change with the acting user

### Requirement: Only keys the panel reads are rendered
The form SHALL render exactly the `sys_ini` keys that the Go code reads, and SHALL NOT render legacy keys with no consumer.

#### Scenario: Password policy keys are editable
- **WHEN** an admin opens Interface Config
- **THEN** `[misc] min_password_length` and `[misc] min_password_strength` are editable fields

#### Scenario: A key with no consumer is absent
- **WHEN** an admin opens Interface Config
- **THEN** legacy keys such as the dashboard layout, custom login text and the ISPConfig update channel are not rendered

#### Scenario: A new consumer without a field fails the build
- **WHEN** code is added that reads a `sys_ini` key with no matching form field
- **THEN** the staleness test fails naming that key

### Requirement: The saved password policy must be satisfiable
The system SHALL refuse a password policy the panel itself cannot honour — a minimum length above the maximum a password field accepts, or a non-numeric value.

#### Scenario: Absurd minimum length is refused
- **WHEN** `min_password_length` is set above the accepted maximum
- **THEN** the save is refused with a field error and the stored value is unchanged

#### Scenario: Policy takes effect immediately
- **WHEN** `min_password_length` is raised and a database user password below it is submitted
- **THEN** the password is refused by the API

### Requirement: Gated by admin_allow_system_config
Access to Interface Config SHALL be gated by the `admin_allow_system_config` security policy, superadmin-only by default.

#### Scenario: Non-superadmin admin is refused
- **WHEN** an admin other than `userid 1` opens Interface Config while the policy is `superadmin`
- **THEN** the request is refused with 403

#### Scenario: Client cannot reach it at all
- **WHEN** a client session requests the endpoint
- **THEN** the request is refused
