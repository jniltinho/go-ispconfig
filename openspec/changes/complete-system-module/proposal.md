# Proposal: complete-system-module

## Why

The System module is the last one with entries the legacy panel has and go-ispconfig does not. The parity sweep behind `server-config-sync` and `cp-users` closed the two biggest ones (Server Config, CP Users) and recorded the rest in `docs/server-config-module.md`. Three of the remaining gaps are not cosmetic — **the data they manage is already read by running code, and there is no way to create or edit it from the panel**:

- **`sys_ini` (Interface Config)** is read on every mail welcome message (`internal/mail/welcome.go`), every database and FTP/shell password validation (`internal/api/sitesdb.go`, `sites_ftp_shell.go` — `[misc] min_password_length`, `min_password_strength`, `ssh_authentication`) and the rspamd level editor (`internal/api/mailrspamd.go`). The row is created by `go-ispconfig migrate` and **can only be changed with SQL**. An operator cannot set their own password policy without a MariaDB client. Port of `interface/web/admin/system_config_edit.php` and `form/system_config.tform.php`.
- **`server_php` (Additional PHP Versions)** is joined on `web_domain.server_php_id` by the jailkit plugin, the cron plugin, the shell plugin and the apache2 vhost renderer — a site can be pinned to a PHP version the panel has no screen to create. Every install therefore has exactly the distro PHP and no way to offer a second one. Port of `interface/web/admin/server_php_list.php` / `server_php_edit.php`.
- **`server` role flags (Server Services)** decide which modules a node runs (`web_server`, `mail_server`, `dns_server`, `db_server`, `firewall_server`, …) and whether it is `active`. `/api/server` exposes the entity, the installer writes the flags once, and after that the only way to turn a role on or off is SQL or a reinstall with `--update`. Port of `interface/web/admin/server_list.php` / `server_edit.php`.

`directive_snippets` is the fourth: the client limit `limit_directive_snippets` already exists in `internal/clients/merge.go`, but **nothing applies a snippet** — neither the nginx nor the apache2 vhost renderer reads the table. Shipping only the CRUD screen would be a knob that does nothing, so this change includes the renderer half.

## What Changes

- **Interface Config** (`System → Interface Config`): an editor over the `sys_ini` INI blob, same section-per-tab shape as Server Config, restricted to the keys this panel actually reads (`[misc] min_password_length`, `min_password_strength`, `ssh_authentication`, `[sites]`, `[mail]`), gated by the existing `admin_allow_system_config` security policy.
- **Server Services** (`System → Server Services`): list and form over the `server` entity — server name, the role flags, mirror target and `active`. Turning a role off SHALL warn when rows depending on it exist (a `mail_server=0` node that still owns mailboxes), and the last active `web_server` cannot be turned off.
- **Additional PHP Versions** (`System → Additional PHP Versions`): CRUD over `server_php` — name, server, client, FastCGI binary/ini dir, FPM init script/ini dir/pool dir/socket dir, CLI binary and jailkit section. The existing `admin_allow_server_php` policy applies. The site form's PHP-version select, today limited to the distro default, gains the rows created here.
- **Directive Snippets** (`System → Directive Snippets`): CRUD over `directive_snippets` (name, type `nginx`/`apache`/`php`/`proxy`, snippet body, `customer_viewable`, `required_php_snippets`, `active`) **plus the renderer half** — the nginx and apache2 vhost templates gain the snippet insertion points, and `web_domain.directive_snippets_id` is honoured. Includes the validation that refuses a snippet whose type does not match the server's `server_type`.
- **Docs**: `docs/system-module.md` collecting the whole System surface, replacing the parity table currently living at the bottom of `docs/server-config-module.md`.

## Capabilities

### New Capabilities

- `interface-config`: the `sys_ini` editor — which keys are exposed, the security policy gate, and the guarantee that a key the panel reads is never silently absent from the form.
- `server-services-ui`: the `server` entity list and form — role flags, mirror target, `active`, and the guards that stop an operator from disabling a role or a server that is still in use.
- `server-php-versions`: `server_php` CRUD and its integration with the site form's PHP-version select and the plugins that already read the table.
- `directive-snippets`: the snippet catalogue plus the nginx/apache2 vhost rendering that applies it, and the per-type validation.

### Modified Capabilities

- `client-limits`: `limit_directive_snippets` gains an effect — it now bounds how many snippets a client may see and attach, instead of being an unused counter.
- `nginx-vhost-generation`: the generated vhost gains snippet insertion points fed by `web_domain.directive_snippets_id`.

## Impact

- **Depends on** `port-ispconfig3-to-go` (entity framework, security policies, getconf, `.master` renderer), `server-config-sync` (the section-per-tab INI editor pattern reused for `sys_ini`) and `cp-users`.
- New `internal/api/systemconfig.go`, `internal/api/serverphp.go`, `internal/api/snippets.go`; Vue views under `frontend/src/views/system/`; nginx/apache2 template changes for the snippet insertion points.
- **DB**: none. `sys_ini`, `server_php`, `server_ip_map` and `directive_snippets` all exist in `internal/database/ispconfig3.sql` and are modeled in `internal/model/`.
- Operationally sensitive: the Server Services form can disable a role and thereby stop a node from applying changes. The guards specified in `server-services-ui` exist precisely because that failure is silent otherwise.

## Non-goals

- **Remote Users** — covered by `add-api-tokens`, which reuses `remote_user`/`remote_session` for API tokens.
- **Server IPv4 mapping** (`server_ip_map`, `interface/web/admin/server_ip_map_*.php`): no consumer anywhere in the Go daemon; it exists for mirrored/NAT topologies this port does not support. It stays an empty table until multi-server lands.
- **Firewall IPTables, Packet Filter, Port Forward** (`iptables_list.php`, `firewall_filter_list.php`, `firewall_forward_list.php`): the firewall module is UFW-only by design; these manage raw iptables chains it does not own.
- **Extension Installer** (`extension_repo_list.php`, `extension_install_list.php`) and **Remote Actions** (`remote_action_osupdate.php`, `remote_action_ispcupdate.php`): there is no ISPConfig extension repository or PHP updater to drive, and running an OS update from the panel is an explicit non-goal of this port.
- **Language editor** (`admin_allow_langedit`): translations live in `frontend/src/locales/*.json`, not in the database.
- Multi-server orchestration — `add-multiserver-mgmt` owns that; Server Services here edits the local node's row and any pre-registered node, nothing more.
- Snippet types beyond `nginx`, `apache`, `php` and `proxy`.
