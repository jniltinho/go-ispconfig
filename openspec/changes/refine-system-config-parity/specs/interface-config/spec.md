# interface-config

## ADDED Requirements

### Requirement: Main Config edits the panel-wide INI section by section
The panel SHALL provide an admin-only `System → Main Config` screen editing the
INI stored in `sys_ini` row 1, one tab per section, merging a submitted section
into the stored document so keys the panel does not render survive. Port of
`interface/web/admin/system_config_edit.php`.

#### Scenario: A section saves without touching the others
- **WHEN** an admin changes one key on one tab and saves
- **THEN** only that section is rewritten and every other section is byte-identical

#### Scenario: An unrendered key survives a save
- **WHEN** the stored INI carries a key the form does not render and its section is saved
- **THEN** that key is still present afterwards with its original value

#### Scenario: The change is journalled
- **WHEN** a section is saved
- **THEN** a datalog row records the change with the acting user

### Requirement: Only keys the Go code reads are rendered
The form SHALL render exactly the `sys_ini` keys read by this port, and SHALL
NOT render legacy keys that configure the PHP interface.

#### Scenario: The password policy is editable
- **WHEN** an admin opens Main Config
- **THEN** `[misc] min_password_length` and `min_password_strength` are editable fields

#### Scenario: Database and shell prefixes are editable
- **WHEN** an admin opens the Sites tab
- **THEN** the database, database-user, FTP and shell user prefixes and the phpMyAdmin URL are editable

#### Scenario: PHP-interface cosmetics are absent
- **WHEN** an admin opens Main Config
- **THEN** the dashboard atom feeds, custom login text, combobox and tab-change settings and maintenance mode are not rendered

#### Scenario: A new consumer without a field fails the build
- **WHEN** code is added that reads a `sys_ini` key with no matching form field
- **THEN** the staleness test fails naming that key

### Requirement: The saved password policy must be satisfiable
The system SHALL refuse a password policy the panel itself cannot honour — a
non-numeric value, or a minimum above the maximum a password field accepts.

#### Scenario: An absurd minimum is refused
- **WHEN** `min_password_length` is set above the accepted maximum
- **THEN** the save is refused with a field error and the stored value is unchanged

#### Scenario: The policy takes effect immediately
- **WHEN** `min_password_length` is raised and a shorter database-user password is submitted
- **THEN** the API refuses that password

### Requirement: Gated by admin_allow_system_config
Access SHALL be gated by the `admin_allow_system_config` security policy,
superadmin-only by default.

#### Scenario: A non-superadmin admin is refused
- **WHEN** an admin other than `userid 1` opens Main Config while the policy is `superadmin`
- **THEN** the request is refused with 403

#### Scenario: A client cannot reach it
- **WHEN** a client session requests the endpoint
- **THEN** the request is refused
