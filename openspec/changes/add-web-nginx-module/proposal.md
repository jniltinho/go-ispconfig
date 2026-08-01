# Proposal: add-web-nginx-module

## Why

The go-ispconfig foundation (`port-ispconfig3-to-go`) delivers the sys_datalog engine, module/plugin registries, the `.master` template renderer, riud permissions, the REST API core and the panel skeleton — but no actual hosting module. The web/nginx module is the first real consumer of that architecture: it makes go-ispconfig able to create and serve websites (nginx vhosts, PHP-FPM pools, SSL/Let's Encrypt), which is the primary use case of an ISPConfig server.

## What Changes

Port of the ISPConfig3 web stack to Go, on top of the foundation:

- **web module** (Go port of `base/ispconfig3_install/server/mods-available/web_module.inc.php`): registers table-hooks for `web_domain`, `web_folder`, `web_folder_user` and translates datalog rows into named events (`web_domain_insert/update/delete`, `web_folder_update/delete`, `web_folder_user_insert/update/delete`); registers the `httpd` and `php-fpm` services in the services registry with `nginx -t` guarded restart/reload.
- **nginx plugin** (Go port of `base/ispconfig3_install/server/plugins-available/nginx_plugin.inc.php`, nginx-only paths): site directory skeleton (`web/`, `log/`, `ssl/`, `tmp/`, `private/`, `cgi-bin/`), system user/group management, vhost generation from `nginx_vhost.conf.master` via the foundation's master-templates renderer, custom `nginx_directives` merge with the `security/nginx_directives.blacklist` filter, redirects / SEO redirects / `rewrite_to_https`, `nginx -t` validation before activation with rollback to the previous vhost (`.err` quarantine of the broken file), delayed reload via the services registry.
- **PHP-FPM pool management** (port of `php_fpm_pool_update/delete`): pool files rendered from `php_fpm_pool.conf.master`; `pm` dynamic/static/ondemand; unix socket or TCP listen; multiple PHP versions resolved through the `server_php` table; pool moved/removed when the PHP version or mode changes.
- **SSL**: self-signed CSR/cert generation (port of the `ssl()` handler, including `ssl_action` create/save/del and the `.acme.invalid` rejection) and Let's Encrypt issuance via **acme.sh** with **certbot** fallback (port of `server/lib/classes/letsencrypt.inc.php`); certificate renewal runs as a scheduled job in the daemon's internal scheduler (no system cron).
- **Sites REST API** (port of `interface/lib/classes/remote.d/sites.inc.php`, web subset): CRUD endpoints for web domains, folders and folder users with tform-equivalent validation, riud permission scoping and datalog writes; swaggo annotations on every endpoint.
- **Sites UI** (Vue): Sites panel module — domain list built on the foundation DataTable, and a domain form with the tabs of `interface/web/sites/form/web_vhost_domain.tform.php` (Domain, Redirect, SSL, Statistics, Backup, Options) rendered from server-provided form metadata.
- **Quotas registered, enforced later**: `hd_quota` / `traffic_quota` fields are **stored-only** in this change — persisted and exposed in API/UI, but no `setquota` call and no enforcement of any kind; filesystem quota enforcement (`setquota`) and traffic accounting are explicitly phase 2 of this module.

## Capabilities

### New Capabilities

- `web-module-events`: web module table-hooks and event fan-out for `web_domain`, `web_folder`, `web_folder_user`; `httpd`/`php-fpm` service registration with validated restarts.
- `nginx-vhost`: nginx plugin — site filesystem/user provisioning, vhost rendering from `nginx_vhost.conf.master`, directive merge + blacklist, redirects, `nginx -t` validation with rollback, delayed reload.
- `php-fpm-pools`: PHP-FPM pool file lifecycle from `php_fpm_pool.conf.master`, pm modes, socket/TCP, multi-version via `server_php`.
- `web-ssl`: self-signed certificates and Let's Encrypt (acme.sh preferred, certbot fallback) with scheduler-driven renewal.
- `sites-api`: REST API for web domains/folders/folder users with validation, riud scoping, datalog integration and Swagger docs.
- `sites-ui`: Vue Sites module — domain DataTable list and tabbed metadata-driven form.

### Modified Capabilities

(none — the foundation capabilities are consumed, not changed: this module registers into the existing registries and uses the existing renderer, permission scopes and API framework.)

## Impact

- New Go packages under `internal/` (module `web`, plugin `nginx`, `letsencrypt`, sites API handlers) wired into the foundation's module/plugin registries via `config.toml`.
- Templates copied from `base/ispconfig3_install/server/conf/`: `nginx_vhost.conf.master`, `php_fpm_pool.conf.master`, `nginx_http_authentication.auth.master` (web_folder HTTP basic auth); blacklist from `security/nginx_directives.blacklist` — embedded in the binary.
- Database: uses existing ISPConfig3 tables only (`web_domain`, `web_folder`, `web_folder_user`, `server_php`, `server`, `server_ip`) — no schema changes; GORM models added for tables not yet mapped.
- Frontend: new `sites` module in the Vue SPA (routes, Pinia store, list + form views).
- Daemon: new scheduled job (Let's Encrypt renewal) registered in the internal scheduler.
- Reference PHP sources (read-only): `server/plugins-available/nginx_plugin.inc.php`, `server/mods-available/web_module.inc.php`, `server/lib/classes/letsencrypt.inc.php`, `server/conf/nginx_vhost.conf.master`, `server/conf/php_fpm_pool.conf.master`, `interface/web/sites/form/web_vhost_domain.tform.php`, `interface/lib/classes/remote.d/sites.inc.php`.
- Testing: unit tests (testify), golden-file tests for rendered vhost/pool files, integration tests against MariaDB; agent-browser E2E for the Sites UI.

## Non-goals

- Apache2 support (nginx only).
- Shell users / jailkit chroot, FTP users, WebDAV, backups execution (`web_backup` events ignored for now). Jailkit: this module only persists the jailkit fields on `web_domain`; all jailkit logic and ownership belong to `add-ftp-shell-module`.
- Statistics engines (awstats/webalizer/goaccess) and pagespeed — later change; the Statistics tab stores settings only.
- HHVM (dead upstream), APS installer, `proxy_directives` reverse-proxy vhosts beyond what the vhost template already covers.
- Disk quota enforcement and traffic accounting jobs (fields stored now; enforcement is phase 2 of this module).
- Multi-server mirroring; `server_ip` change re-rendering of all vhosts limited to single-server semantics.
