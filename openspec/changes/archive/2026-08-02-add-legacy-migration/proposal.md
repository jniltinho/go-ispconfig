# Proposal: Legacy ISPConfig3 Migration Assistant

## Why

The foundation change (`port-ispconfig3-to-go`) already covers the drop-in path: pointing go-ispconfig at an existing `dbispconfig` database (identical schema, cutover). But many operators run the legacy PHP ISPConfig3 on **another host with another database** they cannot or do not want to reuse. This change adds the second migration path: a migration assistant that connects to a running PHP ISPConfig3 via its remote API (`remoting.inc.php` + `remote.d/*`), pulls existing data, and imports it into the local go-ispconfig database — driven from the web UI (wizard) or the CLI.

## What Changes

- **Go client for the ISPConfig3 remote API**: talks to the legacy panel's JSON handler (`/remote/json.php`, preferred; SOAP at `/remote/index.php` documented as fallback, not implemented initially). Authenticates with `login(remote_user, password)` → `remote_session` id; fetches inventory via `*_get` calls with `primary_id = -1` (all) or filter arrays with `#OFFSET#`/`#LIMIT#` (pagination): `client_get_all`/`client_get`, `sites_web_domain_get`, `sites_web_folder_get`/`sites_web_folder_user_get`, `dns_zone_get`, `dns_rr_get_all_by_zone`, `dns_slave_get`, `dns_templatezone_get`, `server_get_all`/`server_get`.
- **Import engine**: maps legacy entities onto the local database (schema is identical to ISPConfig3, so mapping is ~1:1) — clients/resellers, web domains, web folders and folder users (`web_folder`, `web_folder_user`), DNS zones/records, DNS slave zones (`dns_slave`) and DNS templates (`dns_template`) — preserving `sys_userid`/`sys_groupid` relationships and `riud` permission strings via an old-ID → new-ID mapping table. Clients are imported ordered by `parent_client_id` (resellers before their clients) with the hierarchy remapped correctly. `web_domain` SSL fields are imported with an explicit caveat: certificates must be re-issued on the new host (legacy key/cert material and paths are not assumed valid). Idempotent: natural keys (client username, web domain, folder path, DNS origin, RR tuple) make re-runs update-or-skip, never duplicate.
- **Password handling**: crypt hashes (`$6$`, `$1$`) returned by the API for service entities are imported verbatim (foundation D10 already verifies legacy hashes at login). **Panel logins are different**: the remote API never exposes `sys_user.passwort`, so panel user hashes cannot be imported. The wizard and the CLI therefore provide a prominent **bulk password-reset flow** (one-time reset tokens, optionally delivered by e-mail) for all recreated panel users, and the final report lists every user requiring reset.
- **Web UI wizard** under System → Tools: enter legacy URL + remote_user credentials → test connection → inventory (per-entity counts) → select what to import → dry-run with conflict report → execute with live progress (SSE) → final report.
- **CLI**: `go-ispconfig migrate-from --url ... --user ... --password ...` with `--dry-run`, `--only clients,sites,dns`, `--insecure`; same engine, same stages as the wizard.
- **Post-import datalog**: every imported record writes `sys_datalog` rows (JSON `{old,new}`) so the go-ispconfig daemon materializes vhosts and zone files on the new host.
- **Multi-server guard**: multi-server legacy installs are not supported — when the legacy panel reports more than one active server, the wizard blocks and the CLI aborts unless the operator explicitly confirms mapping everything onto the single local server.
- **Security**: legacy credentials are never persisted in clear text — kept only in the wizard's server-side session (or process memory for the CLI); TLS certificate verification on by default, disabled only by explicit `--insecure`/wizard checkbox.

Reference PHP sources (read-only, `base/ispconfig3_install/interface/`): `lib/classes/remoting.inc.php` (login/logout, session, insert/update/delete query pattern), `lib/classes/remoting_lib.inc.php` (`getDataRecord` -1/filter semantics), `lib/classes/remote.d/{client,sites,dns}.inc.php` (function surfaces), `web/remote/{json.php,index.php,rest.php}` (handlers), `remoting_client/` (usage examples).

## Capabilities

### New Capabilities

- `legacy-api-client`: Go client for the ISPConfig3 remote JSON API — login/logout with remote_user credentials, typed `*_get` calls with pagination, TLS verification with explicit insecure override, error mapping of SoapFault-style responses.
- `legacy-import-engine`: entity mapping (clients, web domains, DNS zones/records), ID remapping with preserved riud permissions, password hash import or reset flagging, idempotent upsert by natural key, dry-run conflict reporting, sys_datalog emission after import.
- `legacy-migration-cli`: `migrate-from` Cobra command exposing connect → inventory → dry-run → import with flags `--url`, `--user`, `--password`, `--dry-run`, `--only`, `--insecure`.
- `legacy-migration-wizard`: web UI wizard (System → Tools) — connection test, inventory view, import selection, dry-run report, SSE progress, final report; credentials held only in session.

### Modified Capabilities

(none — the foundation capabilities are consumed, not changed: datalog writer, legacy hash verification, and permission model are used as-is)

## Impact

- New packages: `internal/legacy` (API client + import engine), new Cobra subcommand, new API endpoints under `/api/system/migration/*`, new Vue wizard view in the Tools module.
- Dependencies: none new — `net/http` + `encoding/json` for the client, existing Echo/GORM/Cobra stack.
- Database: writes to existing tables only (`client`, `sys_user`, `sys_group`, `web_domain`, `dns_soa`, `dns_rr`, `sys_datalog`); no schema changes.
- Testing: httptest-based mock of the legacy JSON API for integration tests; dedicated idempotency (run twice, no duplicates) and dry-run (no writes) tests.

## Non-goals

- Migration of site files/data (operator runs rsync; the final report prints a suggested `rsync -a` command per site, including uid/gid remapping — `--usermap`/`--groupmap` or a per-site post-rsync `chown -R` — since `system_user`/`system_group` ids differ on the new host).
- Mail, FTP, shell users, cron, databases — mail is explicitly listed as a future phase; the rest follows the module roadmap.
- Continuous synchronization with the legacy panel — this is a one-shot import, safe to re-run (idempotent), not a sync daemon.
- SOAP client implementation (documented fallback only; JSON handler is available on all supported ISPConfig 3.1+ installs).
- Migrating legacy panels older than ISPConfig 3.1 (no JSON handler).
