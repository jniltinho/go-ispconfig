# Design: Legacy ISPConfig3 Migration Assistant

## Context

go-ispconfig's foundation covers migration by shared database (cutover on the same `dbispconfig`). This change covers the remote path: the legacy PHP ISPConfig 3.x panel runs on another host/database and must be imported over its remote API. The legacy API surface (reference: `interface/lib/classes/remoting.inc.php`, `remoting_lib.inc.php`, `remote.d/{client,sites,dns}.inc.php`, handlers under `interface/web/remote/`):

- Auth: `login(username, password)` against `remote_user` (crypt-verified), returns a `remote_session` id valid for the session lifetime; `logout(session_id)`. Per-function authorization via `remote_user.remote_functions` — `checkPerm` rejects calls not granted to the remote user.
- Reads: `*_get(session_id, primary_id)` where `primary_id` is an int (single record), `-1` (all records of the table), or a filter object whose keys are column names (LIKE when the value contains `%`) plus `#OFFSET#`/`#LIMIT#` for pagination (`remoting_lib.inc.php::getDataRecord`). Convenience calls: `client_get_all` (id list), `dns_rr_get_all_by_zone(session_id, zone_id)`, `server_get_all`, `server_get(server_id, section)`.
- Transports: JSON handler at `/remote/json.php` (`POST /remote/json.php?<method>` with a JSON object of named params; responses `{"code":"ok","message":...,"response":...}` or `{"code":"remote_fault",...}`), SOAP at `/remote/index.php`, REST-ish at `/remote/rest.php`.

Local side: schema is byte-identical to ISPConfig3 (foundation D9), datalog writer emits JSON `{old,new}` (D2), login verifies legacy crypt hashes `$6$`/`$1$` (D10).

## Goals / Non-Goals

**Goals:**
- One import engine with two frontends (wizard + CLI) sharing connect → inventory → dry-run → import → report stages.
- Idempotent, re-runnable import of clients, web domains, DNS zones/records with permissions preserved.
- Imported records flow through `sys_datalog` so the daemon builds vhosts/zones on the new host.

**Non-Goals:**
- SOAP client code (documented fallback only), site file transfer, mail/ftp/shell/cron/db entities, continuous sync, legacy panels < 3.1.

## Decisions

### D1 — JSON handler as the wire protocol
The client speaks only to `/remote/json.php`: `POST <base_url>/remote/json.php?<method>` with `Content-Type: application/json` body of named params (always including `session_id` except for `login`), decoding `{"code","message","response"}`; any `code != "ok"` maps to a typed Go error carrying the legacy fault code (`permission_denied`, `login_failed`, …). Rationale: JSON is native to Go, the handler exists on every supported 3.1+ install, and it exposes the exact same method surface as SOAP. *Alternative considered*: SOAP client — rejected: requires a WSDL-less SOAP encoder in Go for zero functional gain; kept as documented fallback for operators whose install disabled json.php.

### D2 — Read-only against the legacy panel
The client implements only `login`, `logout`, and `*_get` reads. It never calls `*_add/_update/_delete` on the legacy side. Rationale: the migration must be unable to damage the source system; the required `remote_functions` grants for the remote_user are read-scope only (documented in the wizard's connect step, since `checkPerm` will reject missing grants — surfaced as actionable errors naming the missing function).

### D3 — Fetch strategy per entity
- Clients: `client_get_all` → per-id `client_get` (full record incl. `sys_userid`/`sys_groupid`). Import order is by `parent_client_id` dependency: resellers first, then their clients, so the reseller's new id exists before dependent clients are inserted and the hierarchy is remapped correctly.
- Sites: `sites_web_domain_get` with filter `{"type": "vhost", "#OFFSET#": n, "#LIMIT#": 500}` pages (plus `vhostsubdomain`/`vhostalias` in later passes); page size 500, configurable. Web folders and folder users via `sites_web_folder_get` / `sites_web_folder_user_get` (paged filters). `web_domain` SSL fields are imported as-is, with the report warning that certificates must be re-issued on the new host.
- DNS: `dns_zone_get(-1)` for all SOAs, then `dns_rr_get_all_by_zone(zone_id)` per zone; slave zones via `dns_slave_get(-1)`; zone templates via `dns_templatezone_get(-1)`.
- Servers: `server_get_all` + `server_get(id)` for the inventory display and server-id mapping only. When more than one active server is reported, the run is blocked (wizard) / aborted (CLI) unless the operator explicitly confirms mapping everything onto the single local server — multi-server topologies are not supported.
Rationale: uses only documented, stable getters; `-1`/filter semantics come straight from `remoting_lib::getDataRecord`. Sys users/groups are not fetchable as tables over the API — the engine reconstructs them from each client record (see D4).

### D4 — ID remapping with natural-key idempotency
Legacy primary keys cannot be assumed free locally. The engine keeps an in-run mapping `{entity, old_id} → new_id` and rewrites every foreign key through it (`client.client_id`, `sys_groupid`, `web_domain.parent_domain_id`, `dns_rr.zone`, `server_id` → chosen local server). Records are upserted by natural key:
- client: `client.username` (imported in `parent_client_id` order — resellers before their clients — with `parent_client_id` rewritten through the mapping)
- sys_user/sys_group: `username` / `name` (recreated from client data, one user+group per client as ISPConfig does)
- web_domain: `domain` (+`type`)
- web_folder: `(parent_domain_id, path)`; web_folder_user: `(web_folder_id, username)`
- dns_soa: `origin`; dns_slave: `origin`; dns_template: `name`
- dns_rr: `(zone, name, type, data)`
Existing match → update-or-skip (skip when identical, report as "exists"); no match → insert. Re-running the import therefore never duplicates. `sys_perm_user/group/other` riud strings are copied verbatim; `sys_userid`/`sys_groupid` are rewritten via the mapping. *Alternative considered*: preserving legacy numeric ids — rejected: collides with an already-used local database and breaks the "import into a live panel" case.

### D5 — Passwords: import service hashes; panel logins get a bulk reset flow
If a fetched record carries a crypt hash (`$6$`, `$1$`, or `$5$` prefix) in a password field, it is stored verbatim — foundation D10 verifies these at login. Panel logins can never be imported: the remote API does not expose `sys_user.passwort` (client_get returns the `client` form, not sys_user). Recreated panel users therefore get an unusable random placeholder hash and a "password reset required" marker, and both frontends make recovery prominent: the wizard's final step and the CLI report offer a **bulk password-reset flow** that generates one-time reset tokens/links for all flagged users (optionally e-mailed), and the final report lists every user requiring reset. No plaintext is ever generated or stored.

### D6 — Two-phase run: dry-run plan, then apply
The engine always builds a full plan first (per record: `create` / `update` / `skip-identical` / `conflict`), touching the local DB read-only. `--dry-run` (or the wizard's dry-run step) stops there and renders the plan as the conflict report (conflicts: natural key exists with different owner, missing referenced server, unmapped parent domain/zone). Apply executes the same plan inside per-entity transactions, writing each row through the foundation's datalog-aware writer so every insert/update lands in `sys_datalog` (JSON `{old,new}`) and the daemon materializes vhosts and zone files. Rationale: plan/apply symmetry makes dry-run trustworthy (it is the real diff, not an estimate).

### D7 — Wizard state and progress via SSE
The wizard is a server-side session object: legacy URL + credentials live only in the authenticated session store (never in DB or config files, redacted from logs). Import runs in a goroutine; progress events (`entity`, `done`, `total`, `errors`) stream over a single SSE endpoint (`GET /api/system/migration/progress`), with the current snapshot also available by polling the status endpoint (documented fallback for proxies that buffer SSE). One migration run at a time (in-process lock). *Alternative considered*: WebSocket — rejected: one-directional progress needs nothing bidirectional.
<!-- ponytail: single in-process run lock; job queue if concurrent migrations are ever needed -->

### D8 — CLI shares the engine
`migrate-from` is a thin Cobra command over the same engine: `--url`, `--user`, `--password` (or prompt when omitted, so it stays out of shell history), `--dry-run`, `--only clients,sites,dns` (dependency-ordered: sites require clients unless already present locally), `--insecure`. Output: inventory table → plan/conflict summary → progress lines → final report (including the suggested per-site `rsync -a` command). Exit non-zero when any entity fails or when a dry-run finds conflicts.

### D9 — TLS verified by default
`http.Client` with standard verification; `--insecure` / wizard checkbox sets `InsecureSkipVerify` and is loudly echoed in the report. Plain `http://` URLs are allowed but warned about (credentials travel in the body).

## Risks / Trade-offs

- [Legacy API shape drifts across 3.1/3.2/3.3 minor versions] → client asserts only fields the engine maps; unknown fields ignored; connection test reports the legacy version (`server_get` / `get_function_list`) and the report notes untested versions.
- [remote_user lacks a needed `remote_functions` grant] → connect step calls `get_function_list` and verifies every required function upfront, failing with the exact missing grant names before any import starts.
- [Large installs (thousands of zones) → thousands of `dns_rr_get_all_by_zone` calls] → sequential with page-size batching for domains; acceptable for a one-shot tool; progress keeps the operator informed. <!-- ponytail: sequential fetch; bounded worker pool if fetch time ever matters -->
- [Importing into a non-empty panel can attach records to the wrong owner] → conflict detection in the plan (same natural key, different owner) blocks apply for that record; report requires explicit re-run after resolution.
- [Datalog flood after big import overwhelms the daemon] → daemon already processes in LIMIT-1000 batches (foundation D2); report tells the operator to wait for `server.updated` to catch up before DNS cutover.
- [Credentials leakage] → credentials only in session/process memory, redacted in logs and error messages; SSE/status payloads never echo them.

## Migration Plan

Operator flow (documented in the final report and docs):
1. On the legacy panel: create a remote_user with read (`*_get`) grants for client, sites, dns; ensure Remote API is enabled in security settings.
2. Run wizard or `migrate-from --dry-run`; resolve conflicts; run apply.
3. Wait for the go-ispconfig daemon to drain the generated datalog (vhosts + zones written).
4. Rsync site files per the report's suggested commands, remapping uid/gid (`--usermap`/`--groupmap`, or a per-site `chown -R system_user:system_group` after rsync) — `system_user`/`system_group` ids differ on the new host.
5. Only **after** the files are in place, enable/trigger SSL and Let's Encrypt issuance for the imported sites — the webroot challenge fails on an empty docroot; legacy certificates are not reused and must be re-issued on the new host.
6. Lower DNS TTLs, switch DNS/IPs, then decommission the legacy host.

Rollback: imported records are ordinary panel records — delete them through the panel (which datalogs the removals); the legacy system was never written to, so it remains authoritative until DNS cutover.

## Open Questions

- Should `--only dns` allow importing zones whose owning client is absent locally by assigning them to admin (sys_userid 1)? Leaning yes, with a report warning.
- Mail-phase follow-up: reuse the same engine with `mail_domain`/`mail_user` getters — tracked as a future change, no hooks needed now.
