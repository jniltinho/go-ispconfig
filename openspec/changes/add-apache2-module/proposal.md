# Proposal: add-apache2-module

## Why

go-ispconfig currently supports exactly one web server: nginx. `internal/nginx/` is a
complete port of `nginx_plugin.inc.php` (vhost rendering, PHP-FPM pools, SSL/Let's
Encrypt, folder auth, site provisioning), and the installer hardcodes nginx —
`cmd/install.go` exposes `--web` as a plain `y/n` toggle and
`internal/installer/distro.go` ships a fixed `Packages` list containing `nginx`.
`getconf.WebConfig` already declares the Apache keys (`server_type`,
`vhost_conf_dir`, `vhost_conf_enabled_dir`, `apache_init_script`,
`php_ini_path_apache`, `check_apache_config`, `htaccess_allow_override`) but no Go
code reads them, and no `apache*.master` template is embedded.

ISPConfig 3's dominant production deployment is Apache, not nginx. Existing
ISPConfig installations being migrated by `add-legacy-migration` are overwhelmingly
Apache-based — the lab VM `legacy-apache` (192.168.56.21, Apache 2.4.58) carries real
`web_domain` rows whose vhosts go-ispconfig currently cannot regenerate. Without an
Apache module, migration produces a database go-ispconfig can read but a web server
it cannot drive.

Apache is not "nginx with different syntax". It brings a per-directory
configuration model (`<Directory>`, `.htaccess`, `AllowOverride`), five PHP
integration modes instead of one, module-guarded configuration (`<IfModule>`), a
suexec identity model, and a config-validation strategy that has no `nginx -t`
equivalent. Those differences are structural and are what this change is mostly about.

Reference PHP sources being ported (read-only under `base/ispconfig3_install/`):
- `server/plugins-available/apache2_plugin.inc.php` (3864 lines) — the core: site
  provisioning, vhost generation, all PHP modes, SSL, folder protection, webdav,
  stats, PHP-FPM pool lifecycle, jailkit chroot, config-check-and-rollback
- `server/conf/vhost.conf.master` — the per-site Apache vhost template
- `server/conf/apache_ispconfig.conf.master` — the server-level include regenerated on
  `server_ip_*` / `server_*` events
- `server/conf/apache_apps.vhost.master` + `apps_php_fpm_pool.conf.master` — the
  ISPConfig apps vhost (port 8081)
- `server/conf/php_fpm_pool.conf.master`, `php-fcgi-starter.master`,
  `php-cgi-starter.master` — the per-site PHP backends
- `server/plugins-available/apps_vhost_plugin.inc.php` — apps vhost, per `server_type`
- `server/plugins-available/webserver_plugin.inc.php` — `php_ini_changed` detection and
  `server_php_*` handling
- `server/plugins-available/website_symlink_plugin.inc.php` — `home/*/website` symlinks
- `server/mods-available/web_module.inc.php` — table hooks → named events (already
  ported for nginx in `internal/web/module.go`)
- `server/lib/classes/letsencrypt.inc.php` — `get_website_certificate_paths()` and the
  cert request flow (shared with nginx)
- `install/lib/installer_base.lib.php::configure_apache()` and
  `install/tpl/apache_acme.conf.master` — the install-time Apache wiring

## What Changes

- **`internal/apache/` plugin package**: sibling of `internal/nginx/`, subscribing to the
  same named events already raised by `internal/web/module.go`
  (`web_domain_*`, `web_folder_*`, `web_folder_user_*`, `server_ip_*`, `client_delete`).
  Port of `apache2_plugin.inc.php`.
- **Web-server selection**: `getconf.WebConfig.ServerType` becomes load-bearing. The
  daemon registers either the nginx plugin or the Apache plugin, never both. New
  `--web-server nginx|apache2` installer flag replacing the implicit nginx choice
  behind the existing `--web` toggle.
- **Apache vhost rendering**: new embedded `vhost.conf.master` driven by the existing
  `internal/mastertpl` renderer, including the `vhosts` loop that emits one
  `<VirtualHost>` per (IP family × SSL × proxy-protocol port) combination, the
  pre-vhost security-deny `<Directory>` block, the paired legacy/real `<Directory>`
  blocks, `ServerAlias` batching, `<IfModule>`-guarded PHP wiring, suexec, error
  documents and rewrite rules.
- **Five PHP integration modes**: `mod`, `fast-cgi` (mod_fcgid + starter script),
  `cgi` (suexec + starter script), `suphp`, `php-fpm` (socket or per-site TCP port),
  plus `no`. nginx only ever needed `php-fpm`. Per-site FPM port allocation
  (`php_fpm_start_port + domain_id - 1`) and starter-script lifecycle are new.
- **`.htaccess` / `.htpasswd` folder protection**: real files written into the
  document root with marker-delimited ISPConfig blocks, replacing the nginx approach of
  emitting auth into the vhost. Plus the stats-folder `.htaccess`/`.htpasswd_stats`
  pair and the `AllowOverride` policy from `web_domain.allow_override` /
  `htaccess_allow_override`.
- **SSL / Let's Encrypt**: reuse the shared cert-path and ACME logic (`-le.*` file
  naming, `ssl_action` state machine) but render Apache's SSL vhost — `<IfModule
  mod_ssl.c>`, `SSLCertificateFile`/`SSLCertificateKeyFile`, OCSP stapling gated on
  `openssl x509 -ocsp_uri`, and the Apache-2.4.8 bundle-file split.
- **Config validation and rollback**: port of the `check_apache_config` path —
  TCP-probe before restart, restart, TCP-probe after, and on failure quarantine the
  vhost as `.err`, restore the `~` backup (plus SSL material) and restart again.
- **Server-level configuration**: `ispconfig.conf` regenerated on `server_ip_*` /
  `server_*` (port of `apache2_plugin::server_ip()`), and `apps.vhost` +
  `000-apps.vhost` symlink (port of `apps_vhost_plugin.inc.php`, Apache branch) — the
  Go side has no apps-vhost implementation at all today.
- **Installer**: `apache2` package set, `a2enmod`/`a2enconf` provisioning, the
  `IncludeOptional sites-enabled/` fix that makes `.vhost` files load, the ACME alias
  conf, and the `/var/log/ispconfig/httpd` log tree.
- **Shared extraction**: the server-flavour-agnostic helpers currently living in
  `package nginx` (`paths.go`, `data.go`, `ensure.go`, `folder.go`, `ssl.go`, `le.go`,
  `seo.go`) move to a shared `internal/websites` package consumed by both plugins.
- **Testing**: golden-file tests pinning rendered Apache vhosts and pool files against
  output captured from the `legacy-apache` lab VM, plus `apache2ctl configtest` in the
  install test rig.

## Capabilities

### New Capabilities

- `apache-vhost`: per-site Apache vhost generation from `vhost.conf.master` — the
  `vhosts` loop, `<Directory>` pairs, aliases/subdomains/`vhostsubdomain`, rewrites and
  redirects, suexec, directive snippets, activation symlinks, and the
  restart-probe-rollback validation cycle.
- `apache-php-fpm`: the PHP integration matrix — `mod`, `fast-cgi`, `cgi`, `suphp`,
  `php-fpm`, `no` — the per-site FPM pool and port/socket allocator, the
  `SetHandler proxy:` wiring under `<IfModule mod_proxy_fcgi.c>`, starter scripts, and
  custom `php.ini` generation.
- `apache-htaccess`: `.htaccess` / `.htpasswd` folder protection, the stats-folder
  auth pair, and the `AllowOverride` policy.
- `apache-ssl`: Apache SSL vhost generation, cert/key/bundle paths, the `ssl_action`
  state machine and Let's Encrypt issuance/renewal on the Apache webroot.

### Modified Capabilities

- `installer-cli`: new `--web-server nginx|apache2` answer, Apache package set,
  `a2enmod` step, sites-enabled include fix, apps vhost and ACME conf provisioning.
- `web-module-events`: the existing web module gains `server_ip_*` and `client_delete`
  subscriptions consumed by the Apache plugin (nginx never needed them).
- `master-templates`: four new embedded templates (`vhost.conf.master`,
  `apache_ispconfig.conf.master`, `apache_apps.vhost.master`,
  `apps_php_fpm_pool.conf.master`) and two starter-script templates.

## Impact

- **Depends on `port-ispconfig3-to-go`** (datalog engine, `.master` renderer, getconf,
  services registry, command runner) and on `add-web-nginx-module` — the shared helper
  extraction refactors `internal/nginx` and must not regress its golden files.
- **New Go packages**: `internal/apache` (plugin), `internal/websites` (shared helpers
  extracted from `internal/nginx`).
- **DB**: no schema changes. Uses the existing `web_domain`, `web_folder`,
  `web_folder_user`, `webdav_user`, `server_ip`, `server_php`, `directive_snippets`
  tables. `web_domain.php` values `mod`/`fast-cgi`/`cgi`/`suphp` become reachable — the
  Sites UI must stop hiding them when `server_type=apache`.
- **getconf**: the dormant Apache keys in `getconf.WebConfig` become live; the seeded
  `internal/database/server_config.ini` gains an Apache `[web]` variant and a
  `[fastcgi]` section (`fastcgi_starter_path`, `fastcgi_starter_script`,
  `fastcgi_bin`, `fastcgi_phpini_path`, `fastcgi_children`, `fastcgi_max_requests`,
  `fastcgi_config_syntax`, `fastcgi_alias`).
- **External services** on an Apache web server: `apache2`, `libapache2-mod-fcgid`,
  `apache2-suexec-pristine`, `php<v>-fpm`, plus the `a2enmod` set.
- **Mutual exclusivity**: nginx and Apache cannot both own port 80 on one server.
  `server_type` is a per-server setting and switching it is an operator action, not a
  runtime toggle.
- **Migration**: `add-legacy-migration` importing an Apache ISPConfig install now has a
  daemon that can regenerate every vhost it imported.

## Non-goals

- **HHVM** (`web_domain.php = 'hhvm'`) — dead upstream; the `hhvm_update()` init-script
  and monit paths are not ported. Existing rows are treated as `no`.
- **suPHP as a supported install target** — the `suphp` branch is rendered for parity
  with imported rows, but the installer never installs `libapache2-mod-suphp` and the UI
  marks the mode deprecated.
- **mod_ruby / mod_perl / mod_python** blocks in the template — rendered verbatim for
  parity, never provisioned or tested.
- **WebDAV** (`webdav_user`, `_patchVhostWebdav`, `.htdigest`) — the vhost keeps the
  `# WEBDAV BEGIN/END` markers so a later change can patch in-place, but no webdav
  handling ships here.
- **Statistics generation** — awstats / GoAccess / Webalizer config files and the
  stats cron. Only the stats-folder `.htaccess`/`.htpasswd_stats` protection is in
  scope (it is an Apache auth concern).
- **Apache chroot** (`website_basedir/etc/passwd` detection, `chroot useradd`) —
  detected and logged, but the chrooted `groupadd`/`useradd`/`add_user_to_group` paths
  are not ported; chrooted Apache installs are out of scope.
- **mod_security, mpm_itk, proxy-protocol tuning** beyond rendering the template blocks.
- **Apache 2.2** — the template's `<tmpl_else>` branches (`Order allow,deny`) are
  ported for template fidelity, but only Apache ≥ 2.4 is supported and tested.
- **Reverse-proxy plugin** (`nginx_reverse_proxy_plugin`) and nginx-in-front-of-Apache
  hybrid setups.
- **Runtime switching** between nginx and Apache on a live server.
- Translations beyond English.
