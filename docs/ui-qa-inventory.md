# UI QA Inventory — list & form screens

Living catalog of Vue list/form screens under `frontend/src`, mapped to router
paths, API endpoints, OpenSpec modules, and QA status.

**Branch:** `feat/ui-forms-tables-qa`
**Change:** `openspec/changes/ui-forms-tables-qa`
**Source of truth:** `frontend/src/router.ts`, `frontend/src/modules.ts`, views under `frontend/src/views/**`
**Generated:** 2026-08-02 (task 1.1)

## Legend

| Status | Meaning |
|---|---|
| **ready** | Implemented on this branch / main; subject to QA |
| **N/A** | Module not merged yet — skip until available |
| **placeholder** | Route exists but shows `ModulePlaceholder` or shared stub only |

| Kind | Meaning |
|---|---|
| **list** | DataTable (or equivalent) with server-side page/filter |
| **form** | Create/edit form (`EntityForm`, `TabbedForm`, or dedicated form) |
| **wizard** | Multi-step flow |
| **action** | One-shot action view (no list) |
| **shell** | Layout-only / placeholder |

Shared building blocks:

| Component | Path | Role |
|---|---|---|
| `DataTable` | `frontend/src/components/DataTable.vue` | Lists: filter row, pagination, empty/loading |
| `TabbedForm` | `frontend/src/components/TabbedForm.vue` | Tabbed entity forms |
| `EntityForm` | `frontend/src/views/sites/EntityForm.vue` | Metadata-driven CRUD from `GET /api/meta/forms/{entity}` |
| `MailList` | `frontend/src/views/mail/MailList.vue` | Generic list reused by mail + firewall |
| `ClientList` | `frontend/src/views/clients/ClientList.vue` | Clients + resellers lists |

Acceptance criteria for each screen: see `docs/ui-qa-checklist.md` (task 1.2).

---

## Auth / shell

| Route | Name | View | Kind | API | Module | Status | Notes |
|---|---|---|---|---|---|---|---|
| `/login` | `login` | `LoginView.vue` | form | `POST /api/login`, `GET /api/session` | auth | ready | Public |
| `/dashboard` | `dashboard` | `DashboardView.vue` | shell | session modules | dashboard | ready | Dashlets per enabled module |
| `/` | — | redirect → `/dashboard` | — | — | — | ready | |

---

## Sites module

Sidebar (`modules.ts`): Websites, Folders, Databases, Database users.
**Not yet on this branch:** Cron (`feat/cron-module` → `/sites/crons`), FTP/Shell (`feat/ftp-shell-module` → `/sites/ftp-users`, `/sites/shell-users`).

| Route | Name | View | Kind | API | Form entity (`/api/meta/forms/…`) | Status | Filter columns (list) |
|---|---|---|---|---|---|---|---|
| `/sites` | `sites` | `WebDomainList.vue` | list | `GET/DELETE /api/sites/web-domains` | — | ready | active, server_id, domain, type |
| `/sites/domains/new` | `sites-domain-new` | `EntityForm.vue` | form | `POST /api/sites/web-domains` | `web-domains` | ready | |
| `/sites/domains/:id` | `sites-domain-edit` | `EntityForm.vue` | form | `GET/PUT /api/sites/web-domains/:id` | `web-domains` | ready | |
| `/sites/folders` | `sites-folders` | `WebFolderList.vue` | list | `GET/DELETE /api/sites/web-folders` | — | ready | (view columns) |
| `/sites/folders/new` | `sites-folder-new` | `EntityForm.vue` | form | `POST /api/sites/web-folders` | `web-folders` | ready | |
| `/sites/folders/:id` | `sites-folder-edit` | `EntityForm.vue` | form | `GET/PUT /api/sites/web-folders/:id` | `web-folders` | ready | |
| `/sites/folders/:folderId/users` | `sites-folder-users` | `WebFolderUserList.vue` | list | `GET/DELETE /api/sites/web-folder-users` (+ `web_folder_id` filter) | — | ready | scoped by folder |
| `/sites/folders/:folderId/users/new` | `sites-folder-user-new` | `EntityForm.vue` | form | `POST /api/sites/web-folder-users` | `web-folder-users` | ready | fixed `web_folder_id` |
| `/sites/folders/:folderId/users/:id` | `sites-folder-user-edit` | `EntityForm.vue` | form | `GET/PUT …/web-folder-users/:id` | `web-folder-users` | ready | |
| `/sites/databases` | `sites-databases` | `DatabaseList.vue` | list | `GET/DELETE /api/sites/databases` | — | ready | active, remote_access, `_server_name`, `_parent_domain`, `_database_user`, database_name (decorated filters) |
| `/sites/databases/new` | `sites-database-new` | `DatabaseForm.vue` | form | `POST /api/sites/databases` (+ domain/user lookups) | via `EntityForm` + options | ready | |
| `/sites/databases/:id` | `sites-database-edit` | `DatabaseForm.vue` | form | `GET/PUT /api/sites/databases/:id` | — | ready | |
| `/sites/database-users` | `sites-database-users` | `DatabaseUserList.vue` | list | `GET/DELETE /api/sites/database-users` | — | ready | (view columns incl. client label) |
| `/sites/database-users/new` | `sites-database-user-new` | `EntityForm.vue` | form | `POST /api/sites/database-users` | `database-users` | ready | |
| `/sites/database-users/:id` | `sites-database-user-edit` | `EntityForm.vue` | form | `GET/PUT …/database-users/:id` | `database-users` | ready | readonly: `server_id`, `database_user_prefix` |
| `/sites/crons` | — | — | list/form | (cron branch) | — | **N/A** | `add-cron-module` not merged |
| `/sites/ftp-users` | — | — | list/form | (ftp branch) | — | **N/A** | `add-ftp-shell-module` not merged |
| `/sites/shell-users` | — | — | list/form | (ftp branch) | — | **N/A** | `add-ftp-shell-module` not merged |

OpenSpec modules: sites/nginx (web domains, folders), database-module, add-cron-module, add-ftp-shell-module.

---

## DNS module

Sidebar: Zones, Slave zones, Templates (adminOnly).

| Route | Name | View | Kind | API | Form entity | Status | Notes |
|---|---|---|---|---|---|---|---|
| `/dns` | `dns` | `ZoneList.vue` | list | `GET/DELETE /api/dns/zones` | — | ready | active, server_id, origin, ns, mbox |
| `/dns/wizard` | `dns-wizard` | `ZoneWizard.vue` | wizard | zone create APIs | — | ready | |
| `/dns/zones/new` | `dns-zone-new` | `EntityForm.vue` | form | `POST /api/dns/zones` | `zones` | ready | manual create |
| `/dns/zones/:id` | `dns-zone-edit` | `ZoneForm.vue` | form | zone + records | — | ready | includes `RecordGrid` |
| `/dns/slave-zones` | `dns-slave-zones` | `SlaveZoneList.vue` | list | `GET/DELETE /api/dns/slave-zones` | — | ready | |
| `/dns/slave-zones/new` | `dns-slave-zone-new` | `EntityForm.vue` | form | `POST /api/dns/slave-zones` | `slave-zones` | ready | |
| `/dns/slave-zones/:id` | `dns-slave-zone-edit` | `EntityForm.vue` | form | `GET/PUT …/slave-zones/:id` | `slave-zones` | ready | |
| `/dns/templates` | `dns-templates` | `TemplateList.vue` | list | `GET/DELETE /api/dns/zone-templates` | — | ready | adminOnly sidebar |
| `/dns/templates/new` | `dns-template-new` | `EntityForm.vue` | form | `POST /api/dns/zone-templates` | `zone-templates` | ready | |
| `/dns/templates/:id` | `dns-template-edit` | `EntityForm.vue` | form | `GET/PUT …/zone-templates/:id` | `zone-templates` | ready | |
| PowerDNS UI | — | — | — | — | — | **N/A** | `add-dns-powerdns-module` separate change |

OpenSpec: dns-module; PowerDNS out of scope for this QA pass.

---

## Mail module

Sidebar: Domains, Mailboxes, Aliases, Forwards, Catchalls, Alias domains, Transports, Spam policies (admin), Spam users, WB lists, Access.

Generic list: `MailList.vue` with route `props` for `apiBase`, columns, i18n keys.
Generic form: `EntityForm` except domains (`DomainForm` wraps EntityForm + DKIM).

| Route | Name | Kind | API list/CRUD | Form entity | Status |
|---|---|---|---|---|---|
| `/mail` | `mail-domains` | list | `/api/mail/domains` | — | ready |
| `/mail/domains/new` | `mail-domain-new` | form | `/api/mail/domains` | `domains` | ready (`DomainForm`) |
| `/mail/domains/:id` | `mail-domain-edit` | form | `/api/mail/domains/:id` + `POST …/generate-dkim` | `domains` | ready |
| `/mail/mailboxes` | `mail-mailboxes` | list | `/api/mail/mailboxes` | — | ready |
| `/mail/mailboxes/new` · `…/:id` | mailbox form | form | `/api/mail/mailboxes` | `mailboxes` | ready |
| `/mail/aliases` + new/edit | aliases | list/form | `/api/mail/aliases` | `aliases` | ready |
| `/mail/forwards` + new/edit | forwards | list/form | `/api/mail/forwards` | `forwards` | ready |
| `/mail/catchalls` + new/edit | catchalls | list/form | `/api/mail/catchalls` | `catchalls` | ready |
| `/mail/alias-domains` + new/edit | alias-domains | list/form | `/api/mail/alias-domains` | `alias-domains` | ready |
| `/mail/transports` + new/edit | transports | list/form | `/api/mail/transports` | `transports` | ready |
| `/mail/spamfilter/policies` + new/edit | policies | list/form | `/api/mail/spamfilter/policies` | `policies` | ready (admin) |
| `/mail/spamfilter/users` + new/edit | spam users | list/form | `/api/mail/spamfilter/users` | `users` | ready |
| `/mail/spamfilter/wblists` + new/edit | wblists | list/form | `/api/mail/spamfilter/wblists` | `wblists` | ready |
| `/mail/access` + new/edit | access | list/form | `/api/mail/access` | `access` | ready |

OpenSpec: mail-module.

---

## Client module

Sidebar: Clients, Resellers (admin), Limit templates (admin), Message templates, Send message.

| Route | Name | View | Kind | API | Status | Notes |
|---|---|---|---|---|---|---|
| `/clients` | `clients` | `ClientList.vue` | list | `/api/clients` | ready | company, contact, username, customer_no, locked |
| `/clients/new` | `client-new` | `ClientForm.vue` | form | `/api/clients` | ready | |
| `/clients/:id` | `client-edit` | `ClientForm.vue` | form | `/api/clients/:id` + templates assign | ready | delete via `ClientDeleteDialog` |
| `/clients/resellers` | `resellers` | `ResellerList.vue` → ClientList | list | `/api/resellers` | ready | admin |
| `/clients/resellers/new` | `reseller-new` | `ClientForm.vue` | form | `/api/resellers` | ready | `reseller: true` |
| `/clients/resellers/:id` | `reseller-edit` | `ClientForm.vue` | form | `/api/resellers/:id` | ready | |
| `/clients/limit-templates` | `limit-templates` | `LimitTemplateList.vue` | list | `/api/client-templates` | ready | admin |
| `/clients/limit-templates/new` · `…/:id` | limit form | `EntityForm` | form | `/api/client-templates` | `client-templates` | ready |
| `/clients/message-templates` | `message-templates` | `MessageTemplateList.vue` | list | `/api/client-message-templates` | ready | |
| `/clients/message-templates/new` · `…/:id` | msg form | `EntityForm` | form | `/api/client-message-templates` | `client-message-templates` | ready |
| `/clients/send-message` | `send-message` | `SendMessageView.vue` | action | `POST /api/clients/send-message` | ready | |

Also used: `GET /api/countries`, `GET /api/clients/template-options`, resource-counts / delete-everything on clients.

OpenSpec: client-module.

---

## System module

Sidebar: Server config & Users → **placeholder** (`/system`); Firewall (admin); Migration (admin).
Monitor module: **N/A** (`add-monitor-module`).

| Route | Name | View | Kind | API | Status | Notes |
|---|---|---|---|---|---|---|
| `/system` | `system` | `ModulePlaceholder.vue` | shell | — | placeholder | server config / users not implemented |
| `/system/firewall` | `system-firewall` | `MailList.vue` | list | `/api/firewall` | ready | adminOnly; columns active, server_id, tcp/udp ports |
| `/system/firewall/new` | `system-firewall-new` | `EntityForm.vue` | form | `POST /api/firewall` | `firewall` | ready | adminOnly |
| `/system/firewall/:id` | `system-firewall-edit` | `EntityForm.vue` | form | `GET/PUT /api/firewall/:id` | `firewall` | ready | readonly `server_id` |
| `/system/migration` | `system-migration` | `MigrationWizard.vue` | wizard | `/api/system/migration/*` | ready | adminOnly; connect, inventory, dry-run, execute, progress SSE, reset-passwords |

OpenSpec: firewall-module, migration wizard; server users/config still placeholder.

---

## Existing E2E coverage (agent-browser)

| Script | Makefile | Scope |
|---|---|---|
| `e2e/panel-theme.sh` | `make e2e-theme` | login, nav, list filters, tabbed form, theme traits |
| `e2e/panel-clients.sh` | `make e2e-clients` | clients/resellers/templates/send-message/delete |
| `e2e/panel-mail.sh` | `make e2e-mail` | mail domains/mailboxes/spam/transport |
| `e2e/panel-firewall.sh` | `make e2e-firewall` | firewall list/form |
| `e2e/panel-database.sh` | `make e2e-database` | databases + users |
| `e2e/panel-ui-qa-baseline.sh` | (task 1.3) | login + every sidebar section opens |

Screenshots for local validation: `docs/prints/` (gitignored / never committed).
Curated approved shots: `docs/screenshots/`.

---

## Counts (this branch)

| Module | List screens | Form/wizard/action | N/A or placeholder |
|---|---:|---:|---:|
| Auth/dashboard | 0 | 2 | 0 |
| Sites | 5 | 10 | 3 (cron/ftp/shell) |
| DNS | 3 | 7 | 1 (PowerDNS) |
| Mail | 11 | 12 | 0 |
| Client | 4 | 7 | 0 |
| System | 1 | 3 | 2 (placeholder + monitor) |
| **Total ready for QA** | **~24 lists** | **~41 forms/wizards** | |

---

## QA batch order (from tasks.md)

1. **2.x** Sites web domains → databases/users → cron (when merged) → ftp/shell (when merged)
2. **3.x** Mail → DNS → Clients → System (firewall/migration)
3. **4.x** Cross-cutting DataTable / EntityForm / theme (Claude Fable 5 polish on 4.3)
4. **5.x** Lab `.10` redeploy + archive

Update pass/fail status in `docs/ui-qa-checklist.md` as each batch lands.
)
