# CP Users (System → CP Users)

The panel login accounts in `sys_user`. Port of
`interface/web/admin/form/users.tform.php` plus the rules that live in
`users_edit.php` rather than in the form definition, checked against the PHP
panel on the lab VM `192.168.56.20`.

- UI: `/system/cp-users` (list) → `/system/cp-users/new` | `/system/cp-users/:id`
- API: `/api/cp-users` (standard entity CRUD)
- Admin only, on every route.

## Fields

| Field | Control | Notes |
|---|---|---|
| `username` | text | NOTEMPTY, UNIQUE, `^[\w.\-]{1,64}$` |
| `passwort` | password | bcrypt-hashed on write, minimum 8 characters; **empty on update means unchanged**, and the stored hash never reaches the browser |
| `typ` | select | `user` / `admin` |
| `active` | checkbox | `0` / `1` |
| `modules` | checkbox array | the module ids of this panel, stored as the legacy CSV |
| `language` | select | `en` / `pt-BR` |

`modules` only gates non-admin logins — `AppShell` shows every module to
`typ=admin` — so the practical use is an admin trimming what a client sees.
Ids this panel does not have (`admin`, `vm`, … from an adopted ISPConfig
database) are dropped on write instead of being echoed back verbatim.

## Rules ported from `users_edit.php`

| Rule | Behaviour |
|---|---|
| `onBeforeInsert` "Do not add users here" | a create with `typ=user` is refused (`cpuser_error_no_user_insert`) and every created row is an admin — a client login is created by the Client module, which also builds its `sys_group` and `client` row |
| `admin_allow_new_admin` | creating an admin, or promoting a login to admin, is gated by the security policy (superadmin only by default) |
| client login ↛ admin | a row with `client_id > 0` can never become admin (`cpuser_error_client_not_admin`) |
| standalone login ↛ user | a row with `client_id = 0` can never become a plain user (`cpuser_error_no_user_insert`) |
| rename / password change | propagated to `client.username` / `client.password` and to the client's `sys_group.name`, **inside the update transaction** and journalled |
| `admin_allow_del_cpuser` | deletes are gated by the security policy |
| delete guards | the seeded admin (`userid 1`) and client-owned logins are refused (`cpuser_error_delete_admin`, `cpuser_error_delete_client`) |

Identity columns (`sys_userid`, `sys_groupid`, `sys_perm_user`,
`sys_perm_group`, `groups`, `default_group`) are stamped server-side on
create: an admin login always belongs to the admin group and owns itself,
exactly like the seeded `userid 1`. Leaving them at zero would give the
account no permission scope at all.

## Deliberate omissions

Same rule as [Server Config](server-config-module.md) — a field with no
consumer in this panel is not rendered:

| Legacy field | Why |
|---|---|
| `startmodule` | the SPA always lands on `/dashboard` |
| `app_theme` | the theme is a client-side toggle, not a stored preference |
| `otp_type` | there is no OTP implementation |
| `lost_password_function` | the login page's "password lost" only shows a hint to contact the admin |

`groups` / `default_group` are not editable either: with a single admin group
and one group per client, the only meaningful values are the ones the create
hook and the Client module already stamp. The legacy
`admin_allow_cpuser_group` policy exists for the day that changes.
