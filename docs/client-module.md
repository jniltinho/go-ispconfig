# Client module

Port of the ISPConfig3 Client module: clients and resellers with panel
logins, per-service limits, limit templates, email messaging and the
daemon-side `client_*` events. API under `/api/clients`, `/api/resellers`,
`/api/client-templates`, `/api/client-message-templates`; panel UI under
the **Client** top-nav module.

## Client / reseller model

Both roles live in the ISPConfig `client` table; the discriminator is
`limit_client`:

- **Client** — `limit_client = 0`. Managed on `/api/clients`.
- **Reseller** — `limit_client != 0` (`-1` = unlimited sub-clients,
  `> 0` = quota). Managed on the admin-only `/api/resellers`.

The two API surfaces are disjoint: lists are filtered by the
discriminator and the by-id routes return 404 for rows of the other
role. Nesting is one level — a reseller can never have a parent, and a
client's parent (`parent_client_id`) must be a reseller.

Creating a client or reseller provisions its panel login in the same
transaction (PHP `client_edit.php` parity):

- one `sys_group` (name = username, `client_id` set);
- one `sys_user` (bcrypt password, `default_group` = that group,
  modules `dashboard,sites,dns,tools,help`, plus `client` for
  resellers; `locked = y` maps to `sys_user.active = 0`);
- when the client has a parent reseller: the group is appended to the
  reseller's `sys_user.groups` CSV and the client row is re-owned to
  the reseller's user/group, so riud permission scoping gives the
  reseller full visibility.

Updates keep the login in sync (username rename, password rehash +
`last_password_change`, language, locked/active, `limit_client` adds or
removes the `client` module token, parent reassignment moves the group
membership and row ownership). Deletes deprovision everything and
refuse a reseller that still has child clients.

The panel top-nav shows the **Client** module only when the session
user's `sys_user.modules` contains `client` (admins always see it).

## Limit semantics and enforcement

Numeric `limit_*` columns use ISPConfig semantics:

| Value | Meaning |
|-------|---------|
| `-1`  | unlimited |
| `0`   | feature disabled (creates vetoed) |
| `> 0` | veto when the current count reaches the limit |

Enforcement runs in the API create path through the foundation limit
hook: the owning client is resolved from the session's
`sys_user.client_id`, the count is taken over rows owned by the
client's group, and a veto returns **HTTP 403** with an
`error.limit_*` i18n key. Admin sessions bypass counting. Currently
enforced entities: web domains (per type: domain/subdomain/alias), DNS
zones, secondary zones, DNS records, and child clients
(`limit_client`, counted like PHP by rows in the reseller's group).
Unknown entities are never vetoed, so future modules opt in by adding a
rule. Like PHP, the count-then-insert is not atomic under concurrency.

Anti-escalation: on create and on every update, a child's limits are
capped to its parent reseller's (`CapToParent`) — numeric clamp, flags
forced to the parent's `n` (`force_suexec` to `y`), cron type held,
cron frequency floored, CSV lists intersected.

## Limit templates and the merge

`client_template` rows come in two types: **master** (`m`, selected via
`client.template_master`; `0` = custom/no template) and **additional**
(`a`, assigned per client in `client_template_assigned`; the same
template may be assigned multiple times and its limits add up each
time). Assignments live under `/api/clients/{id}/templates`
(get/add/delete) and re-materialize the client limits in the same
transaction, journaled as an `{old,new}` client datalog update.

Merge rules (PHP `client_templates.inc.php` parity):

- numeric limits add; any `-1` promotes to unlimited (a master `-1` is
  frozen and additionals cannot lower it);
- `limit_cron_frequency` merges toward the lowest value, floor 1;
- y/n flags: `y` wins — except `force_suexec`, where `n` wins;
- CHECKBOXARRAY/MULTIPLE columns (php options, ssh chroot, server CSV
  lists) union;
- `limit_cron_type` takes the strongest granted (`full` > `chrooted` >
  `url`);
- default servers are only taken from the master when the client has
  none, zero values are ignored;
- `limit_client` from templates only applies to resellers.

The template catalog (`/api/client-templates` CRUD) is admin-only —
unlike PHP, where resellers manage their own templates; resellers
consume the catalog through `GET /api/clients/template-options` (form
selects) and the assignment endpoints. `template_master` sits on the
form's first tab (PHP shows it on the limits tab). The lookup helpers
(`by-username`, `by-customer-no`, `by-groupid`) serve both roles;
only the CRUD surfaces are role-disjoint.

A legacy `client.template_additional` slash-list (e.g. `2/5/2`) is
migrated to `client_template_assigned` rows on first touch and the
column cleared, exactly like PHP. Editing a template row does **not**
immediately re-materialize every assigned client; limits refresh on the
next client save or assignment change.

## Messaging and SMTP

Email templates (`client_message_template`, types `welcome` / `gdpr` /
`other`) are riud-scoped: resellers manage their own, admins see all.
`POST /api/clients/send-message` ports `client_message.php`: one client
or every non-canceled client in the caller's scope, subject/body typed
or loaded from a template, `{column}` placeholders replaced per
recipient from the client row (`{username}`, `{contact_name}`,
`{company_name}`, `{email}`, …). `{password}` is filled **only** in
the welcome-on-create email while the plaintext is still in-request —
never from the stored hash.

After a client create with a non-empty email, the `welcome` template
belonging to the **creator's** group is rendered and sent once the
transaction has committed; a send failure is logged and never rolls
back the create.

Transport is stdlib SMTP behind config — without `smtp_host` the
send-message endpoint returns 422 `error.smtp_not_configured` and
welcome emails are skipped:

```toml
[mail]
smtp_host = "mail.example.com"  # empty = sending disabled
smtp_port = 25
smtp_user = ""                  # PLAIN auth when set
smtp_pass = ""
from      = "panel@example.com" # sender of all panel emails
```

Divergences from PHP (deliberate): the sender is always `mail.from`
(PHP used the acting reseller's/admin's address), and there is no
`{salutation}` gender mapping.

## Delete and cascade

- `DELETE /api/clients/{id}` (and `/api/resellers/{id}`) removes the
  client row, its `sys_user`/`sys_group`, group memberships and
  template assignments, journaling a `client` delete (the daemon event
  `client_delete` fires on every node — client journal rows broadcast
  with `server_id = 0`). Owned resources are kept. A reseller with
  children is refused (422 `error.client_has_children`).
- `DELETE /api/clients/{id}/everything` (admin only) additionally
  cascades: child clients first (their resources and logins included),
  then every resource owned by the group — web domains, protected
  folders/users, DNS zones/records/secondary zones — one datalog delete
  per row so the daemons tear the real configs down. Mail/ftp/shell/
  db/cron tables follow when their Go modules exist.
- `GET /api/clients/{id}/resource-counts` feeds the panel's delete
  confirmation (counts per resource type + child clients).

## Daemon events

The daemon's client module announces `client_insert`, `client_update`
and `client_delete` and raises them from the `client` table datalog on
every server (broadcast). Plugins subscribe as usual (e.g. a web
plugin tearing down a deleted client's vhosts). Emergency switch:

```toml
[daemon]
disable_client_events = true  # module announces but never raises
```

## Migration notes for existing ISPConfig clients

- Rows imported from a legacy panel (see
  [legacy-migration.md](legacy-migration.md) / [MIGRATION.md](MIGRATION.md))
  work unchanged: the schema is identical and `$1$`/`$6$` crypt hashes
  keep verifying at login; a password set through this module rehashes
  to bcrypt.
- Legacy `template_additional` slash-lists are converted to
  `client_template_assigned` rows the first time the client is saved or
  its templates are touched.
- PHP-era children created by an admin under a reseller may not be
  owned by the reseller's group; child counting and visibility follow
  group ownership (like the PHP client list), so re-save such clients
  (or fix `sys_groupid`) if a reseller must see them.
- `customer_no` is a plain manual field (the PHP auto-template
  `R[CLIENTID]C[CUSTOMER_NO]` counter is not ported).
