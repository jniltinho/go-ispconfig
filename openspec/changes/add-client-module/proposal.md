# Proposal: Client Module (clients, resellers, limits)

> Roadmap phase 2 — proposal only for now; design/specs/tasks will be authored when the module is scheduled. Depends on the foundation change `port-ispconfig3-to-go`.

## Why

Every hosting panel needs client/reseller management: without it, only the admin can own sites, DNS zones and mailboxes, and there is no way to enforce per-customer resource limits. This ports the ISPConfig3 client module so go-ispconfig can serve multi-tenant setups and so migrated `client` records from an existing ISPConfig3 database become manageable.

## What Changes

- **Client and reseller CRUD** backed by the existing schema tables `client`, `client_template`, `client_template_assigned`, `client_message_template` (already created by the foundation DDL; GORM models added here).
- **Automatic sys_user/sys_group provisioning**: creating a client creates its `sys_group` and a `sys_user` (panel login) bound to that group, mirroring `interface/web/client/client_edit.php` behavior; deleting a client cascades ownership cleanup (raises `client_delete` so other modules remove owned records).
- **Limit templates**: `client_template` holds resource limits (limit_web_domain, limit_dns_zone, limit_maildomain, limit_mailbox, limit_ftp_user, limit_shell_user, limit_database, limit_cron, quotas, allowed servers…); templates are assigned via `client_template_assigned` (main + additional, additive merge) and materialized onto the `client` row.
- **Limit enforcement in the API**: the foundation's CRUD framework (`rest-api-core`) already ships a vetoable limit-check hook (no-op by default); this module plugs the real enforcement into it — checking the owning client's limits before insert (e.g. count of `web_domain` rows vs `limit_web_domain`) and rejecting with a clear error, port of the interface-side limit checks in `client/lib/` and tform validators. No retrofit of existing endpoints is needed: they already pass through the hook.
- **Daemon-side client module**: Go port of `server/mods-available/client_module.inc.php` — table hook on `client` raising `client_insert`/`client_update`/`client_delete` events for plugins.
- **Client UI module** in the Vue panel: client list/edit (address, contact, limits tabs), reseller list/edit, limit template list/edit, and the client message send form (`client_message.php`) using `client_message_template` (welcome mail, password reset templates).
- **REST API endpoints** mirroring `interface/lib/classes/remote.d/client.inc.php`: client_add/get/update/delete, client_templates_get_all, client_get_by_username, etc., with Swagger docs.

## Capabilities

### New Capabilities

- `client-management`: client/reseller CRUD, automatic sys_user/sys_group lifecycle, client deletion cascade, REST API surface.
- `client-limits`: limit templates, template assignment/merge, per-resource limit enforcement hooks used by all other modules' create endpoints.
- `client-messaging`: message templates and sending client emails (welcome, notifications) from the panel.
- `client-ui`: Client module in the Vue panel (clients, resellers, templates, messages).

### Modified Capabilities

(none — the limit-check hook already exists in `rest-api-core` from the foundation as a vetoable no-op; this module only registers the enforcement implementation into it)

## Impact

- Reference PHP sources: `server/mods-available/client_module.inc.php`, `interface/web/client/` (`client_edit.php`, `client_list.php`, `reseller_*.php`, `client_template_*.php`, `client_message.php`, `message_template_*.php`, `form/`, `list/`), `interface/lib/classes/remote.d/client.inc.php`.
- Tables: `client`, `client_template`, `client_template_assigned`, `client_message_template`, `country`, plus writes to `sys_user`/`sys_group`.
- Other modules' create endpoints already pass through the foundation's limit-check hook — no endpoint changes needed; this module registers the enforcement behind the hook.
- Client message delivery uses an **optional generic SMTP relay** configured in `config.toml`; when no relay is configured, actual sending is disabled (templates can be managed and previewed, nothing is sent) until the mail module or a relay is available.

## Non-goals

- Billing/invoicing (never part of ISPConfig core).
- `client_circle` (interface convenience grouping) — table stays, no logic.
- Domain-module (`interface/web/client/domain_*.php` "domaintool") — separate concern, later.
- Reseller-of-reseller nesting beyond what ISPConfig3 supports (one reseller level).
