# legacy-migration-cli Specification

## Purpose
TBD - created by archiving change add-legacy-migration. Update Purpose after archive.
`go-ispconfig migrate-from` — CLI frontend of the legacy import engine.

## Requirements

### Requirement: migrate-from command
The CLI SHALL provide `migrate-from` with flags `--url` (legacy panel base URL, required), `--user` (remote_user name, required), `--password` (prompted interactively when omitted), `--dry-run`, `--only <clients,sites,dns>` (default: all three), and `--insecure`. The command SHALL run connect (login + grant preflight) → inventory → plan → apply (unless `--dry-run`) using the shared import engine.

#### Scenario: Full run
- **WHEN** `migrate-from --url https://legacy:8080 --user migrator --password s3cret` is run against a reachable legacy panel
- **THEN** the command prints the inventory, the plan summary, per-entity progress, and the final report, and exits 0 when no entity failed

#### Scenario: Password prompt
- **WHEN** `--password` is omitted on an interactive terminal
- **THEN** the command prompts for the password without echoing it

#### Scenario: Entity subset
- **WHEN** `--only clients,dns` is passed
- **THEN** only clients and DNS zones/records are fetched and planned; sites are untouched

### Requirement: Dry-run output and exit code
With `--dry-run` the command SHALL print the plan (per-entity create/update/skip/conflict counts and each conflict with its reason) and SHALL perform no local writes. The command SHALL exit non-zero when the dry-run finds conflicts or when any stage fails; a conflict-free dry-run SHALL exit 0.

#### Scenario: Conflicts found
- **WHEN** a dry-run finds 2 conflicts
- **THEN** both conflicts are printed with record, natural key, and reason, and the exit code is non-zero

#### Scenario: Clean dry-run
- **WHEN** a dry-run finds no conflicts
- **THEN** the command exits 0 and prints the would-be create/update counts

### Requirement: Connection and grant failures are actionable
When login fails or the grant preflight finds missing `remote_functions`, the command SHALL exit non-zero before fetching any data, printing the legacy fault code or the exact missing function names.

#### Scenario: Missing grants
- **WHEN** the remote_user lacks `sites_web_domain_get`
- **THEN** the command exits non-zero and the output names `sites_web_domain_get`

### Requirement: Multi-server legacy guard
When the legacy panel reports more than one active server, the command SHALL abort before planning, stating that multi-server topologies are not supported, unless the operator explicitly confirms (`--map-all-to-local-server` or equivalent explicit flag) that all entities are to be mapped onto the single local server.

#### Scenario: Multi-server legacy aborts
- **WHEN** `server_get_all` reports two active servers and no override flag is passed
- **THEN** the command exits non-zero naming the servers and the required explicit confirmation flag

### Requirement: Bulk password reset for panel users
After apply, the command SHALL surface the password-reset user list prominently and SHALL support generating one-time reset tokens for all flagged users in bulk (flag or documented follow-up command), printing the tokens/links (or e-mailing them when delivery is configured) without ever printing plaintext passwords.

#### Scenario: Reset tokens generated
- **WHEN** an apply recreated 5 panel users without importable hashes and the bulk reset is requested
- **THEN** 5 one-time reset tokens/links are produced, one per flagged user

### Requirement: TLS behavior
The command SHALL verify TLS by default and fail on untrusted certificates unless `--insecure` is passed; when `--insecure` is used or the URL is plain `http://`, a warning SHALL be printed and repeated in the final report.

#### Scenario: Insecure warning
- **WHEN** `--insecure` is passed
- **THEN** a TLS-verification-disabled warning appears in the output and final report

### Requirement: No credential leakage
The command SHALL never write the legacy password to disk, config files, or logs; process output SHALL redact it.

#### Scenario: Verbose output redacts
- **WHEN** the command logs the login request in verbose mode
- **THEN** the password value is redacted
