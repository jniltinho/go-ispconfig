# Design: complete-system-module

## Context

Four System entries are missing. They are not one feature — they are four small surfaces that happen to share a menu — so the design question for each is the same and short: *what already reads this data, and what is the smallest screen that lets an operator set it without inventing new semantics?*

What exists to build on:

- **The section-per-tab INI editor** landed with `server-config-sync`: `FormMeta` served from a static field table, `TabbedForm` on the SPA side, and a PUT that merges one section into the stored blob so unknown keys survive. `sys_ini` is the same shape as `server.config` — an INI blob in a text column — so Interface Config is the same editor pointed at a different row.
- **The declarative `Entity` framework**: `server_php` and `directive_snippets` are ordinary tables with ordinary CRUD, so they are entity definitions plus hooks, exactly like `firewallEntity()`.
- **`serverEntity()` already exists** (`internal/api/servers.go`) with the role flags declared as CHECKBOX fields — Server Services is mostly a route and a form, plus the guards.

What does **not** exist: anything that applies a directive snippet. `web_domain.directive_snippets_id` is stored and ignored by both vhost renderers.

## Goals / Non-Goals

**Goals:**

- Every key the panel reads out of `sys_ini` is editable, and nothing else is.
- A pinned PHP version can be created and selected end to end: `server_php` row → site form select → jailkit/cron/apache2 pick it up.
- Disabling a server role fails loudly when it would orphan data, instead of silently stopping a node from applying changes.
- A directive snippet actually reaches the generated vhost.

**Non-Goals:**

- Any new table or column.
- Multi-server orchestration beyond editing the rows that already exist.
- A general "arbitrary config file fragment" mechanism — snippets are typed and scoped to the vhost insertion points.

## Decisions

### D1 — Interface Config reuses the Server Config editor against `sys_ini`

**Decision**: serve `GET /api/meta/forms/system_config` from a static field table, and `GET|PUT /api/system/config[/:section]` over `sys_ini` row 1 with the same merge-one-section semantics as `/api/servers/:id/config`.

**Why**: the two surfaces are the same problem — an INI blob whose keys outnumber what the panel understands, where a write must not drop the keys it does not render. Reusing the pattern means one merge implementation, one tolerant-parse behaviour, and an operator who learns one screen learns both.

**Which keys**: only those with a consumer in the Go code —

| Section | Key | Read by |
|---|---|---|
| `misc` | `min_password_length`, `min_password_strength` | `internal/api/sitesdb.go` |
| `misc` | `ssh_authentication` | `internal/api/sites_ftp_shell.go` |
| `sites` | `ssh_authentication` (override) | `internal/api/sites_ftp_shell.go` |
| `sites` | (whole section) | `internal/api/sitesdb.go` database defaults |
| `mail` | welcome-mail keys | `internal/mail/welcome.go` |
| `mail` | `rspamd_spam_*`, `rspamd_greylisting_level` | `internal/api/mailrspamd.go` |

The legacy `system_config.tform.php` has ~60 more (dashboard layout, company logo, custom login text, maintenance mode, `demo_mode`, the ISPConfig update channel). They are not rendered — same rule as Server Config and CP Users. A staleness test mirrors `TestServerConfigFormMatchesGetconf`: **every `sections["…"]["…"]` literal in the codebase must correspond to a rendered field**, so a new consumer cannot be added without the form following.

**Alternative considered**: a bespoke settings page with typed Go fields instead of an INI editor. Rejected — it would need a migration path for the keys it does not model, and `sys_ini` is written by the PHP panel on an adopted database.

### D2 — Server Services guards are refusals, not warnings

**Decision**: three hard guards on the `server` entity update, returning 422:

1. the last `active` server with `web_server = 1` cannot lose that flag or its `active` state;
2. turning a role off is refused while rows owned by that role exist on the server (`mail_server` with `mail_domain` rows, `dns_server` with `dns_soa`, `db_server` with `web_database`, `web_server` with `web_domain`);
3. `server_id` of an existing row is immutable (already the pattern in `serverIPEntity`).

**Why refusals rather than a confirmation checkbox**: the failure is silent and delayed. A node with `mail_server = 0` keeps its mailboxes in the database, keeps serving what is already on disk, and simply stops applying new changes — the operator finds out days later when a new mailbox never appears. A checkbox labelled "I understand" does not make that discoverable; refusing and naming the rows does. The escape hatch for a genuine decommission is to move or delete the rows first, which is the operation that was actually intended.

**Trade-off**: an operator decommissioning a role must delete its data first, which is more steps. That is the correct order anyway.

### D3 — `server_php` is CRUD plus one join the site form already wants

**Decision**: an admin-only entity over `server_php` scoped by `admin_allow_server_php`, plus a `/api/meta/lookups/server-php?server_id=` datasource so the site form's PHP select lists the rows for the site's server. No plugin work: jailkit, cron, apache2 and shell already join `web_domain.server_php_id`.

**Validation that matters**: the FastCGI/FPM paths must exist **on the target server**, and the panel cannot see another node's filesystem. So the form validates shape (absolute paths, plausible binary names) and the *daemon* validates existence when it first renders a pool for that version, reporting through the existing datalog error state that the site form already surfaces. Pretending to validate a remote path in the browser would be a lie.

### D4 — Directive snippets are inserted at named points, not concatenated

**Decision**: the vhost templates gain explicit insertion points — `{SNIPPET_SERVER}`, `{SNIPPET_LOCATION}`, `{SNIPPET_PHP}` — and a snippet declares which point it targets via its `type`. A snippet whose type does not match the server's `server_type` is refused at save time.

**Why not "append the text to the vhost"**: nginx and apache2 both have contexts where a directive is legal in one place and a syntax error in another. Appending would let a `location` block land at server scope and break every site on the node at the next reload. Named points make the contract explicit and let the renderer keep the config test it already runs before applying.

**Why the type check at save time**: a snippet is written once and applied to many sites. Catching the mismatch when the operator writes it is one error message; catching it at apply time is one broken vhost per site plus a rollback.

`customer_viewable` and `limit_directive_snippets` gate what a non-admin sees and how many they may attach — the client-limit hook point already exists (`rest-api-core`'s "Client limit hook point"), so this is a limit registration, not new machinery.

### D5 — Four surfaces, four commits, no shared abstraction

**Decision**: build them independently. No "system settings framework".

**Why**: they share a menu, not a shape. One is an INI editor (already written), two are plain entities, one is a renderer change. A common abstraction over four different things would be larger than the four things.

## Risks / Trade-offs

- **Interface Config exposes a password policy that immediately affects every password field** → the minimum length/strength are validated on save (a policy the panel itself cannot satisfy is refused), and the change is journalled like any other.
- **Server Services can stop a node applying changes** → D2's refusals, plus the existing monitor state page which already surfaces a node that has stopped consuming its datalog.
- **`server_php` paths cannot be validated from the panel** → shape validation in the form, existence validation in the daemon with the error surfaced on the site form (D3). Documented so an operator knows where the real error appears.
- **A directive snippet is arbitrary config text with root effect** → admin-only creation, type-scoped insertion points, the existing pre-apply config test on both renderers, and `customer_viewable` defaulting to off. A snippet that breaks the config test is not applied and the previous vhost stays in place.
- **Staleness between `sys_ini` consumers and the form** → the D1 test that scans for `sections[...]` literals. It is a grep-shaped test and will need updating if the access pattern changes; that is cheaper than a form that silently omits a key someone's password policy depends on.

## Migration Plan

1. Ship Interface Config first — highest value, lowest risk, reuses a landed pattern.
2. Server Services second, with the guards; validate on the lab VMs by attempting each refused transition.
3. `server_php` third, validated end to end on the apache-test VM (which already runs the apache2 renderer that reads it).
4. Directive Snippets last, because it is the only one touching vhost rendering; validate with a config-test failure injected on purpose.
5. **Rollback**: each surface is additive — removing a route removes the screen and leaves the data untouched. The only irreversible step is a snippet already applied to a vhost; re-rendering without it restores the previous file.

## Open Questions

- Should Interface Config expose the legacy `maintenance_mode` key even though nothing reads it, given that an operator migrating from ISPConfig will look for it? Current answer: no, same rule as everywhere else — but it is the most likely one to be asked for.
- Does `limit_directive_snippets` count snippets *attached* or snippets *visible*? The PHP panel counts visible; attached is more useful. Needs a decision before the limit is registered.
- Should a snippet be versioned, so editing one that is live on 40 sites is reviewable before it re-renders them all?
