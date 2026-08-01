# Tasks: port-ispconfig3-to-go (foundation)

Rule: every finished task = validated (tests pass) + conventional commit. Cross-agent validation (grok/codex/opencode/cursor-agent) per AGENTS.md after each group.

## 1. Repository and skeleton

- [ ] 1.1 Create GitHub repo `go-ispconfig` with `gh`, init git, push initial layout (README.md stub, LICENSE, .gitignore excluding: `docs/prints/`, `base/`, `.vagrant/`, `*.box`, `testdata/`, `config.toml`, DB files, built binary — no Vagrant images, no testdata, no sensitive data in the repo)
- [x] 1.2 `go mod init go-ispconfig` (plain module name, binary named `go-ispconfig` — project convention, see go-cubemail); add Echo v5, GORM+mysql, Cobra, Viper, testify
- [x] 1.3 Cobra skeleton: `cmd/{root,serve,daemon,migrate,init,version}.go`, ldflags version injection, `main.go` with `embed.FS` pass-through (go-cubemail pattern)
- [x] 1.4 `internal/config`: Viper load (flag → ./config.toml → /etc/go-ispconfig/), env prefix `GOISP_`, `config.toml.example`; `init` command generates default config
- [x] 1.5 Makefile (`all: clean frontend build`, CGO_ENABLED=0, trimpath, UPX build-prod), golangci-lint config with godoc-enforcing rules

## 2. Database layer

- [ ] 2.1 Embed `ispconfig3.sql` (copied from base/ispconfig3_install/install/sql/); `migrate` executes it on empty DB, detects existing ISPConfig schema via `server.dbversion` and skips DDL
- [ ] 2.2 GORM models with explicit column tags: sys_user, sys_group, sys_datalog, sys_remoteaction, sys_config, sys_ini, sys_log, sys_session, server, server_ip, server_php, client, web_domain, web_folder, web_folder_user, dns_soa, dns_rr, dns_slave, dns_template
- [ ] 2.3 Seed: admin user (generated password printed once), admin group, local server row, sys_config defaults — skipped when adopting existing DB
- [ ] 2.4 Schema-identity test: migrate empty DB, diff `SHOW CREATE TABLE` against import of original ispconfig3.sql (docker MariaDB integration test)
- [ ] 2.5 `getconf` port: parse `server.config` INI text into typed structs (web/dns sections), `sys_ini`/`sys_config` accessors + tests

## 3. Auth and permissions

- [ ] 3.1 Password verify: bcrypt + legacy crypt `$6$`/`$1$` (crypt lib), rehash to bcrypt only when `auth.rehash_legacy` enabled (default off — rollback safety); tests with real ISPConfig3 hashes
- [ ] 3.2 Sessions: sys_session-backed store, HTTP-only SameSite=Strict cookie + per-session CSRF token on mutating endpoints + bearer transport for non-browser clients
- [ ] 3.3 riud permission GORM scope `WithPerm(user, flag)` + repository base; access levels admin/reseller/client with full reseller graph (sys_user.groups, default_group, parent_client_id); brute-force lockout (attempts_login)
- [ ] 3.4 Permission test suite: cross-client isolation, reseller→client-group access, cross-reseller isolation, group access, admin bypass, 403 without flag
- [ ] 3.5 Security policy flags from security_settings.ini in sys_config, enforced by API middleware (superadmin = sys_user id 1); tests

## 4. sys_datalog engine

- [ ] 4.1 Datalog writer: transactional insert with JSON `{old,new}` diff (changed fields only on update), dbidx `<pk>:<value>`; wired into repository base
- [ ] 4.2 Module/Plugin registries: typed interfaces, table-hook map, announced-events map (startup error on unannounced registration), event raise with decoded data
- [ ] 4.3 Daemon: persistent process, ticker loop (configurable, default 10s), batch 1000, ordered processing, `server.updated` advance, skip-tick when busy, single-instance guard
- [ ] 4.4 Services registry with delayed dedup restarts (reload→restart upgrade); systemd unit files for serve + daemon
- [ ] 4.4b Remote action registry: RegisterAction/RaiseAction, sys_remoteaction polling after datalog, ok/warning/error state; non-JSON datalog row quarantine (skip + datalogError, per-row server.updated advance); multi-server startup guard (mirror_server_id/multiple active servers → refuse to start)
- [ ] 4.5 Internal job scheduler: cron-spec jobs registered in code, last-run/status persistence, API endpoint listing jobs; datalog pruning job
- [ ] 4.6 End-to-end engine test: write datalog → daemon cycle → hook fired → service action recorded

## 5. Master template engine

- [ ] 5.1 Lexer/renderer for `<tmpl_var>`, `<tmpl_if op value>`, `<tmpl_else>`, `<tmpl_unless>`, `<tmpl_loop>` (subset used by nginx/bind templates)
- [ ] 5.2 Golden-file tests: nginx_vhost.conf.master, php_fpm_pool.conf.master, bind_pri.domain.master, bind_named.conf.local.master + .slave rendered against fixtures

## 6. REST API core

- [ ] 6.1 Echo v5 bootstrap: SPA static serve from embed, `/api` group, error handler with i18n keys, request logging (slog)
- [ ] 6.2 Auth endpoints: login/logout/session-info; Swagger security definitions
- [ ] 6.3 CRUD framework: entity definitions (fields, validators, tabs), generic handlers wiring validation → limit hook (no-op registration, vetoable) → permission scope → datalog writer
- [ ] 6.4 Validators: REGEX, UNIQUE, NOTEMPTY, ISEMAIL, ISINT, ISPOSITIVE, ISIPV4, ISIPV6, ISIP, CUSTOM + 422 field-error map + tests
- [ ] 6.5 Form metadata endpoint `/api/meta/forms/<entity>`
- [ ] 6.6 swaggo annotations on all endpoints; embedded Swagger UI at `/swagger/`; CI staleness check

## 7. Panel skeleton (frontend)

- [ ] 7.1 Vite + Vue 3 + TS + Tailwind v4 + Pinia scaffold in `frontend/`, outDir `../web/dist`, dev proxy `/api`; vendored fonts in `web/static/fonts`
- [ ] 7.2 Minimal theme tokens only (brand #C70F19, bg #F2F5F7, text #3C444B, radius 0) + Lucide icon map — full visual polish (dark mode, shadows, dashlets) belongs to add-panel-ui-theme, don't duplicate
- [ ] 7.3 i18n composable + `en.json`; missing-key fallback with dev warning
- [ ] 7.4 Layout: login page, topbar (logo, module tabs, logout, global search), sidebar, content router
- [ ] 7.5 Primitives: DataTable (server pagination, inline thead filters, row actions, zebra) and TabbedForm (rendered from form metadata, Save/Cancel)
- [ ] 7.6 agent-browser E2E: login flow + navigation smoke test against built binary; screenshots to docs/prints/ (local) and curated ones to docs/screenshots/

## 8. Docs and release

- [ ] 8.1 README.md (English): overview, install, quick start; docs/ARCHITECTURE.md with diagrams (datalog flow, module/plugin registries)
- [ ] 8.2 AGENTS.md: environment bootstrap, build/test/validate commands, cross-agent validation workflow
- [ ] 8.3 GitHub Actions: CI (lint, test, swagger staleness) + release workflow on tag `v*` (go-cubemail pattern)
- [ ] 8.4 docs/MIGRATION.md: shared-DB cutover procedure from PHP ISPConfig3
