## 1. Interface Config (`sys_ini`)

- [ ] 1.1 Add `internal/api/systemconfig.go`: `GET|PUT /api/system/config[/:section]` over `sys_ini` row 1, reusing the merge-one-section semantics of `serverConfigSaveHandler` (locked re-read, INI re-serialise, datalog row). Gate every route with `admin_allow_system_config`.
- [ ] 1.2 Add the static field table for the rendered keys (`[misc] min_password_length`, `min_password_strength`, `ssh_authentication`; `[sites]`; `[mail]` welcome + rspamd levels) served as `GET /api/meta/forms/system_config`.
- [ ] 1.3 Add the staleness test: scan the codebase for `sections["…"]["…"]` reads of the global config and fail when a key has no rendered field.
- [ ] 1.4 Validate the submitted password policy (numeric, within the accepted maximum) and refuse a policy the panel cannot satisfy.
- [ ] 1.5 Add the Vue `System → Interface Config` view reusing `ServerConfigView`'s shape, plus the route, the sidebar entry and the i18n keys.
- [ ] 1.6 Integration test: change `min_password_length`, confirm a shorter database-user password is refused by the API afterwards.

## 2. Server Services

- [ ] 2.1 Add the `System → Server Services` list and form routes over the existing `server` entity, gated by `admin_allow_server_services`.
- [ ] 2.2 Implement the in-use guard in `BeforeUpdate`: clearing a role flag while rows owned by it exist on that server is refused with 422 naming the blocking table and count (`web_domain`, `mail_domain`, `dns_soa`, `web_database`).
- [ ] 2.3 Implement the last-web-server guard: the final active `web_server` cannot lose the flag or its `active` state.
- [ ] 2.4 Keep `server_id` immutable on update (same pattern as `serverIPEntity`).
- [ ] 2.5 Tests for each refused transition plus the allowed ones, and a lab-VM run attempting each.

## 3. Additional PHP versions (`server_php`)

- [ ] 3.1 Add the `server_php` entity (`internal/api/serverphp.go`) with the path/name fields, gated by `admin_allow_server_php`.
- [ ] 3.2 Shape-validate the path fields (absolute, plausible binary names); document that existence is validated on the node.
- [ ] 3.3 Refuse deleting a version referenced by any `web_domain`, naming the referencing sites.
- [ ] 3.4 Add `GET /api/meta/lookups/server-php?server_id=` and wire it into the site form's PHP-version select so only the site's server's versions are offered.
- [ ] 3.5 Add the Vue list and form views, route, sidebar entry and i18n keys.
- [ ] 3.6 End-to-end validation on the apache-test VM: create a version, pin a site to it, confirm the rendered FPM pool and jailkit/cron configuration use its binaries, and confirm a bad path surfaces as a datalog error on the site form.

## 4. Directive snippets

- [ ] 4.1 Add the `directive_snippets` entity (`internal/api/snippets.go`): name, type, body, `customer_viewable`, `required_php_snippets`, `active`; admin-only create/update/delete.
- [ ] 4.2 Refuse a type that does not match the target server's `server_type`, at save and at attach.
- [ ] 4.3 Add named insertion points to `nginx_vhost.conf.master` and the apache2 vhost template, and emit the snippet referenced by `web_domain.directive_snippets_id` there.
- [ ] 4.4 Run the snippet body through the existing directives blacklist and report stripped lines as datalog errors, exactly like `nginx_directives`.
- [ ] 4.5 Register `limit_directive_snippets` with the client limit hook; decide and document whether it counts attached or visible snippets (design open question).
- [ ] 4.6 Hide non-`customer_viewable` snippets from non-admin sessions in the site form datasource.
- [ ] 4.7 Add the Vue list and form views (with a monospace body editor), route, sidebar entry and i18n keys.
- [ ] 4.8 Failure-path test: attach a snippet with invalid syntax, assert the config test fails, the previous vhost stays on disk, no reload happens and the error is recorded.

## 5. Documentation and closing the parity table

- [ ] 5.1 Write `docs/system-module.md` covering the whole System surface, and move the parity table out of `docs/server-config-module.md` into it.
- [ ] 5.2 Update `docs/ROADMAP.md`, `docs/README.md` and the nginx module doc for the snippet insertion points.
- [ ] 5.3 Extend `e2e/panel-ui-qa-baseline.sh` so every new System section is opened and asserted.
- [ ] 5.4 Refresh `docs/screenshots/` with the new System screens.
