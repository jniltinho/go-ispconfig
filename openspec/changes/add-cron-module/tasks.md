# Tasks: add-cron-module

## 1. Models and validators

- [x] 1.1 Add GORM model for the `cron` table with explicit `gorm:"column:…"` tags matching the ISPConfig3 schema (`id`, `sys_userid`, `sys_groupid`, `sys_perm_user`, `sys_perm_group`, `sys_perm_other`, `server_id`, `parent_domain_id`, `type` enum url/chrooted/full, `command`, `run_min`, `run_hour`, `run_mday`, `run_month`, `run_wday`, `log`, `active`); unit-test round-trip against MariaDB. Commit.
- [x] 1.2 Port `validate_cron.inc.php` schedule validators to Go: `run_time_format` for min/hour/mday/wday (charset, ranges, step/range syntax), `run_month_format` (same plus `@reboot`), and `MinFrequencyMinutes` computing `cron_min_freq` from the five fields; table-driven unit tests covering valid/invalid tokens and frequency edge cases (wrap-around, single value, `*/n`). Commit.
- [x] 1.3 Port `command_format`: reject CR/LF/NUL; for URL commands require `http`/`https`, hostname-shaped host after `{DOMAIN}` expansion, and no backslash; unit tests. Commit.
- [x] 1.4 Add type auto-derivation helper (port of `cron_edit.php::onSubmit`): URL regex → `url`; else owner `limit_cron_type` → `full` or `chrooted`; admin-owned site → `full`; unit tests. Commit.

## 2. Cron module (daemon events)

- [x] 2.1 Implement `internal/cron` Module: announce `cron_insert` / `cron_update` / `cron_delete`, register table hook for `cron`, map datalog actions `i`/`u`/`d` to events; gate on `server.web_server=1` + config.toml enablement; unit tests with fake registries. Commit.
- [x] 2.2 Wire the module into the daemon bootstrap next to web/dns modules; test that non-web servers register no cron hooks. Commit.

## 3. Client-job runner and plugin (scheduler execution)

- [ ] 3.1 Implement `ClientJobRunner`: in-process `robfig/cron` registry keyed by `cron.id`, Add/Replace/Remove under a mutex, compose 5-field expressions (spaces stripped) and handle `@reboot` as run-once-on-start; unit tests for register/update/delete and expression composition. Commit.
- [ ] 3.2 Implement daemon-start load of all `cron WHERE active='y' AND server_id=<this>` into the runner; integration test against MariaDB. Commit.
- [ ] 3.3 Implement URL executor: `{DOMAIN}` substitution, HTTP GET with timeout (default 7200s), TLS verify on, refuse insecure command chars; unit tests with `httptest`. Commit.
- [ ] 3.4 Implement full/chrooted executor: placeholder expansion (`{DOMAIN}`, `{DOCROOT_CLIENT}`, `[web_root]`, `{SITE_PHP}` from `server_php` join / `/usr/bin/php` fallback), argv split without shell, cwd `{document_root}/web`, chrooted path strip of `document_root`; unit tests. Commit.
- [ ] 3.5 Implement fail-safe privilege drop: resolve site uid/gid, refuse root, set `Credential` + `NoNewPrivileges`, context timeout + process-group kill; abort and log when drop fails; tests with stubbed/user-namespace-friendly helpers. Commit.
- [ ] 3.6 Implement execution log writer: `sys_log` rows with the `cron_run id=…` message convention when `log='y'` (and always on security aborts); bounded output tail; unit tests. Commit.
- [ ] 3.7 Implement cron plugin: subscribe to `cron_insert`/`cron_update`/`cron_delete`, resolve parent `web_domain`, skip when parent missing/root-owned (PHP WARN parity), drive the runner; never write under `crontab_dir`. Commit.
- [ ] 3.8 Implement legacy cutover on plugin load: delete `ispc_*` / `ispc_chrooted_*` (and `*.cron`) under getconf `cron.crontab_dir`, log removals, then load active jobs; tests with a temp dir. Commit.
- [ ] 3.9 Integration test: API-level insert of an active cron → datalog row → daemon process → runner holds the job; update to inactive removes it; delete removes it; assert no files created under a temp `crontab_dir`. Commit.

## 4. REST API

- [ ] 4.1 Register the cron entity under `/api/sites/crons` via `RegisterEntity` (or equivalent Sites wiring): CRUD with riud scopes, datalog `{old,new}` writes, `Prepare` forcing `server_id`/`sys_groupid` from parent and immutable `parent_domain_id` on update; permission tests (client/reseller/admin). Commit.
- [ ] 4.2 Wire schedule and command validators into create/update; type auto-derivation before store; handler/unit tests for validation failures (no datalog row). Commit.
- [ ] 4.3 Enforce non-admin client limits: `limit_cron` count, `limit_cron_type`, `limit_cron_frequency` via `MinFrequencyMinutes`; admin bypass; tests per limit class. Commit.
- [ ] 4.4 Add `GET /api/sites/crons/:id/runs` paginated history from `sys_log` filtered by the `cron_run id=<id>` convention, gated on read permission for the cron row; tests. Commit.
- [ ] 4.5 Swaggo annotations for all cron endpoints; `make swagger`; verify Swagger UI lists them; CI staleness check green. Commit.

## 5. Panel UI (Vue)

- [ ] 5.1 Add Sites → Cron sidebar section and job list view (DataTable: active, parent domain, schedule summary, type, command, log) with search/filter and add button; English i18n keys. Commit.
- [ ] 5.2 Cron form (TabbedForm single tab): parent domain select (vhosts; disabled on edit), five schedule fields, command with placeholder help, type display, log/active toggles; client-side validation mirroring API; inline API errors. Commit.
- [ ] 5.3 Run-history section/view on the edit screen listing runs from `/api/sites/crons/:id/runs` (start, status, exit, output tail) with empty state. Commit.
- [ ] 5.4 Pinia/store + router wiring for cron list/form/history; unit tests for store fetch/error paths. Commit.

## 6. E2E and docs

- [ ] 6.1 agent-browser E2E against the built binary: create URL cron, edit schedule/active/log, open run history (seed `sys_log` if needed), delete job, client isolation; screenshots to `docs/prints/`. Commit.
- [ ] 6.2 Module docs in `docs/cron-module.md`: architecture (module/plugin/runner), schedule fields, job types, privilege-drop guarantees, `sys_log` run convention, legacy crontab cutover, client limits, migration notes; link from ROADMAP. Commit.
