## 1. Server Config — Server tab

- [x] 1.1 Add `getconf.ServerSection` decoding the two consumed keys (`ip_address`, `ssh_port`) and wire it into `GetServerConfig`; switch `internal/api/sitesdb.go` and `cmd/daemon.go` off `cfg.Raw["server"][…]`.
- [x] 1.2 Extend the form generator to emit the `server` tab: the 26 legacy fields in tform order plus `ssh_port`, split by a collapsible LEGEND into applied and compatibility groups, with labels from `en_server_config.lng`.
- [x] 1.3 Declare `serverCompatKeys` in `internal/api/serverconfigform.go` and extend `TestServerConfigFormMatchesGetconf`: every decoded key must be rendered, and every extra rendered key must be in that list.
- [x] 1.4 Validate `ssh_port` (1–65535) on save; keep the daemon's port-22 fallback for an unparseable stored value, with a test for both.
- [x] 1.5 Add the i18n labels and verify the tab against `.20` field by field (order, labels, select options).
- [ ] 1.6 Lab validation: set `ip_address`, create a client database, confirm the suggested host follows; set `ssh_port`, re-render the firewall, confirm the allow rule.

## 2. Main Config (`sys_ini`)

- [x] 2.1 Add `internal/api/systemconfig.go`: `GET|PUT /api/system/config[/:section]` over `sys_ini` row 1, reusing the locked-re-read + merge + datalog path of `serverConfigSaveHandler`. Gate every route with `admin_allow_system_config`.
- [x] 2.2 Add the static field table (Sites, Mail, Misc) served as `GET /api/meta/forms/system_config`, restricted to the keys the Go code reads.
- [x] 2.3 Add the staleness test: scan for `sections["…"]["…"]` reads of the global config and fail when a key has no rendered field.
- [x] 2.4 Validate the password policy on save (numeric, within the accepted maximum) and refuse one the panel cannot satisfy.
- [x] 2.5 Add the Vue `System → Main Config` view reusing `ServerConfigView`'s shape, plus route, sidebar entry and i18n keys.
- [x] 2.6 Integration test: raise `min_password_length`, confirm a shorter database-user password is refused by the API.

## 2b. Follow-up found by seeding sys_ini

- [ ] 2b.1 An admin creating a database user **without** `parent_domain_id` now fails with `database_user_error_regex`. Seeding `dbuser_prefix=c[CLIENTID]` made a latent path reachable: with no site to resolve the client from, `expandSitesPrefix` keeps the literal placeholder, and `c[CLIENTID]` contains `[`/`]`, which the name regex forbids. It matches ISPConfig (where the key is always populated), but the error blames the name the operator typed correctly. Return an actionable error naming the missing site instead — or resolve the client from `client_group_id` when it is given, which is the information the caller already supplied.

## 3. Remote Users — function groups

- [ ] 3.1 Extract the 58 function groups from the seven `lib/remote.conf.php` files into a static Go table (label, module, function names, implied scopes); assert at build time that every implied scope is valid.
- [ ] 3.2 Serve it as `GET /api/tokens/function-groups`, so the form and the parser share one source of truth.
- [ ] 3.3 Extend `apitoken.ParseMeta`: a bare CSV of legacy function names translates through the table into scopes; a `scopes=` value is untouched. Table-driven tests for both, plus the unmappable case.
- [ ] 3.4 Rework the token form to render the groups grouped by module, derive the checked state from the stored scopes, and display the resulting scope list.
- [ ] 3.5 Vitest coverage for the derivation: groups sharing a scope check together, a CLI-minted token renders, and the scope list dedupes.
- [ ] 3.6 Compare the form against `.20` side by side and correct label drift.

## 4. Documentation and validation

- [ ] 4.1 Update `docs/server-config-module.md`: the Server tab is no longer an omission; list the compatibility fields and what "stored, not applied" means.
- [ ] 4.2 Write `docs/system-module.md` covering the whole System surface, moving the parity table out of `docs/server-config-module.md`.
- [ ] 4.3 Update `docs/api-tokens.md` with the function-group mapping and the legacy compatibility path.
- [ ] 4.4 Note in `complete-system-module` that its `interface-config` capability is superseded here.
- [ ] 4.5 Extend `e2e/panel-ui-qa-baseline.sh` so the new System sections are opened and asserted.
- [ ] 4.6 Full lab pass on `192.168.56.12`, compared screen by screen against `192.168.56.20`; refresh `docs/screenshots/`.
