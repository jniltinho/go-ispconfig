# installer-cli

Delta requirements for optional PowerDNS installation and configuration. Extends the archived `add-installer-cli` capability so operators can choose PowerDNS instead of Bind at install time (port of `configure_powerdns` and install-time DNS software selection).

## ADDED Requirements

### Requirement: DNS backend answer selects Bind or PowerDNS
The installer SHALL accept a DNS backend answer (`--dns-backend bind|powerdns`, answers-file key `dns_backend`, interactive prompt when DNS is enabled). Default SHALL be `bind`. When DNS is disabled (`--dns n`), the backend answer SHALL be ignored. The chosen backend SHALL be written into `server.config` as `[dns] dns_backend=…`.

#### Scenario: Unattended PowerDNS install
- **WHEN** root runs `go-ispconfig install --yes --dns y --dns-backend powerdns` on a supported host
- **THEN** the install completes with `dns_backend=powerdns` in the server config and PowerDNS packages/DB configured

#### Scenario: Default remains Bind
- **WHEN** the install enables DNS without specifying `--dns-backend`
- **THEN** Bind packages and base config are used and `dns_backend` is `bind` or unset-equivalent defaulting to Bind

### Requirement: PowerDNS packages on supported distros
When `dns_backend=powerdns`, the package step SHALL install the distro profile's PowerDNS package set (`pdns-server` and `pdns-backend-mysql` on Debian 11–13 and Ubuntu 22.04–24.04) instead of requiring bind9 for DNS. The step SHALL remain idempotent. When `dns_backend=bind`, behavior remains the existing bind9 package set.

#### Scenario: PowerDNS packages installed
- **WHEN** the packages step runs with `dns_backend=powerdns` on Ubuntu 24.04
- **THEN** `pdns-server` and `pdns-backend-mysql` are installed (or already present)

#### Scenario: Bind path unchanged
- **WHEN** the packages step runs with `dns_backend=bind`
- **THEN** bind9 (and related) packages are installed as in the base installer-cli spec

### Requirement: powerdns database and gmysql configuration
When `dns_backend=powerdns`, the installer SHALL: `CREATE DATABASE IF NOT EXISTS powerdns` with the same charset as the main DB; `GRANT ALL` on `powerdns.*` to the ISPConfig DB user from localhost; apply the embedded `powerdns.sql` DDL (tables `domains`, `records`, `supermasters`, `domainmetadata` with `ispconfig_id` bridge columns); render embedded `pdns.local.master` into `/etc/powerdns/pdns.d/pdns.local` (mode 0600, root-owned) substituting gmysql host, port, user, password, and dbname; and enable/restart the `pdns` (or `powerdns`) unit. Re-runs SHALL be idempotent (existing DB/schema and identical config produce no destructive rewrite of credentials beyond reuse of the known `ispconfig` password).

#### Scenario: Fresh PowerDNS DB created
- **WHEN** the PowerDNS configure step runs on a host without a `powerdns` database
- **THEN** database `powerdns` exists with the `domains` and `records` tables including `ispconfig_id`, and `pdns.local` points gmysql at that database with the ispconfig user

#### Scenario: Config file permissions
- **WHEN** `pdns.local` is written
- **THEN** the file mode is 0600 and owner/group are root

### Requirement: Validation before activating PowerDNS
Before marking DNS ready with PowerDNS backend, the installer SHALL verify that the gmysql config is readable and that the `ispconfig` user can connect to database `powerdns`. On failure the step SHALL fail without leaving the daemon configured to a broken backend without operator-visible error.

#### Scenario: Unreachable PowerDNS DB fails the step
- **WHEN** MariaDB rejects the ispconfig user for database `powerdns` during configure
- **THEN** the installer exits non-zero naming the PowerDNS database step

### Requirement: Optional Vagrant or dig smoke for PowerDNS
The install test rig SHALL provide an opt-in path (toggle or separate scenario) that installs with `--dns-backend powerdns`, creates one zone via the API after boot, and checks resolution with `dig` (or documents the manual procedure if the default Vagrant box remains Bind-only).

#### Scenario: PowerDNS smoke path documented or automated
- **WHEN** the PowerDNS install test path is enabled
- **THEN** the run installs PowerDNS and a post-install check confirms the `pdns` service is active
