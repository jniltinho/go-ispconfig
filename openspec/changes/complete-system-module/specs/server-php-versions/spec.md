# server-php-versions

## ADDED Requirements

### Requirement: Additional PHP versions are managed from the panel
The panel SHALL provide an admin-only `System → Additional PHP Versions` list and form over `server_php`, exposing name, server, client, FastCGI binary and ini dir, FPM init script, ini dir, pool dir and socket dir, CLI binary and the jailkit section. Port of `interface/web/admin/server_php_list.php` and `server_php_edit.php`.

#### Scenario: A PHP version is created
- **WHEN** an admin fills the form for a server and saves
- **THEN** a `server_php` row exists for that server and is journalled

#### Scenario: Versions are listed per server
- **WHEN** an admin opens the list
- **THEN** each row shows its name and the server it belongs to

### Requirement: Path fields are shape-validated in the panel and existence-validated on the node
The form SHALL require absolute paths and refuse obviously malformed values, and the daemon SHALL report a missing binary or directory through the existing datalog error state when it first renders a configuration for that version.

#### Scenario: Relative path is refused at save
- **WHEN** a FastCGI binary is entered as a relative path
- **THEN** the save is refused with a field error

#### Scenario: A path that does not exist on the node surfaces later
- **WHEN** a version pointing at a missing binary is assigned to a site and the daemon renders it
- **THEN** the failure is recorded in the datalog error state and shown on the site form

### Requirement: A site can be pinned to a created version
The site form's PHP-version select SHALL list the `server_php` rows of the site's server in addition to the distro default, and the selected row SHALL be stored in `web_domain.server_php_id`.

#### Scenario: Created version appears in the site form
- **WHEN** a `server_php` row exists for the site's server and the site form is opened
- **THEN** the version appears as an option in the PHP-version select

#### Scenario: Pinning takes effect in the rendered configuration
- **WHEN** a site is pinned to a version and the daemon re-renders it
- **THEN** the generated FPM pool and jailkit/cron configuration use that version's binaries

#### Scenario: Versions of another server are not offered
- **WHEN** the site belongs to server A and versions exist for server B
- **THEN** server B's versions are not listed

### Requirement: Deleting a version in use is refused
The system SHALL refuse to delete a `server_php` row while any `web_domain` references it.

#### Scenario: In-use version cannot be deleted
- **WHEN** an admin deletes a version referenced by a site
- **THEN** the delete is refused with 422 naming the referencing sites

### Requirement: Gated by admin_allow_server_php
Access SHALL be gated by the `admin_allow_server_php` security policy, superadmin-only by default.

#### Scenario: Non-superadmin admin is refused
- **WHEN** an admin other than `userid 1` opens the page while the policy is `superadmin`
- **THEN** the request is refused with 403
