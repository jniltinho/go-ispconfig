# server-config-server-tab

## ADDED Requirements

### Requirement: Server Config renders the Server tab
The Server Config editor SHALL render a `Server` tab over the `[server]`
section of `server.config`, carrying the fields of the legacy
`server_config.tform.php` Server tab in its order and with its labels, plus
`ssh_port`. Port of `interface/web/admin/templates/server_config_server_edit.htm`.

#### Scenario: The tab is present
- **WHEN** an admin opens System → Server Config for a node
- **THEN** a `Server` tab is offered alongside Web, DNS, Mail, Getmail and Jailkit

#### Scenario: Stored values from an adopted database are shown
- **WHEN** the panel is pointed at a `dbispconfig` previously managed by PHP ISPConfig3
- **THEN** the Server tab shows the `[server]` values that database already carries

#### Scenario: Saving the tab writes only its own section
- **WHEN** a field on the Server tab is changed and saved
- **THEN** only `[server]` is rewritten, every other section is byte-identical, and one `sys_datalog` row is journalled

### Requirement: Applied and compatibility fields are visually separated
The tab SHALL group the fields this port acts on (`ip_address`, `ssh_port`)
above a collapsible legend stating that the fields below it are stored for
ISPConfig3 compatibility and are not applied by this server.

#### Scenario: An operator can tell what has an effect
- **WHEN** the Server tab is rendered
- **THEN** `ip_address` and `ssh_port` appear above a legend, and the remaining fields appear under it

#### Scenario: The compatibility group states its own status
- **WHEN** the legend is read
- **THEN** it says the fields below are stored but not applied by this server

#### Scenario: Compatibility fields still round trip
- **WHEN** a compatibility field is edited and saved, and the tab is reopened
- **THEN** the value entered is the value shown

### Requirement: ssh_port is editable and validated
`ssh_port` SHALL be editable from the Server tab, validated as a TCP port in
1–65535, and consumed by the firewall module's SSH allow rule. A value that
does not parse SHALL leave the daemon's existing fallback to port 22 in place.

#### Scenario: A valid port is applied to the firewall rule
- **WHEN** `ssh_port` is set to 2222 and the firewall is re-rendered
- **THEN** the generated rule allows 2222

#### Scenario: An out-of-range port is refused at save
- **WHEN** `ssh_port` is set to 70000
- **THEN** the save is refused with a field error and the stored value is unchanged

#### Scenario: An unparseable stored value falls back
- **WHEN** the stored `ssh_port` is empty or not a number
- **THEN** the daemon uses port 22 and does not fail

### Requirement: The consumed keys are decoded, not read raw
`internal/getconf` SHALL decode the `[server]` keys this port consumes into a
typed section, and the existing consumers SHALL read that section instead of
indexing the raw INI map.

#### Scenario: The database host suggestion reads the decoded value
- **WHEN** a client database is created and `[server] ip_address` is set
- **THEN** the suggested host comes from the decoded section

#### Scenario: The staleness guard covers the tab
- **WHEN** a key is added to the decoded section without a matching form field
- **THEN** the form staleness test fails naming that key

#### Scenario: A rendered field that is neither decoded nor listed fails the build
- **WHEN** a field is added to the Server tab that is not decoded and not in the declared compatibility list
- **THEN** the form staleness test fails naming that field
