# Tasks: add-client-module

## 1. Models and domain foundations

- [x] 1.1 Add GORM models for `client_template`, `client_template_assigned`, `client_message_template` and `country` with explicit `gorm:"column:..."` tags matching `ispconfig3.sql`; confirm existing `Client` / `SysUser` / `SysGroup` models cover every column used by this change; unit-test round-trip against MariaDB. Commit.
- [x] 1.2 Implement client domain helpers: reseller detection (`limit_client != 0`), parent validation (one-level nesting), username uniqueness against `client` + `sys_user`, password hashing via `auth.HashPassword` for new passwords. Table-driven tests. Commit.
- [x] 1.3 Implement atomic provisioner: on client create, insert `sys_group` + `sys_user`, append group to parent reseller `sys_user.groups`, stamp ownership; on update sync username/password/language/modules; on delete remove identity rows and detach parent groups — all datalog-aware in one transaction. Tests for create/update/delete paths. Commit.

## 2. Limit templates and enforcement

- [ ] 2.1 Implement template assignment store (`client_template_assigned`) and legacy `template_additional` slash-list migration on save. Tests. Commit.
- [ ] 2.2 Port `apply_client_templates` merge rules (numeric add / -1 promote, cron frequency min, y/n less-restrictive, CHECKBOXARRAY and `*_servers` union, no `limit_client` on non-resellers, skip when `template_master = 0`). Pure function + DB apply; fixture tests from known master+additional pairs. Commit.
- [ ] 2.3 Implement parent limit cap (child cannot exceed parent `limit_*` / flags). Tests. Commit.
- [ ] 2.4 Implement and register `api.RegisterLimitHook`: resolve owning client, map entity names to `limit_*` + count queries by `sys_groupid`, admin bypass, `LimitError` keys. Integration tests: `dns_soa` / `web_domain` / child `client` at limit, unlimited, zero. Commit.

## 3. Daemon client module

- [ ] 3.1 Implement `internal/modules/client` Module: announce `client_insert`/`client_update`/`client_delete`, register table hook on `client`, map datalog `i`/`u`/`d`; optional config.toml disable; unit tests with fake registries. Commit.
- [ ] 3.2 Wire module into daemon bootstrap; verify nginx (or test double) `client_delete` handler still receives events when a client datalog delete is processed. Commit.

## 4. REST API

- [ ] 4.1 Client endpoints: create/get/list/update/delete/delete-everything, get-by-username, get-by-customer-no, get-by-groupid, id helpers, change-password, locked/canceled; redacted password fields; swaggo annotations; handler tests (403 cross-tenant, 422 validation). Commit.
- [ ] 4.2 Reseller endpoints (dedicated or filtered surface documented in swagger) with nesting guard and `limit_client` rules; tests. Commit.
- [ ] 4.3 Template CRUD + additional-template assign/list/remove with materialization; countries list; tests. Commit.
- [ ] 4.4 Message-template CRUD + send-message endpoint; optional SMTP transport behind config; welcome-on-create hook; tests with fake transport (no network). Commit.
- [ ] 4.5 Regenerate swagger (`make swagger` / `swag init`), verify Swagger UI lists all client endpoints, CI staleness check green. Commit.

## 5. Panel UI (Vue)

- [ ] 5.1 Client module navigation + Clients list + Resellers list (DataTable, search, status); `en.json` keys; router/modules.ts wiring. Commit.
- [ ] 5.2 Client form with TabbedForm tabs Info / Address / Limits / IP address; country select; write-only password; parent selector for admin; API error mapping. Commit.
- [ ] 5.3 Reseller form (same tabs, `limit_client` editable) and limit-template list/form. Commit.
- [ ] 5.4 Message-template list/form + Send message view (recipient, template load, delivery-disabled state). Commit.
- [ ] 5.5 Delete confirmation dialog with owned-resource counts and delete vs delete-everything actions. Commit.
- [ ] 5.6 agent-browser E2E: admin creates reseller + client; reseller isolation; template assign; message template; send attempt; delete confirm; screenshots to `docs/prints/`. Commit.

## 6. Integration and docs

- [ ] 6.1 End-to-end integration test against MariaDB: API client create → sys_user/sys_group rows → datalog → daemon raises `client_delete` on delete-everything after owning a site/zone (datalog child deletes present). Commit.
- [ ] 6.2 LimitHook integration: client at `limit_dns_zone = 1` with one zone cannot create a second via DNS API; unlimited can. Commit.
- [ ] 6.3 Module docs in `docs/` (client/reseller model, limit semantics, template merge, SMTP config, cascade delete, migration notes for existing ISPConfig clients). Commit.
