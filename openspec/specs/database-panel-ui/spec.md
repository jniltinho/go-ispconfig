# database-panel-ui Specification

## Purpose
TBD - created by archiving change add-database-module. Update Purpose after archive.
## Requirements
### Requirement: Sites navigation for databases and database users
The Sites module SHALL expose navigation entries for Databases and Database Users (visible per user permissions / module access), with list screens and entry points to create forms. All strings SHALL go through the i18n layer (`en.json` first).

#### Scenario: Client opens database list
- **WHEN** a client with sites access opens Sites → Databases
- **THEN** only databases readable under the riud scope are listed (columns include name, site, server, user, active, remote access)

#### Scenario: Database users list
- **WHEN** a user opens Sites → Database Users
- **THEN** only accessible `web_database_user` rows are listed with username and server

### Requirement: Database form
The database form SHALL mirror `database.tform.php` / `database_edit.php`: fields for server (DB servers only), parent site (`web_domain` vhosts), database name with visible prefix, quota (MB), charset, rw database user, optional ro database user, remote access checkbox, remote IPs, active checkbox, and backup_interval select (none/daily/weekly/monthly) stored for schema parity. Client-side validation SHALL mirror API rules; API field errors SHALL display inline. Non-admins SHALL NOT edit database name or charset after create; remote access controls SHALL be hidden when `disable_client_remote_dbserver` is enabled for non-admins.

#### Scenario: Create form shows name prefix
- **WHEN** a client opens the new-database form
- **THEN** the configured name prefix is shown and the user edits only the suffix

#### Scenario: Remote access hidden for clients when disabled globally
- **WHEN** `disable_client_remote_dbserver` is `y` and a non-admin opens the form
- **THEN** remote access / remote IPs inputs are not rendered

#### Scenario: Validation error shown inline
- **WHEN** the user submits without a parent site
- **THEN** the form shows a field error and does not lose other input

### Requirement: Database user form
The database-user form SHALL mirror `database_user.tform.php`: server, username with prefix, password (required on create, optional on edit). Password SHALL never be displayed back after save.

#### Scenario: Edit user leaves password blank to keep current
- **WHEN** an admin opens an existing database user
- **THEN** the password field is empty and saving without a new password keeps the existing hash

### Requirement: phpMyAdmin external link
When a phpMyAdmin URL is configured (`sites.phpmyadmin_url` or documented fallback), the database list or form SHALL offer an action that opens the URL with `[SERVERNAME]` and `[DATABASENAME]` substituted. The panel SHALL NOT install or proxy phpMyAdmin.

#### Scenario: Open phpMyAdmin with configured URL
- **WHEN** the user clicks Open phpMyAdmin on database `c1_app` on server `host.example`
- **THEN** the browser opens the configured URL with those placeholders replaced

#### Scenario: No action when neither URL nor fallback applies
- **WHEN** no phpMyAdmin URL can be resolved
- **THEN** the Open phpMyAdmin action is hidden or disabled

### Requirement: E2E coverage of the database UI
agent-browser E2E tests SHALL cover: create database user, create database linked to a site and user, toggle remote access, edit password, delete database, delete user; screenshots to `docs/prints/`.

#### Scenario: E2E suite passes against a seeded panel
- **WHEN** the database E2E suite runs against a dev server with seeded site and client data
- **THEN** all listed flows complete without errors

