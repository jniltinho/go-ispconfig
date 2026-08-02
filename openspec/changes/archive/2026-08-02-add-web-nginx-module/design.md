# Design: web/nginx module

## Context

The foundation change (`port-ispconfig3-to-go`) provides: the sys_datalog pipeline (JSON `{old,new}` payloads), typed `Module`/`Plugin` registries, a `Services` registry with delayed restarts, the `.master` template renderer, `getconf`-style server config, riud GORM scopes, the Echo v5 API core with tform-style validators and form metadata export, and the Vue panel skeleton with DataTable/form primitives.

The PHP reference is `nginx_plugin.inc.php` (3627 lines) + `web_module.inc.php` (330 lines) + `letsencrypt.inc.php` (932 lines). The plugin does far more than we port (jailkit, webdav, stats engines, hhvm, symlinks to pma/webmail); this design cuts it to the nginx/PHP-FPM/SSL core while keeping the same event surface and database semantics so the rest can be added later without rework.

## Goals / Non-Goals

**Goals:**
- A datalog change to `web_domain` ends with a live, validated nginx vhost, a PHP-FPM pool, provisioned site directories and system user — or a clean rollback with the error surfaced to the panel (datalog error, `.err` file).
- Reuse `nginx_vhost.conf.master` and `php_fpm_pool.conf.master` nearly verbatim through the foundation renderer; golden-file tests pin the rendered output.
- Same DB rows and field semantics as ISPConfig3 (`web_domain`, `web_folder`, `web_folder_user`, `server_php`) so migrated sites keep working.
- Sites API + UI with the tabs/validators of `web_vhost_domain.tform.php`.

**Non-Goals:**
- Apache, jailkit/shell, FTP, webdav, stats engines, pagespeed, HHVM, APS (see proposal Non-goals).
- Jailkit specifically: this module only persists the jailkit fields of `web_domain` (stored, exposed in API/UI); all jail logic and its ownership belong to `add-ftp-shell-module`.
- Quota enforcement and traffic accounting (phase 2 of this module: fields stored, jobs later).
- Multi-server mirroring.

## Decisions

### D1 — Event surface trimmed but names preserved
`web` module announces the full ISPConfig event list only for the tables we hook now: `web_domain`, `web_folder`, `web_folder_user` (plus `server_php` as a data table without events). Unhooked tables (`ftp_user`, `shell_user`, `webdav_user`, `web_backup`, `aps_*`) are simply not registered — the registry design makes adding them additive later. Event names are identical to PHP (`web_domain_insert`, …) so future plugins port 1:1. The nginx plugin additionally registers a `client_delete` handler (PHP parity: `nginx_plugin` subscribes to `client_delete` to remove all sites owned by the deleted client). The cascade contract is declared in this module; the event itself will be emitted by the future `add-client-module` — until then the handler is registered but never invoked.
*Alternative*: hook all tables and no-op — rejected, dead code.

### D2 — nginx plugin registers two handlers per web_domain event, ordered
Port of the PHP dual registration: `ssl` handler runs before `insert`/`update`/`delete` for the same event (certificates must exist on disk before the vhost referencing them is rendered and nginx reloaded). The foundation registry dispatches handlers for one event in registration order; the plugin relies on that documented guarantee.
*Alternative*: single handler calling ssl internally — considered, but keeping two handlers mirrors the PHP structure and lets Let's Encrypt logic stay isolated.

### D3 — Vhost write pipeline: render → merge → blacklist → write temp → `nginx -t` → activate → reload
1. Build the template vector from `web_domain` + server web config (document_root, http/https ports, redirects, SEO redirects via `get_seo_redirects` port, `rewrite_to_https`, FastCGI socket/port, SSL file paths).
2. Render `nginx_vhost.conf.master`; merge custom `nginx_directives` with the `nginx_merge_locations` port (custom `location` blocks replace/extend template ones; `{FASTCGIPASS}`/snippet placeholders substituted).
3. Reject any custom directive matching a regex line in the embedded `nginx_directives.blacklist` (`load_module` etc.) — rejection is recorded as a datalog error, the vhost is rendered **without** the offending directives (ISPConfig behavior: strip, log, continue). In addition, the sites API validates `nginx_directives` against the same blacklist at save time and rejects the write with a per-field validation error visible to the user; the render-time strip remains as defense in depth (PHP parity for rows that bypass the API, e.g. migrated data).
4. Write to `<vhost>.conf.new`, keep previous file as backup, run `nginx -t`. On success: move into place, ensure symlink in `sites-enabled`, `restartServiceDelayed('httpd','reload')`. On failure: save broken file as `<vhost>.conf.err`, restore previous file, write datalog error with the `nginx -t` output.
Simplification vs PHP: ISPConfig's default path restarts nginx and probes TCP port 80 before/after (`check_apache_config`). We always use `nginx -t` + reload — it validates without a service bounce and is the modern equivalent; the TCP-probe dance is not ported.

### D4 — Filesystem/user provisioning as an idempotent "ensure" step
`insert` and `update` share one `ensureSite()`: create/chown directory tree (`web/`, `log/` bind-target, `ssl/`, `tmp/` 1777, `private/`, `cgi-bin/`), create system user/group (`useradd -r`-equivalent via os/exec, names from `system_user`/`system_group`), handle domain rename (move docroot, rewrite vhost name) and docroot moves. All shell interaction goes through the foundation's command runner (logged, testable with a fake runner). Deletion removes vhost + pool + directories, guarded by ISPConfig's same sanity checks (never `rm -rf` a path shorter than the web root, never outside `website_basedir`).

### D5 — PHP-FPM pools keyed by (server_php, domain)
Port of `php_fpm_pool_update/delete`: pool name `web<domain_id>`, rendered from `php_fpm_pool.conf.master` into the pool dir of the PHP version resolved from `server_php` (falls back to the server default from web config). Vector includes pm mode fields, socket vs TCP (`php_fpm_use_socket`), open_basedir, custom php.ini lines split into the `custom_php_ini_settings` loop. When PHP version or socket mode changes, the old pool file is deleted from the old version's pool dir and that version's FPM service is also reloaded (delayed, `php-fpm:<version>` service key). `php` field values other than `php-fpm` (and `no`) are out of scope — vhost renders without a PHP location.

### D6 — SSL: port `ssl()` + letsencrypt class; acme.sh preferred
- `ssl()` port: `ssl_action` = `create` → openssl CSR + self-signed cert into `<docroot>/ssl/<ssl_domain>.{csr,key,crt}` (0400 key), fields written back to `web_domain` **without** generating a new datalog row (same as PHP: direct update flagged no-datalog); `save` → persist pasted cert/key from the form, rejecting certs containing `.acme.invalid`; `del` → remove files and clear DB fields.
- Let's Encrypt: Go port of `letsencrypt.inc.php` — detect acme.sh, else certbot; the plugin only *detects and uses* an existing client, it never installs one (installation is an optional step of `add-installer-cli`); when no client is found a clear datalog error is recorded; webroot challenge dir served by every vhost (`/.well-known/acme-challenge` alias, port of the acme location in the vhost template); issue with `--always-force-new-domain-key`, ECDSA ec-256 when the client version allows, RSA 4096 fallback; symlink `ssl/<domain>-le.{crt,key}` into the site ssl dir. Failures never break the vhost: on issuance failure the site falls back to previous cert or non-SSL, with a datalog error.
- Renewal: acme.sh/certbot own their renewal state; the daemon scheduler job runs daily, invokes the client's renew command and reloads nginx only when certs changed. No system cron (foundation D1b).

### D7 — Sites API/UI reuse the foundation form framework
`web_vhost_domain` field definitions become a Go form descriptor (fields, tabs Domain/Redirect/SSL/Statistics/Backup/Options, validators REGEX/NOTEMPTY/UNIQUE/CUSTOM ported from the tform arrays, defaults). One descriptor drives: API validation, Swagger models, and the JSON form metadata the Vue form renderer consumes — no hand-written Vue form per tab. Endpoints mirror `remote.d/sites.inc.php` naming (`sites_web_domain_add/get/update/delete`, folder + folder-user equivalents) mapped to REST routes under `/api/sites/...`. Writes go through the foundation datalog writer; reads through the riud scope.

### D8 — Config keys read from server web config (getconf)
The plugin reads the same `server.config` `[web]` keys ISPConfig uses: `website_basedir`, `website_path`, `website_symlinks`, `vhost_conf_dir`, `vhost_conf_enabled_dir`, `nginx_user/group`, `php_fpm_init_script`, `php_fpm_pool_dir`, `php_fpm_socket_dir`, `security_level`, `connect_userid_to_webid`. Defaults for Debian/Ubuntu ship in the seed. Rationale: panel-editable server behavior, unchanged from ISPConfig, and the installer change can fill them per distro.

### D9 — web_folder HTTP basic auth is in scope now
`web_folder`/`web_folder_user` events port `_create_web_folder_auth_configuration`: `.htpasswd`-style auth files maintained per protected folder, and the protected `location` block rendered from the embedded `nginx_http_authentication.auth.master` template and merged into the parent domain's vhost re-render. This is part of this module (the web_folder/web_folder_user CRUD is already in scope), not a phase 2 item.

## Risks / Trade-offs

- [Vhost template edge cases (vhostsubdomain/vhostalias, SEO redirect loops, proxy_protocol)] → golden-file matrix: one fixture per site type × php mode × ssl × redirect variant, outputs pinned; fixtures derived by running the PHP `tpl.inc.php` once on the same vectors.
- [Daemon runs as root and executes useradd/rm/chown] → all destructive paths behind the sanity checks of D4; command runner logs every exec; unit tests with fake runner assert exact argv (no shell string interpolation — `exec.Command` slices only).
- [Custom nginx_directives are user-supplied config injection by design] → blacklist enforced server-side at render (not only at API validation), same regex file as ISPConfig; directives run inside the `server` block only.
- [acme.sh installs via `wget | sh` in ISPConfig] → we port the detection order but installation of acme.sh is delegated to the installer change; the plugin only *uses* an existing client and logs a clear error if none found.
- [Let's Encrypt rate limits during testing] → integration tests use self-signed path + a mocked client binary; real LE only in Vagrant manual validation with staging endpoint.
- [Two FPM reloads (old+new version) on version change may thrash] → delayed service registry dedups per service key; worst case two reloads per run, acceptable.

## Migration Plan

Additive: enable `web` module + `nginx` plugin in `config.toml`. Existing `dbispconfig` rows are picked up as-is; a full re-render of all active vhosts can be forced with the ported "resync" remote action (datalog update touch) — not a new mechanism. Rollback: disable module/plugin in config; vhost files on disk stay untouched.

## Open Questions

- `directive_snippets` table (folder_directive_snippets, snippet placeholders in the merge): port now or stub? Leaning stub — placeholder substitution implemented, snippets UI later.
- `server_ip_*` events re-render all vhosts in PHP; single-server scope may defer this to the resync action.
