# installer-cli

## ADDED Requirements

### Requirement: Install command with interactive and non-interactive modes
The binary SHALL expose `go-ispconfig install` (Cobra). Without `--yes` it SHALL prompt interactively for unanswered questions (hostname, DB settings, panel port, enabled services, optional php-fpm, admin email), showing defaults. With `--yes` it SHALL run without prompting, resolving answers in precedence order: CLI flags > answers file (`--answers <file.toml>`) > defaults; if a required answer has no value it SHALL abort naming the missing flag.

#### Scenario: Unattended install on a clean host
- **WHEN** root runs `go-ispconfig install --yes` on a clean supported host
- **THEN** the install completes without any prompt and exits 0 with the panel reachable

#### Scenario: Answers file overridden by flag
- **WHEN** the answers file sets `panel_port = 8080` and the command line passes `--panel-port 9443`
- **THEN** the panel is configured on port 9443

#### Scenario: Refuses to run unprivileged
- **WHEN** a non-root user runs `go-ispconfig install` without `--dry-run`
- **THEN** the command exits non-zero stating root is required, before making any change

### Requirement: Distro detection with supported-distro gate
The installer SHALL detect the distribution from `/etc/os-release` (`ID`, `VERSION_ID`) and map it to a static profile (package names, service names, config paths, PHP-FPM version) for: Debian 11, 12, 13 and Ubuntu 22.04, 24.04. Any other distro/version SHALL abort with an error naming the detected distro and the supported list.

#### Scenario: Ubuntu 24.04 detected
- **WHEN** `/etc/os-release` contains `ID=ubuntu` and `VERSION_ID="24.04"`
- **THEN** the ubuntu24.04 profile is selected (php8.3-fpm paths, bind9/nginx Debian-family paths)

#### Scenario: Unsupported distro aborts
- **WHEN** the installer runs on a host with `ID=centos`
- **THEN** it exits non-zero listing supported distros and performs no changes

### Requirement: Preflight checks
Before changing the system the installer SHALL verify: running as root, supported distro, systemd present, apt available, and that no existing PHP ISPConfig3 installation is present (`/usr/local/ispconfig`); on an existing PHP install it SHALL abort pointing to the migration change.

#### Scenario: Existing PHP ISPConfig3 detected
- **WHEN** `/usr/local/ispconfig` exists on the target host
- **THEN** the installer aborts without modifying the system and references the legacy-migration path

### Requirement: Package installation step
The installer SHALL install, via apt in noninteractive mode, the profile's package set: nginx, bind9, mariadb-server always; the profile's php-fpm package only when the php-fpm answer is enabled. The step SHALL be idempotent (already-installed packages are a no-op) and SHALL wait for a held dpkg lock up to a timeout before failing.

#### Scenario: Re-run with packages present
- **WHEN** `install` runs a second time on a host where all packages are already installed
- **THEN** the packages step succeeds without reinstalling anything

### Requirement: Database configuration with embedded ISPConfig3 DDL
The installer SHALL connect to MariaDB as root via unix socket (or `--db-root-password` when socket auth is unavailable), create the `dbispconfig` database and an `ispconfig` user with a generated random password, and create the schema through the same code path as `go-ispconfig migrate`, i.e. the embedded original `install/sql/ispconfig3.sql` DDL. If the database already contains the ISPConfig schema the DDL SHALL be skipped.

#### Scenario: Fresh database created
- **WHEN** the database step runs on a host with an empty MariaDB
- **THEN** `dbispconfig` exists with the full ISPConfig3 table set and user `ispconfig` can connect with the generated password

#### Scenario: Existing schema preserved on re-run
- **WHEN** the database step runs again after a completed install
- **THEN** no DDL is executed and existing data is untouched

### Requirement: IP detection and server record
The installer SHALL detect the host's global IPv4/IPv6 addresses and insert them into `server_ip`, and SHALL create (or reuse) the `server` row with `server_name` = hostname, `web_server=1`, `dns_server=1`, `active=1`, and a default `config` INI covering the web and dns sections consumed by the daemon.

#### Scenario: Server row created with flags
- **WHEN** the server-record step completes on a fresh install
- **THEN** exactly one `server` row exists with web_server=1 and dns_server=1, and `server_ip` holds each detected global address

### Requirement: config.toml generation
The installer SHALL write `/etc/go-ispconfig/config.toml` (mode 0600, root-owned) containing the DB DSN with the generated `ispconfig` password, the panel listen address/port, and TLS cert/key paths. An existing config.toml SHALL be backed up before being replaced and its DB credentials reused instead of regenerated.

#### Scenario: Credentials survive re-run
- **WHEN** `install` runs again on an installed host
- **THEN** the DB password in config.toml is unchanged and the previous file was backed up only if content changed

### Requirement: systemd unit installation without crontabs
The installer SHALL write embedded unit files `go-ispconfig-serve.service` and `go-ispconfig-daemon.service` to `/etc/systemd/system/`, run `systemctl daemon-reload`, and `enable --now` both. The installer SHALL NOT create or modify any crontab entry — all periodic work belongs to the daemon's internal scheduler.

#### Scenario: Units active after install
- **WHEN** the install completes
- **THEN** `systemctl is-active go-ispconfig-serve go-ispconfig-daemon` reports active for both and no go-ispconfig entry exists in any crontab

### Requirement: Base nginx and bind configuration from embedded templates
The installer SHALL render, from templates embedded in the binary: the include/snippet directories the web module expects (no nginx vhost or proxy is created for the panel — the `serve` binary terminates TLS directly with the certificate from `/etc/go-ispconfig/ssl/`), and the bind base config (`named.conf.options` with the zonefile directory, inclusion of the ISPConfig local zones file). Every config write SHALL: back up a differing existing file to `<file>.bak-<timestamp>`, validate with `nginx -t` / `named-checkconf` before reloading, and on validation failure restore the original and fail the step.

#### Scenario: Config validation gate
- **WHEN** a rendered nginx config fails `nginx -t`
- **THEN** the previous config is restored, nginx is not reloaded, and the installer exits non-zero naming the invalid file

#### Scenario: Unchanged config produces no backup
- **WHEN** a re-run renders a config byte-identical to the file on disk
- **THEN** no backup file is created and no service reload happens for that file

### Requirement: Self-signed panel certificate
The installer SHALL generate, using Go crypto/x509 (no openssl dependency), a self-signed certificate and key under `/etc/go-ispconfig/ssl/` with CN and SAN set to the host FQDN, validity 10 years, key mode 0600. This certificate is consumed directly by the `go-ispconfig serve` binary, which terminates the panel's TLS itself — no nginx vhost proxies the panel. Generation SHALL be skipped when a non-expired cert already exists.

#### Scenario: Existing cert kept
- **WHEN** `install` re-runs and a valid cert exists
- **THEN** the certificate files are not regenerated

### Requirement: Admin credentials printed once
The installer SHALL create the admin user with a generated random password and print the panel URL and credentials exactly once in the final summary. Persisting the summary root-only to `/root/.go-ispconfig-credentials` SHALL be opt-in via `--write-credentials` (with an instruction to delete the file); by default nothing is written to disk. Re-runs SHALL NOT regenerate or reprint the admin password.

#### Scenario: One-time credential output
- **WHEN** the first install completes
- **THEN** the summary shows the admin password, and a subsequent `install` run does not show or change it

#### Scenario: Credentials file only on request
- **WHEN** the install runs without `--write-credentials`
- **THEN** `/root/.go-ispconfig-credentials` is not created; with `--write-credentials` it is created root-only with the summary

### Requirement: Optional acme client installation for site certificates
The installer SHALL provide an optional `install-acme` step (enabled via answer/flag, default off) that installs acme.sh — or certbot when explicitly chosen — for use by the web module to issue Let's Encrypt certificates for hosted sites. The step SHALL be idempotent (existing client detected → no-op). The panel certificate SHALL remain self-signed regardless of this step.

#### Scenario: acme.sh installed on request
- **WHEN** the install runs with the acme answer enabled and no acme client present
- **THEN** acme.sh is installed and available for the web module, and the panel still serves its self-signed certificate

#### Scenario: Step skipped by default
- **WHEN** the install runs without enabling the acme answer
- **THEN** no acme client is installed and the step is reported as skipped

### Requirement: Update mode
`go-ispconfig install --update` SHALL re-render only the base configs and systemd units (with the same backup/validation rules) and restart the units. It SHALL NOT modify the database contents, config.toml credentials, TLS certificates, or the admin user.

#### Scenario: Update preserves data
- **WHEN** `install --update` runs on a host with existing sites and DNS zones
- **THEN** all sites/zones and credentials remain intact and services are running the re-rendered base configs

### Requirement: Uninstall command
The binary SHALL expose `go-ispconfig uninstall` which stops, disables and removes both systemd units, removes go-ispconfig-rendered nginx/bind config files and `/etc/go-ispconfig/`, and requires typed confirmation unless `--yes`. The database and DB user SHALL be dropped only with `--purge-db`; OS packages SHALL never be removed.

#### Scenario: Default uninstall keeps database
- **WHEN** `go-ispconfig uninstall --yes` runs without `--purge-db`
- **THEN** units and rendered configs are gone but `dbispconfig` still exists

### Requirement: Step logging and idempotent pipeline
The installer SHALL execute steps in a fixed order, log each step's name and outcome (done/skipped/failed) to stdout, and be safe to re-run after any mid-install failure: a full re-run on a partially installed host SHALL converge to a complete installation with exit code 0.

#### Scenario: Recovery after mid-install failure
- **WHEN** an install fails at the bind step and `install --yes` is run again
- **THEN** completed steps are skipped or converge without error and the installation finishes successfully
