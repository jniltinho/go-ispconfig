# Tasks: add-legacy-migration

## 1. Legacy API client (`internal/legacy/client`)

- [x] 1.1 Implement JSON-handler transport: `POST <url>/remote/json.php?<method>`, request encoding of named params, `{"code","message","response"}` decoding, typed fault errors (fault code + message), transport vs fault error distinction; unit tests with httptest. Commit.
- [x] 1.2 Implement `Login`/`Logout` with session-id storage and injection into every call; redact password/session id in errors and logs; tests for login failure fault codes. Commit.
- [x] 1.3 Implement TLS options: verified default, explicit `Insecure` flag (InsecureSkipVerify + insecure marker on session), warning marker for plain `http://`; test against httptest TLS server with untrusted cert. Commit.
- [x] 1.4 Implement typed getters: `ClientGetAll`, `ClientGet`, `SitesWebDomainGet(filter)` with `#OFFSET#`/`#LIMIT#` page iteration (page size 500, configurable, stop on short page), `SitesWebFolderGet`, `SitesWebFolderUserGet`, `DNSZoneGetAll` (`primary_id=-1`), `DNSRRGetAllByZone`, `DNSSlaveGetAll`, `DNSTemplateZoneGetAll`, `ServerGetAll`, `ServerGet`, `GetFunctionList`; unknown response fields ignored; tests incl. 3-page pagination. Commit.
- [x] 1.5 Implement grant preflight: compare `get_function_list` result against the engine's required-function set, error lists exact missing names; tests. Commit.

## 2. Mock legacy server (test fixture)

- [x] 2.1 Build an httptest-based mock of `/remote/json.php` serving canned fixtures (login/logout, attempts limit, grant list, clients, paged web domains, zones, RRs incl. `$6$` hash fields) for reuse by client, engine, CLI, and wizard integration tests. Commit.

## 3. Import engine (`internal/legacy/importer`)

- [x] 3.1 Define entity mappers legacy→local models for client (+derived sys_user/sys_group), web_domain (incl. SSL fields with re-issue warning), web_folder, web_folder_user, dns_soa, dns_rr, dns_slave, dns_template; server-id mapping to a selected local server; unit tests on fixture records. Commit.
- [x] 3.2 Implement inventory (per-entity counts incl. folders/folder users/slave zones/templates + legacy server list; multi-server detection flag for the guard) using only client reads; integration test against the mock. Commit.
- [x] 3.3 Implement the planner: natural-key lookup (client.username; sys_user.username/sys_group.name; web_domain domain+type; web_folder parent_domain_id+path; web_folder_user folder+username; dns_soa.origin; dns_slave.origin; dns_template.name; dns_rr zone+name+type+data), classification create/update/skip-identical/conflict, ID remap table, client ordering by parent_client_id (resellers before their clients, hierarchy remapped), conflict reasons (different owner, unmapped server, missing parent/zone/owner), dependency-ordered selection subset (clients→sites/dns), optional assign-orphan-zones-to-admin flag. Local reads only. Tests per conflict class. Commit.
- [x] 3.4 Implement apply: execute plan in per-entity transactions through the foundation's datalog-aware writer (JSON `{old,new}`, correct dbtable/dbidx/action/server_id); skip conflicts; riud strings copied verbatim, sys_userid/sys_groupid rewritten via remap. Integration test: applied zone produces sys_datalog rows consumable by the daemon. Commit.
- [x] 3.5 Implement password handling: import `$1$/$5$/$6$` hashes verbatim for service entities; panel sys_users always get unusable placeholder + reset-required marker (API never exposes `sys_user.passwort`); bulk one-time reset-token generation for all flagged users; report lists reset users; assert no plaintext in DB or logs. Tests. Commit.
- [x] 3.6 Implement final report struct: per-entity counts, reset list, warnings (insecure TLS, unmapped servers, SSL re-issue), operational-order note (rsync files → SSL/LE → DNS cutover), per-domain rsync suggestion with uid/gid remap (`rsync -a --usermap=... --groupmap=... legacyhost:<document_root>/ <document_root>/` or post-rsync `chown -R`). Tests. Commit.
- [x] 3.7 Idempotency and dry-run integration tests against MariaDB + mock server: dry-run leaves zero writes and zero datalog rows; apply-twice yields identical counts and all-skip second plan; changed legacy field re-plans as update. Commit.

## 4. CLI (`migrate-from`)

- [x] 4.1 Add Cobra command `migrate-from` with `--url`, `--user`, `--password` (hidden interactive prompt when omitted), `--dry-run`, `--only clients,sites,dns`, `--insecure`; wire connect → preflight → inventory → plan → apply; inventory table, plan/conflict summary, progress lines, final report output. Commit.
- [x] 4.2 Exit codes and failure paths: non-zero on login/preflight failure (naming fault code or missing grants, before any fetch), on multi-server legacy without the explicit map-to-single-server confirmation flag, on dry-run conflicts, on any entity failure; insecure/http warnings printed and repeated in report; redaction in verbose output. Integration tests against the mock. Commit.
- [x] 4.3 Bulk password-reset flow in the CLI: prominent reset-required list in the report + one-time reset-token generation for all flagged users (flag or follow-up command), never printing plaintext passwords. Tests. Commit.

## 5. Wizard API (`/api/system/migration/*`)

- [x] 5.1 Add admin-only endpoints: POST connect/test (login + preflight, returns panel info or fault/missing grants/cert error), GET inventory (incl. multi-server guard state), POST dry-run, POST execute (rejected for multi-server legacy without explicit confirmation), POST reset-passwords (bulk one-time tokens for reset-required users), GET status (snapshot), GET progress (SSE). Credentials stored only in the server-side session; never in DB/config/logs/responses. Swaggo annotations. Commit.
- [ ] 5.2 Implement run manager: single active run (in-process lock, reject second start with already-running error), goroutine execution surviving page reloads, progress events (entity, done, total, errors) fanned to SSE and snapshot. Integration tests: SSE event stream, concurrent-start rejection, status reattach. Commit.

## 6. Wizard UI (Vue, System → Tools)

- [ ] 6.1 Add "Migrate from ISPConfig3" wizard view with steps: connection form (URL, user, password, skip-TLS checkbox with warning, Test connection showing panel info or exact missing grants), inventory with per-entity counts and clients/sites/dns toggles + target server mapping + multi-server block requiring explicit confirmation to map onto the single local server. English i18n keys. Commit.
- [ ] 6.2 Add dry-run step (plan counts + conflict list with reasons, execute button noting conflicts are skipped), execution step (SSE progress with polling fallback, reattach via status on reload), final report step (counts, reset-required users with prominent bulk password-reset action generating one-time tokens/links, warnings incl. SSL re-issue, operational order rsync→SSL/LE→DNS cutover, rsync suggestions with uid/gid remap and operator-responsibility note). agent-browser E2E against the mock legacy server. Commit.

## 7. Docs

- [ ] 7.1 Write `docs/legacy-migration.md`: remote_user setup on the legacy panel (required read grants, enabling Remote API), wizard and CLI walkthrough, SOAP fallback note, rsync/file-transfer responsibility with uid/gid remap (`--usermap`/`--groupmap` or per-site chown), operational order (rsync files before enabling SSL/Let's Encrypt — webroot challenge fails on empty docroot), bulk password-reset flow for panel users, multi-server guard, DNS cutover order (wait for datalog drain, lower TTLs), rollback notes. Screenshots to docs/screenshots. Commit.

## 8. Real-server validation (read-only)

- [ ] 8.1 Validate the legacy API client against the real ISPConfig3 server (Apache2 + PHP-FPM, see AGENTS.local.md — read-only): remote API login, inventory counts (clients/sites/zones), dry-run report; agent-browser read-only walkthrough with screenshots to docs/prints/
- [ ] 8.2 Compare a mysqldump of the real server's schema (analysis only, dump never committed) against the embedded DDL to confirm drop-in adoption compatibility of a long-lived production database
