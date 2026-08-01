# core-database Specification

## Purpose
TBD - created by archiving change port-ispconfig3-to-go. Update Purpose after archive.
## Requirements
### Requirement: Database schema identical to ISPConfig3
The system SHALL create its MariaDB schema by executing the embedded original ISPConfig3 DDL (`install/sql/ispconfig3.sql`, all ~80 tables) verbatim, producing a schema identical to a PHP ISPConfig3 install — names, types, indexes and defaults — so an existing `dbispconfig` database can be migrated to go-ispconfig without schema conversion. GORM models SHALL map (with explicit `column` tags) at least: `sys_user`, `sys_group`, `sys_datalog`, `sys_remoteaction`, `sys_config`, `sys_ini`, `sys_log`, `sys_session`, `server`, `server_ip`, `server_php`, `client`, `web_domain`, `web_folder`, `web_folder_user`, `dns_soa`, `dns_rr`, `dns_slave`, `dns_template`.

#### Scenario: Migration creates identical schema
- **WHEN** `go-ispconfig migrate` runs against an empty MariaDB database
- **THEN** `SHOW CREATE TABLE` output for every table matches the schema produced by importing ISPConfig3's ispconfig3.sql

#### Scenario: Existing ISPConfig3 database adopted
- **WHEN** `migrate` runs against a database that already contains an ISPConfig3 schema (detected via `server.dbversion`)
- **THEN** no DDL is executed, compatibility is validated, and existing data (clients, domains, zones, users) is served by go-ispconfig unchanged

### Requirement: Record ownership columns on user-data tables
Every user-data table SHALL carry `sys_userid`, `sys_groupid`, `sys_perm_user`, `sys_perm_group`, `sys_perm_other` columns, where each perm value is a subset of the string `riud`.

#### Scenario: New record gets ownership
- **WHEN** a client user creates a record through the API
- **THEN** the record is stored with the creator's sys_userid/sys_groupid and default permissions `riud`/`riud`/empty

### Requirement: Seed data
Migration SHALL seed: admin sys_user (id 1) with a generated password, admin sys_group, the local `server` row with service flags, and default `sys_config` entries.

#### Scenario: Fresh install login
- **WHEN** migration completes on an empty database
- **THEN** the admin user can log into the panel with the generated credentials printed once to stdout

