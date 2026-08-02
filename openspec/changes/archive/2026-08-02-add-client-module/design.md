# Design: Client module (clients, resellers, limits)

## Context

ISPConfig3's multi-tenancy is the `client` module:

1. `interface/web/client/` — tform forms writing `client`, `client_template`, `client_template_assigned`, `client_message_template`; on insert, PHP also creates a `sys_group` (name = username, `client_id` set) and a `sys_user` panel login bound to that group (`client_edit.php` / `reseller_edit.php`).
2. `interface/lib/classes/client_templates.inc.php` — materializes `template_master` + additional templates onto the `client` limit columns (additive merge for numeric limits, set-union for CHECKBOXARRAY / server lists).
3. `interface/lib/classes/remote.d/client.inc.php` — remote API surface (`client_add/get/update/delete`, `client_get_by_username`, `client_templates_get_all`, `client_change_password`, `client_delete_everything`, …).
4. `server/mods-available/client_module.inc.php` — table hook on `client` raising `client_insert` / `client_update` / `client_delete` so plugins (e.g. nginx `client_delete`) clean filesystem state.

The foundation already provides: identical DDL (tables exist), GORM models for `client` / `sys_user` / `sys_group`, riud scopes with reseller graph resolution via `client.parent_client_id` (`ResolveIdentity`), the vetoable `RegisterLimitHook` in `rest-api-core` (still a no-op), datalog writer (`server_id = 0` for tables without `server_id` — broadcast), and Vue panel primitives (`DataTable`, `TabbedForm`, i18n).

This change plugs the client domain into those hooks. The interface never touches the OS; daemon plugins already registered for `client_delete` (web/nginx) become live once this module raises the event.

## Goals / Non-Goals

**Goals:**
- Behavior-faithful port of client/reseller CRUD, automatic `sys_user`/`sys_group` lifecycle, limit templates and enforcement, message templates, and the panel module.
- Limit enforcement plugged into the existing `RegisterLimitHook` so sites/DNS (and future modules) create endpoints reject over-limit inserts without endpoint changes.
- `client_delete` event parity so existing plugin handlers run; interface-side cascade of owned records via datalog deletes (port of `client_delete_everything` / `client_del.php`).
- REST surface parity with `remote.d/client.inc.php` and UI parity with `client.tform.php` / `reseller.tform.php` / `client_template.tform.php` / `client_message.php`.

**Non-Goals:**
- Billing/invoicing, `client_circle`, domaintool (`domain_*.php`), reseller-of-reseller nesting beyond one level (see proposal Non-goals).
- Schema changes of any kind — never alter existing columns/types/names.
- Full mail stack: message delivery uses an optional generic SMTP relay in `config.toml` only; when unset, templates are managed/previewed but nothing is sent.

## Decisions

### D1 — Models on the immutable ISPConfig3 schema
GORM models map the existing tables with explicit `gorm:"column:..."` tags. `model.Client`, `model.SysUser` and `model.SysGroup` already exist; this change adds `ClientTemplate`, `ClientTemplateAssigned`, `ClientMessageTemplate` and `Country` (read-only lookup). Numeric `limit_*` columns are `int32` with ISPConfig semantics: `-1` = unlimited, `0` = disabled/none (except boolean-style enum limits `y`/`n`). No AutoMigrate shape changes; migrate only runs the embedded DDL.

### D2 — Reseller vs client is `limit_client`
Port of the PHP rule used everywhere (`client_add`, forms, template apply):
- **Client**: `limit_client = 0` (cannot own sub-clients). Form = `client.tform.php`.
- **Reseller**: `limit_client != 0` (`-1` unlimited or a positive quota). Form = `reseller.tform.php`. Reseller `sys_user.modules` always includes `client`.
- One nesting level only: a reseller cannot have `parent_client_id` pointing at another reseller (`limit_client != 0` parent with `limit_client != 0` child is rejected). Admin (`parent_client_id = 0`) may own both.
- Reseller list/endpoints filter `limit_client != 0`; client list filters `limit_client = 0` (or expose a unified list with a role discriminator — UI shows two lists like PHP).

### D3 — Atomic sys_group + sys_user provisioning on create
On successful client/reseller insert (API transaction, port of `client_edit.php::onInsert`):
1. Insert `client` row (password hashed; never return hash on read — omit or redact `password`).
2. Insert `sys_group` with `name = username`, `client_id = new client_id`, via datalog-aware writer.
3. Insert `sys_user` with `username`, `passwort` (bcrypt via `auth.HashPassword` for new passwords; verify path still accepts legacy `$6$`/`$1$`), `modules` = configured interface modules (+ `,client` when `limit_client > 0`), `startmodule` = `dashboard` if enabled else `client`, `typ = 'user'`, `active = 1`, `language`, `groups`/`default_group` = new groupid, `client_id` = new client_id.
4. If `parent_client_id > 0`, append the new groupid to the parent reseller's `sys_user.groups` CSV so the reseller can administrate the client (port of `add_group_to_user`).
5. Stamp the client row's ownership: for admin-created under a reseller, `sys_userid`/`sys_groupid` become the reseller's user/group; for direct admin clients, admin ownership (group 1).

On update: username change propagates to `sys_user.username` and `sys_group.name`; password change updates `sys_user.passwort` + `last_password_change`; language and `limit_client`-driven modules stay in sync. Parent reassignment (admin only) rewires group membership exactly as PHP.

On delete: remove `sys_user` rows and `sys_group` for that `client_id`, detach group from parent reseller's `groups` CSV, then delete the client row — all producing datalog rows so `client_delete` fires for plugins.

### D4 — Template materialization is pure domain logic
Port `client_templates.inc.php::apply_client_templates` / `update_client_templates`:
- `client.template_master` references a `client_template` (`template_type = 'm'` master) or `0` = custom (no auto-overwrite of limits).
- Additional templates live in `client_template_assigned` (`client_id`, `client_template_id`); legacy slash-separated `template_additional` is migrated into that table on first save (PHP does the same) and the column is cleared.
- Merge rules when master > 0: start from master template limits; for each additional template: numeric limits add when master is not `-1` (or promote to `-1` if additional is `-1`); `limit_cron_frequency` takes the minimum (≥ 1); `y`/`n` flags use the less-restrictive value (`y` wins except `force_suexec` where `n` is less restrictive); CHECKBOXARRAY / `*_servers` / `web_php_options` / `ssh_chroot` are set-unions; default servers are not overridden by additional templates when master already set them.
- Non-resellers MUST NOT receive `limit_client` from templates (prevents accidental client→reseller conversion).
- Materialization runs inside the same DB transaction as client create/update when templates change; resulting limit columns land on the `client` row and in the datalog `{old,new}` diff.

### D5 — LimitHook maps entity names to `client.limit_*`
Register a real `api.RegisterLimitHook` implementation at process startup:
1. Resolve the owning client from the identity (`sys_user.client_id` → `client`; for admin inserts on behalf of a client, the ownership group of the new row / explicit client selector).
2. Map foundation entity names already used by sites/DNS (and reserved for future modules) to columns and count queries scoped by the client's `sys_group.groupid`:

| Entity (hook name) | Limit column | Count source |
|---|---|---|
| `web_domain` (type vhost) | `limit_web_domain` | `web_domain` where `sys_groupid` in client group and type vhost |
| `web_domain` subdomain/alias | `limit_web_subdomain` / `limit_web_aliasdomain` | by type |
| `dns_soa` | `limit_dns_zone` | `dns_soa` by group |
| `dns_slave` | `limit_dns_slave_zone` | `dns_slave` by group |
| `dns_rr` | `limit_dns_record` | `dns_rr` by group |
| `client` | `limit_client` | child `client` rows under reseller group |
| (reserved) mail/ftp/shell/db/cron | matching `limit_*` | when those modules land |

3. Semantics: limit `< 0` → allow; limit `== 0` → veto; limit `> 0` → veto when `count >= limit`. Return `*api.LimitError` with i18n key `error.limit_<entity>` (rendered 403).
4. Feature flags (`limit_ssl`, `limit_cgi`, …) are enforced at field/validator level on the owning module's entity (not the create-count hook); this module documents the contract and may export a helper `ClientAllows(client, flag string) bool`.

Admin identities bypass count limits (PHP admin is not constrained by a client row); resellers are constrained by their own `limit_*` when creating for themselves, and cannot grant a child more than they have (tform `valuelimit` / cap on save — port as validator against parent client limits).

### D6 — Delete cascade is interface-side + event
Two operations (remote-API parity):
- **`client_delete`**: delete the client record + its `sys_user`/`sys_group` (and template assignments). Does not wipe sites/DNS; those become orphaned under a dead group unless the operator used delete-everything. Datalog `client` action `d` still raises `client_delete` for plugins (directory cleanup).
- **`client_delete_everything`**: before removing the client, walk the owned tables for `sys_groupid = client_group` (same list as PHP: `cron`, `dns_rr`, `dns_soa`, `dns_slave`, `ftp_user`, mail_*, `shell_user`, `web_domain`, `web_folder`, `web_folder_user`, `web_database`, `web_database_user`, child `client`, …) and datalog-delete each row so module plugins tear down resources. Then delete sys_user/sys_group/client.

UI delete confirmation lists counts of owned resources (port of `client_del.php` preview) and offers the full cascade as an explicit option.

### D7 — Daemon client module: events only
`internal/modules/client` (or package under the modules tree) ports `client_module.inc.php`:
- Announce `client_insert`, `client_update`, `client_delete`.
- Register table hook on `client` mapping datalog `i`/`u`/`d` → those events with `{old,new}` payloads.
- Always loaded when the daemon runs (no `server.web_server` gate — client rows are broadcast `server_id = 0`).
- No filesystem work in this module; plugins already subscribed (nginx `client_delete`) perform host-local cleanup.

### D8 — Messaging: templates + optional SMTP
`client_message_template` stores `template_type` ∈ {`welcome`, `gdpr`, `other`}, `template_name`, `subject`, `message` with placeholders (`{username}`, `{password}`, `{company_name}`, contact fields, etc. — same substitution set as PHP welcome send).
- On client create, if a `welcome` template exists for the creator's group and the client has an email, render and send (when SMTP configured).
- Panel "Send message" form ports `client_message.php`: recipient = one client, all clients (admin), or all children (reseller); subject/body free text or from a template.
- SMTP: optional `[smtp]` (or equivalent) section in `config.toml` (host, port, TLS, auth). When missing/disabled, send endpoints return a clear "delivery disabled" error for actual send, while template CRUD and preview still work. No dependency on the future mail module.

### D9 — REST surface under `/api/clients`
Port of `remote.d/client.inc.php` as REST (session/token auth, riud scopes), style of `internal/api/sites.go` / `dns.go`:
- Clients: list/get/create/update/delete, get-by-username, get-by-customer-no, get-by-groupid, get-id-from-sys-userid, change-password, set locked/canceled.
- Resellers: list/create/update (or unified client resource with `role` / `limit_client` discriminator — handlers remain explicit in swagger).
- Templates: CRUD + list; additional-template assign/list/remove (`client_template_additional_*`).
- Messaging: message-template CRUD; send-message endpoint.
- Countries: read-only list for address forms.
- Swaggo annotations on every handler; `make swagger` in tasks.

Password fields are write-only; list/get never include `password` / `passwort` / `tmp_data` / private keys.

### D10 — UI mirrors tform tabs
Vue Client module (nav entry + sections):
- **Clients** list (DataTable: company, contact, username, customer_no, locked/canceled; search) and form tabs: **Info** (username/password/language/theme/locked/canceled/can_use_api/parent for admin), **Address** (contact + country select from `country`), **Limits** (all `limit_*`, defaults servers, template master/additional), **IP address** (`limit_web_ip`).
- **Resellers** list/form (same tabs; `limit_client` editable).
- **Limit templates** list/form (`template_type` m/a, name, limits tab).
- **Message templates** list/form; **Send message** action form.
- All strings via `en.json`. Validation errors from API shown per field. Permission-gated: resellers see only their clients; clients without `limit_client` do not see the module (or see read-only self profile if exposed later — out of scope; PHP clients typically have no client module).

### D11 — Permissions and datalog
Every mutation goes through foundation repository scopes (`sys_userid` / `sys_groupid` / `sys_perm_*`) and writes `{old,new}` JSON to `sys_datalog`. Client/template tables have no `server_id` → journal `server_id = 0` (all daemons process; required for multi-server `client_delete` cleanup). Policy: client module routes require the `client` module flag on `sys_user.modules` (and admin always allowed), matching `check_module_permissions('client')`.

## Risks / Trade-offs

- [Partial cascade leaves orphan sites] → delete-everything is explicit; simple delete documents the risk; UI confirmation lists owned resource counts.
- [Template re-apply on every save doubles limits if merge is wrong] → port PHP's "master required; additional from `client_template_assigned` only" guard; unit tests with known master+additional fixtures.
- [LimitHook entity-name drift vs module registrations] → single map in one package, table-driven tests; unknown entity names no-op (allow) so unrelated admin entities keep working.
- [Password in welcome email] → only on create when plaintext is still in the request; never re-send stored hashes; log redaction required.
- [Reseller privilege escalation via inflated child limits] → cap child limits to parent limits on save (valuelimit port).
- [Broadcast datalog `server_id=0` amplifies daemon work] → same as PHP; handlers must be cheap no-ops when no local resources exist.

## Migration Plan

- Code only — no DDL change. Migrated ISPConfig3 databases already have `client` / templates / sys_user / sys_group rows; they become manageable immediately.
- Fresh installs: seed remains admin only (`sys_user` 1 / `sys_group` 1); operators create clients through the new UI/API.
- LimitHook starts enforcing as soon as this module registers — existing over-limit data is not deleted; only new creates are blocked.
- Rollback: disable client routes/module in config; data remains; LimitHook can be unregistered to return to no-op (not recommended in multi-tenant prod).

## Open Questions

- Should a non-reseller client be allowed a read-only "My account" view of their own `client` row (PHP has limited self-edit paths)? Default: out of scope; only admin/reseller manage clients.
- Exact `config.toml` key names for SMTP (`[smtp]` vs `[mail.relay]`) — align with whatever the mail module proposal adopts when it lands; this change documents one section and keeps a small interface so the mail module can replace the transport later.
- Whether `client_delete` (without everything) should refuse when owned resources exist — PHP allows it; keep parity, warn in UI.
