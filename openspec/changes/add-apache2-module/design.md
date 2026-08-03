# Design: Apache 2 web module

## Context

go-ispconfig has one web-server implementation. `internal/web/` is the module (datalog
table hooks → named events, `httpd` service registration); `internal/nginx/` is the
plugin that turns those events into filesystem state. `internal/web/services.go`
hardcodes `nginxUnit = "nginx"` and wraps the executor in a `GuardedExecutor` that runs
`nginx -t` before every restart. The installer (`cmd/install.go`,
`internal/installer/`) installs a fixed package list containing `nginx` and runs a
single `nginxBaseStep`. `getconf.WebConfig` already parses the Apache keys — nothing
reads them.

ISPConfig 3's Apache implementation is `apache2_plugin.inc.php`, a 3864-line class
subscribing to eleven event families. Structurally it does the same job as
`nginx_plugin.inc.php` — provision the site tree, render one config file, symlink it
into an enabled dir, reload the daemon — but the config it renders is a different kind
of artefact:

1. **nginx has no per-directory configuration.** Apache's `<Directory>` /
   `<FilesMatch>` / `<Files>` / `<If>` blocks, and `.htaccess` with its `AllowOverride`
   contract, are the model. Half of `vhost.conf.master` is `<Directory>` blocks.
2. **nginx has one PHP backend.** Apache has five (`mod`, `fast-cgi`, `cgi`, `suphp`,
   `php-fpm`) plus `no`, and `vhost.conf.master` emits php-fpm wiring **three times
   over**, each wrapped in a different `<IfModule>`, so one rendered file works on a host
   with `mod_fastcgi`, `mod_proxy_fcgi`, or neither.
3. **nginx has one server block per listener.** Apache's template drives a `vhosts`
   loop that can emit up to eight `<VirtualHost>` blocks in one file — IPv4/IPv6 ×
   HTTP/HTTPS × normal/proxy-protocol port.
4. **nginx validates with `nginx -t`.** Apache's plugin has no equivalent in the hot
   path: `check_apache_config=y` means "TCP-probe :80, restart, TCP-probe again, and if
   it was up before and is down now, roll the file back". That is a fundamentally
   riskier operation and needs a deliberate design decision.
5. **Apache is version-sensitive.** The template branches on `apache_version` and
   `apache_full_version` at 2.2, 2.4, 2.4.8, 2.4.11, 2.4.26 and 2.4.30 boundaries
   (`Require all granted` vs `Order allow,deny`, separate bundle file, OCSP stapling,
   `ProxyFCGISetEnvIf`, `RemoteIPProxyProtocol`).

Evidence for this design was taken from three sources: the PHP plugin and templates,
and the live `legacy-apache` lab VM (Apache 2.4.58, four real ISPConfig vhosts,
`/etc/php/8.3/fpm/pool.d/web{1,2,3}.conf`). Where they disagree, the rendered VM output
wins — it is what Apache actually consumes.

## Goals / Non-Goals

**Goals:**
- Behaviour-faithful port of `apache2_plugin.inc.php` for `web_domain`, `web_folder`,
  `web_folder_user`, `server_ip` and `client_delete` events, plus `apps_vhost_plugin`
  (Apache branch) and the `php_ini_changed` action.
- Byte-level golden-file parity between the Go renderer and the PHP renderer for the
  same fixture rows, verified against vhosts captured from `legacy-apache`.
- A single `--web-server nginx|apache2` installer decision that provisions a complete,
  working Apache host: packages, modules, include wiring, log tree, ACME alias, apps
  vhost, default vhost handling.
- All five PHP modes render correctly; `php-fpm` (socket and per-site TCP) is the
  tested, supported, default mode.
- Shared logic (site tree provisioning, symlinks, SSL/ACME, folder auth, SEO
  redirects, path guards) extracted once and consumed by both plugins.

**Non-Goals:**
- HHVM, WebDAV, statistics generation, Apache chroot, mod_security/mpm_itk tuning,
  Apache 2.2 as a supported target, nginx↔Apache runtime switching (see proposal
  Non-goals).
- Rewriting the vhost template into idiomatic modern Apache. The template is ported
  as-is; improving it is a separate, reviewable change.
- Schema changes of any kind.

## Decisions

### D1 — `internal/apache` as a sibling plugin, `internal/websites` as shared base

`internal/nginx` is a flat package where roughly half the files are already
server-flavour agnostic. Rather than parameterise the nginx plugin, extract the shared
half and add a sibling:

| New home | Moved from | Contents |
|---|---|---|
| `internal/websites` | `nginx/paths.go`, `data.go`, `ensure.go`, `folder.go`, `ssl.go`, `le.go`, `le_renew.go`, `seo.go`, `delete.go` (partial) | site tree provisioning, user/group creation, docroot moves, symlinks, path guards, `row` helper, self-signed + Let's Encrypt lifecycle, SEO redirect vectors |
| `internal/nginx` | — | nginx-only: `vhost.go`, `render.go`, `merge.go`, `blacklist.go`, `activate.go`, `pool.go`, `rewrite_rules.go` |
| `internal/apache` | new | `plugin.go`, `vhost.go`, `render.go`, `activate.go`, `pool.go`, `starter.go`, `phpini.go`, `htaccess.go`, `ispconfig_conf.go`, `apps.go` |

Both plugins implement the same handler signatures and subscribe to the same events.
`cmd/daemon.go` registers exactly one of them based on
`getconf.WebConfig.ServerType`.

*Alternative*: one `internal/web` plugin with a `ServerType` switch inside every
handler — rejected: the two renderers share almost no template logic, and the branchy
version would make the existing nginx golden files harder to defend.

*Constraint*: the extraction must be a pure move. `internal/nginx`'s 14 golden files
are the regression gate — if any of them changes, the extraction is wrong.

### D2 — `install --web-server apache2` vs the nginx path

This is the largest install-time divergence. Concretely:

| Concern | nginx today | apache2 |
|---|---|---|
| **Flag** | `--web y/n` (`cmd/install.go:115`), nginx implied | `--web-server nginx\|apache2` (default `nginx`); `--web n` still skips the whole step |
| **Packages** (`installer.Profile.Packages`) | `nginx` | `apache2`, `libapache2-mod-fcgid`, `apache2-suexec-pristine` (+ `libapache2-mod-php<v>` only when `mod` PHP is requested) |
| **Service unit** | `nginx` | `apache2` |
| **Config test** | `nginx -t` (in `web.GuardedExecutor`) | `apache2ctl configtest` |
| **Site dir** | `/etc/nginx/sites-available` → `sites-enabled` | `/etc/apache2/sites-available` → `sites-enabled` |
| **Include wiring** | write `/etc/nginx/conf.d/go-ispconfig-sites.conf` if `nginx.conf` lacks `include sites-enabled` | **rewrite** `apache2.conf`'s `IncludeOptional sites-enabled/*.conf` → `IncludeOptional sites-enabled/` (drop the suffix) — otherwise `.vhost` files are never read |
| **Enabled naming** | `<domain>.vhost`, `100-`/`900-` prefix symlinks | identical: `<domain>.vhost` in sites-available, `100-<domain>.vhost` (or `900-` for wildcard) symlinks. Note both use `.vhost`, **not** `.conf` |
| **Server-level conf** | none | `sites-available/ispconfig.conf`, regenerated on `server_ip_*` (D9) |
| **Default vhost** | none (panel terminates its own TLS) | Debian's `000-default.conf` is left enabled but its `NameVirtualHost *` / `<VirtualHost *>` lines are normalised to `*:80`, and `ports.conf` gets `Listen 443` plus its `NameVirtualHost` lines commented out |
| **Apps vhost** | not implemented | `sites-available/apps.vhost` + `sites-enabled/000-apps.vhost`, port 8081 (D10) |
| **ACME** | webroot dir only (`/usr/local/ispconfig/interface/acme`) | same webroot **plus** `conf-available/999-acme.conf` (`Alias /.well-known/acme-challenge …` + `Require all granted`) and `a2enconf 999-acme` |
| **Modules** | n/a — nginx has no module system | `a2enmod` step (D3) |
| **PHP-FPM pool dir** | `/etc/php/<v>/fpm/pool.d` | same directory, but pools are consumed via `SetHandler proxy:` in the vhost rather than `fastcgi_pass`; TCP pools additionally need `listen.allowed_clients = 127.0.0.1` |
| **Log tree** | `/var/log/ispconfig/httpd` (dir only) | same, plus per-domain subdirs bind-mounted into `<docroot>/log` with `/etc/fstab` entries |
| **fcgid tuning** | n/a | `/etc/apache2/mods-available/fcgid.conf` → `MaxRequestLen 15728640` |

Implementation: `installer.Profile` gains `WebServer`, `ApachePackages`,
`ApacheConfigDir`, `ApacheVhostConfDir`, `ApacheVhostEnabledDir`, `ApacheService`,
`ApacheUser/Group`; `nginxBaseStep` is joined by an `apacheBaseStep` and the pipeline
selects one. `packagesStep` picks the package set from the answer.
Port of `installer_base.lib.php::configure_apache()`.

### D3 — Explicit `a2enmod` set, no auto-detection

PHP never enables Apache modules from the plugin — the only `a2enmod` calls in the
entire tree are `proxy` and `proxy_http` in `apps_vhost_plugin.inc.php` when Rspamd is
in use. Everything else is left to the operator following the perfect-server guide.
That is not acceptable for a one-command installer, and the rendered vhost is
`<IfModule>`-guarded, meaning a missing module fails **silently** (PHP served as plain
text, or a 403) rather than failing the config test.

`apacheBaseStep` therefore runs an explicit `a2enmod` list and fails the install if any
of them fails:

| Module | Needed for |
|---|---|
| `rewrite` | every redirect, SEO redirect, `rewrite_to_https`, ACME `[END]` guard |
| `ssl` | the entire `<IfModule mod_ssl.c>` SSL vhost |
| `suexec` | `SuexecUserGroup` — without it, `cgi`/`fast-cgi` run as `www-data` |
| `actions` | `Action php-cgi` / `Action php-fcgi` |
| `proxy`, `proxy_fcgi` | `SetHandler "proxy:unix:…\|fcgi://localhost"` — the php-fpm path |
| `fcgid` | `fast-cgi` mode (`SetHandler fcgid-script` + `FCGIWrapper`) |
| `headers` | stats `.htaccess` CSP, apps-vhost security headers |
| `auth_basic`, `authn_file`, `authz_user` | `.htaccess` folder protection (D11) |
| `alias`, `dir`, `mime`, `env`, `setenvif`, `filter`, `deflate` | template baseline (`Alias /error/`, `DirectoryIndex`, `SetEnv TMP`) |
| `http2`, `socache_shmcb` | `Protocols h2 http/1.1`, `SSLStaplingCache` |
| `expires`, `include` | directive snippets, SSI (`Options +Includes`) |
| `proxy_http` | apps vhost Rspamd passthrough, only when `content_filter=rspamd` |

The list is verified against `legacy-apache`'s `mods-enabled/`, which carries exactly
this set plus `dav`/`dav_fs`/`auth_digest` (WebDAV, out of scope) and
`passenger`/`python` (unrelated).

`a2enmod ssl` is idempotent; the step is safe on `--update`. `apache2ctl configtest`
runs after the module set and before the first reload.

*Alternative*: probe `apache2ctl -M` and only enable what is missing — rejected,
`a2enmod` is already idempotent and the probe is the more code.

### D4 — Reproduce the double `<Directory>` emission verbatim

`vhost.conf.master` emits every `<Directory>` body **twice**: once for
`web_document_root_www` (`{website_basedir}/{domain}/web`, e.g.
`/var/www/wp1a.goisp.test/web`) and once for `web_document_root`
(`{document_root}/{web_folder}`, e.g. `/var/www/clients/client1/web1/web`). The
rendered VM vhost confirms it: identical `SetHandler None` / `Options
+SymlinksIfOwnerMatch` / `AllowOverride All` / `Require all granted` bodies, and again
for both `mod_fastcgi` and `mod_proxy_fcgi` PHP blocks — four `<Directory>` pairs in one
80-line vhost.

**Decision: reproduce it, unconditionally.** Reasons, in order:

1. `{website_basedir}/{domain}` is a real path — it is the symlink created from the
   `website_symlinks` getconf pattern (`/var/www/[website_domain]/`). Requests can and
   do resolve through it, and `web_document_root_www` is the `DocumentRoot` for
   `php = mod`, `no`, and `fast-cgi`. Emitting only the client path would leave those
   modes with **no** `<Directory>` grant at all — with `apache_ispconfig.conf`'s
   `<Directory /> Require all denied` in force, that is an instant 403 for every
   mod_php and static site.
2. Dropping the pair would break golden-file parity, which is the whole verification
   strategy for this change.
3. The dedup is not free: it requires resolving both paths, proving they are the same
   inode, and proving the symlink will still exist at Apache reload time. That is more
   code, more failure modes, and a real security downside (a suppressed `<Directory>`
   silently downgrades to whatever a parent block says).

The duplication is recorded as known debt with a named ceiling: it roughly doubles
vhost file size and Apache's per-request directory-walk cost. Upgrade path, if it ever
matters: emit the legacy block only when `website_symlinks` actually contains a
`[website_domain]` pattern — a config-driven skip, not a filesystem probe.

### D5 — PHP mode matrix

`web_domain.php` selects one of six behaviours. Only `php-fpm` overlaps with nginx.

| Mode | `DocumentRoot` | What the plugin writes | Vhost emits |
|---|---|---|---|
| `no` | `web_document_root_www` | nothing | `<Files ~ '.php[s3-6]{0,1}$'> Require all denied` in both `<Directory>` blocks |
| `mod` | `web_document_root_www` | custom `php.ini` under `{website_basedir}/conf/{system_user}` only | `AddType application/x-httpd-php`, `SetEnv TMP/TMPDIR/TEMP`, `php_admin_value sendmail_path` (unless the custom ini already sets it), `upload_tmp_dir`, `session.save_path`, and `open_basedir` **only at `security_level=20`** |
| `suphp` | `web_document_root` | custom `php.ini`; `open_basedir` is appended to the custom ini (suPHP is the only mode that needs it there) | `<IfModule mod_suphp.c>` + `suPHP_ConfigPath` |
| `cgi` | `web_document_root` | `php-cgi-starter` script from `php-cgi-starter.master` into `{website_basedir}/php-cgi-scripts/{system_user}/`, chowned to the site user, chmod 0755 (`security_level=10`) or 0550, then made **immutable** | `ScriptAlias /php-cgi`, `Action php-cgi`, `SetHandler php-cgi` in both `<Directory>` blocks |
| `fast-cgi` | `web_document_root_www` | `php-fcgi-starter` script from `php-fcgi-starter.master` into `fastcgi_starter_path` (same chown/chmod/immutable treatment) | `<IfModule mod_fcgid.c>` tuning block (syntax v1 or v2 per `fastcgi_config_syntax`), `SetHandler fcgid-script` + four `FCGIWrapper` lines per `<Directory>` |
| `php-fpm` | `web_document_root` | pool file (D6) | three `<IfModule>`-guarded variants (D7) |

Mode transitions must clean up the previous mode's artefacts: leaving `fast-cgi`
removes the starter script (and the whole starter dir for `type=vhost`); leaving
`php-fpm` removes the pool from **every** PHP version's pool dir; leaving `cgi` removes
the cgi starter. Starter scripts are `chattr +i` in PHP — the Go port clears the
immutable flag before rewriting and re-sets it after, via the command runner.

Vhostsubdomain/vhostalias sites get a `_web<domain_id>` suffix on starter script names
and a `_<web_folder>` suffix on the custom-php.ini directory, so several sites under one
system user do not collide.

Port of `apache2_plugin.inc.php::update()` lines ~1550–1790 and
`get_master_php_ini_content()`.

### D6 — PHP-FPM pool: same template, different consumer, plus a port allocator

The pool file itself is the existing `php_fpm_pool.conf.master` already embedded for
nginx, and `internal/nginx/pool.go`'s logic ports over almost unchanged: pool name
`web<domain_id>`, `pm`/`pm.*` from the row, `php_admin_value`/`php_admin_flag` derived
from `custom_php_ini` + directive-snippet PHP snippets, chroot handling, and pruning the
pool from every other version's `php_fpm_pool_dir`.

Two things are Apache-specific:

1. **Per-site TCP port allocation.** `fpm_port = php_fpm_start_port + domain_id - 1`.
   The lab VM's `web1` pool listens on `127.0.0.1:9010` with
   `listen.allowed_clients = 127.0.0.1`, and the vhost references `9010` in three
   places. nginx never needed this because `internal/nginx` defaults to sockets. The
   allocator is deterministic (no state, no registry) and must produce the identical
   number on both sides of the pool/vhost pair — it lives in `internal/websites` and is
   called once, its result threaded into both renderers.
2. **Socket-vs-TCP is a real fork in the vhost**, not just the pool. `use_socket`
   yields `SetHandler "proxy:unix:<sock>|fcgi://localhost"`; `use_tcp` yields
   `SetHandler "proxy:fcgi://127.0.0.1:<port>"`. Sockets are the default and the
   recommendation; TCP exists for chrooted pools and multi-host setups.

Reload semantics follow the existing `reloadAction(cfg)` / `scheduleFPM(unit, action)`
helpers driven by `php_fpm_reload_mode`. PHP's `sleep(1)` between reloading other
versions and the current one is dropped — the services registry already batches and
deduplicates per unit at end-of-cycle.

Port of `apache2_plugin.inc.php::php_fpm_pool_update()` / `php_fpm_pool_delete()`.

### D7 — Triple-emitted, `<IfModule>`-guarded PHP wiring, and the `<If "-f …">` guard

For `php = php-fpm` the template emits the same intent three times:

```
<IfModule mod_fastcgi.c>      → Action php-fcgi /php-fcgi virtual
                                Alias /php-fcgi <docroot>/cgi-bin/php-fcgi-<ip>-<port>-<domain>
                                FastCgiExternalServer … -host 127.0.0.1:9010   (or -socket <sock>)
<IfModule mod_proxy_fcgi.c>   → SetHandler "proxy:fcgi://127.0.0.1:9010"       (use_tcp)
                              → SetHandler "proxy:unix:<sock>|fcgi://localhost" (use_socket)
```

This is deliberate: one rendered file must work on a host with the legacy
`mod_fastcgi`, on a modern host with `mod_proxy_fcgi`, and on a host with both (where
`mod_fastcgi` wins because its `Action` handler is set first). The Go renderer
reproduces all three branches. The installer only ever enables `proxy_fcgi`, so on a
go-ispconfig-provisioned host the `mod_fastcgi` block is inert — but it is what makes
migrated hosts keep working before the operator touches anything.

**The `<If "-f '%{REQUEST_FILENAME}'">` guard is security-relevant, not cosmetic.**
Every `SetHandler` for php-fpm sits inside `<FilesMatch "\.php[345]?$">` *and* that
`<If>`. Without it, Apache hands **any** URL ending in `.php` to the FPM pool,
including paths that do not exist on disk. That enables the classic
`/uploads/evil.jpg/x.php` and `/nonexistent.php/../../etc/passwd` style path-info
attacks, where PHP's `cgi.fix_pathinfo` resolves the request to a different, attacker-
influenced file. The `-f` test makes Apache confirm the resolved filename is a real
regular file before delegating. The Go renderer MUST emit this guard on every
php-fpm `SetHandler`, and a golden-file test asserts its presence.

`php_fpm_chroot=y` additionally emits `ProxyFCGISetEnvIf` rewrites for `DOCUMENT_ROOT`,
`CONTEXT_DOCUMENT_ROOT`, `HOME` and `SCRIPT_FILENAME` inside `<IfVersion >= 2.4.26>`,
because inside the jail the pool sees a different root than Apache does.

### D8 — Config validation: keep `check_apache_config`, but make the safe path the default

nginx gets a cheap, total, side-effect-free validation: `nginx -t`. Apache has no
equivalent that catches everything `apache2ctl configtest` misses (a syntactically
valid config can still fail to bind or fail an `<IfModule>` assumption), which is why
PHP resorts to a restart-and-probe cycle:

1. TCP-probe `localhost:80` → `up_before`
2. `restartService('httpd', 'restart')` → `retval`
3. sleep 2, then TCP-probe up to 5 times → `up_after`
4. if (`up_before && !up_after`) or `retval > 0`: copy the vhost to `<file>.err`,
   restore `<file>~` (or write a warning stub if there is no backup), restore
   `~`-backed SSL key/crt/csr/bundle if the cert changed, log the `apache2ctl -t`
   output into `sys_datalog` errors, and restart again.

The Go port keeps this, with one change: it runs `apache2ctl configtest` **first** and
short-circuits on failure, so the common case (a typo in a directive snippet) never
takes the site down. Only if configtest passes does the restart-and-probe cycle run.
`check_apache_config=n` degrades to a plain delayed reload, exactly as PHP.

`web.GuardedExecutor` is generalised: it takes the test command as a field
(`nginx -t` or `apache2ctl configtest`) instead of hardcoding nginx.

*Alternative*: rely on configtest alone and drop the probe — rejected: configtest does
not catch bind failures (duplicate `Listen`, a port taken by the panel), and the probe
is what turns "site down until an operator notices" into "site down for two seconds".

### D9 — `ispconfig.conf`: a server-level include with no nginx counterpart

On `server_ip_insert|update|delete` and `server_insert|update`, the Apache plugin
regenerates `{vhost_conf_dir}/ispconfig.conf` from `apache_ispconfig.conf.master`. It
carries: `ServerTokens`/`ServerSignature`/`DirectoryIndex`, the vlogger `CustomLog`
pipeline (`logging = yes|anon|no`), `Require all denied` on `/`, `/var/www/clients`,
`/var/www/conf`, `/var/www/php-cgi-scripts`, `/var/www/php-fcgi-scripts`, allow-grants
for phpMyAdmin/awstats/squirrelmail, and (Apache < 2.4 only) `NameVirtualHost` lines
per `server_ip` row where `virtualhost='y'`.

nginx has nothing like this — its equivalent security posture is per-`server{}`. The
`Require all denied` on `/var/www/clients` is precisely why D4's double `<Directory>`
emission cannot be dropped.

This file is written to `sites-available/ispconfig.conf` and is expected to be enabled
as `sites-enabled/000-ispconfig.conf` by the installer.

### D10 — Apps vhost (port 8081), Apache branch only

`apps_vhost_plugin.inc.php` renders `apache_apps.vhost.master` into
`{vhost_conf_dir}/apps.vhost` and symlinks `sites-enabled/000-apps.vhost` when
`apps_vhost_enabled = y`. Notable details the Go port must keep:

- `Listen {apps_vhost_port}` is emitted, but commented out when the port is 80 or 443.
- The template mixes `{tmpl_var}` syntax with legacy `{brace}` placeholders that PHP
  string-replaces *after* rendering. `internal/mastertpl` handles the former; the
  latter needs an explicit post-render replacement pass for
  `apps_vhost_ip/port/dir/servername/basedir` and `vhost_port_listen`.
- SSL directives are enabled by substituting an `ssl_comment` var with `''` or `'#'`
  depending on whether the panel's `ispserver.crt`/`.key`/`.bundle` exist — a
  comment-toggle rather than a conditional block.
- Rspamd passthrough (`RewriteRule ^/rspamd/(.*) http://127.0.0.1:11334/$1 [P]`) is
  gated on `mail.content_filter = rspamd` and triggers `a2enmod proxy proxy_http`.
- The apps FPM pool comes from `apps_php_fpm_pool.conf.master` (pure `{brace}`
  placeholders, no tmpl syntax) written as `{php_fpm_pool_dir}/apps.conf` with user and
  group `ispapps`.
- PHP wiring here is `<IfModule mod_php5.c>` / `<IfModule mod_php7.c>` /
  `<IfModule mod_fcgid.c>` — a third variation on the same triple-emission pattern.

Registered on `server_insert` / `server_update`, and requests a **restart** (not a
reload) because `Listen` may have changed.

### D11 — `.htaccess` and the `AllowOverride` contract

This capability simply does not exist for nginx: `internal/nginx/folder.go` maintains
`.htpasswd` files but injects the `auth_basic` directives into the vhost. Apache reads
`.htaccess` from the filesystem, so the plugin writes both files into the document root:

- **Path**: `{document_root}/{web_folder}/{web_folder.path}/`, with `web_folder = 'web'`
  for `type=vhost` and `web_domain.web_folder` for `vhostsubdomain`/`vhostalias`.
  Rejected if the path contains `..`, `./` or `\`, or resolves outside `document_root`.
- **`.htpasswd`**: 0751, owned `system_user:system_group`, one `username:crypt` line per
  active `web_folder_user` row. Inactive or renamed users have their line removed.
- **`.htaccess`**: an ISPConfig-owned block delimited by
  `### ISPConfig folder protection begin ###` and
  `### ISPConfig folder protection end ###\n\n`, containing `AuthType Basic`,
  `AuthName "Members Only"`, `AuthUserFile <abs path>`, `require valid-user`. If the
  file already exists, the marked block is replaced in place; user content outside the
  markers is preserved. On removal, only the marked block is stripped, and the file is
  deleted only if what remains is blank.
- **Folder rename** (`web_folder_update` with a changed `path`): move `.htpasswd`, strip
  the block from the old `.htaccess`, write it into the new one.
- **Stats folder**: a separate, unmarked `.htaccess` (0640) plus `.htpasswd_stats`
  containing a single `admin:<stats_password>` line, rewritten whenever
  `stats_password` changes.

**`AllowOverride` policy.** The vhost's `<Directory>` blocks use
`web_domain.allow_override`, defaulting to `All` when the column is empty; the getconf
key `htaccess_allow_override` (default `All`) is the panel-side default offered when a
site is created. Two blocks in the same rendered file deliberately do **not** honour it:
the pre-vhost `<Directory {web_basedir}/{domain}>` and `ispconfig.conf`'s `<Directory
/>` both pin `AllowOverride None` + `Require all denied`. That ordering — deny at the
domain root, grant inside `web/` — is what keeps `.htaccess` from being read outside the
document root. A site set to `AllowOverride None` gets its `.htaccess` written anyway
(so switching the setting back works without a resync) but Apache ignores it, and the
panel must warn.

Port of `apache2_plugin.inc.php::web_folder_user()`, `web_folder_update()`,
`web_folder_delete()` and the stats block in `update()`.

### D12 — Aliases, subdomains, and the `vhostsubdomain` / `vhostalias` types

`web_domain.type` has five values and the plugin treats them in two groups:

- **`vhost`, `vhostsubdomain`, `vhostalias`** get their own vhost file, own docroot
  tree, own PHP backend, own SSL. `vhostsubdomain`/`vhostalias` additionally: derive
  `web_folder` from the row (not `'web'`), derive the log subfolder from the hostname
  part (`preg_replace` of the parent domain off the front, falling back to
  `web<domain_id>`), reject blacklisted web folders, and suffix their custom-php.ini dir
  and starter scripts with the folder / domain id.
- **`alias`, `subdomain`** have **no** vhost of their own. They are folded into the
  parent's `ServerAlias` list. Any insert/update/delete on such a row causes the plugin
  to reload the **parent** `web_domain` row and re-run `update()` against it, with
  `update_letsencrypt = true` so the parent's certificate picks up the new name. If the
  row's `parent_domain_id` itself changed, the *old* parent is re-rendered first.

`ServerAlias` construction: the site's own `subdomain` setting contributes
`www.<domain>` or `*.<domain>`; `website_autoalias` (with `[client_id]`,
`[website_id]`, `[client_username]`, `[website_domain]` placeholders) contributes one
more; each active child alias/subdomain contributes `www.<d> <d>`, `*.<d> <d>` or `<d>`.
The resulting list is emitted with a **new `ServerAlias` line every 30 names** — Apache
has no hard limit but the line length becomes unmanageable.

Each alias can also carry its own `redirect_type`/`redirect_path` and `seo_redirect`,
which are merged into the parent's rewrite-rule loop (aliases prefixed `alias_`).
Wildcard rules (`subdomain = '*'`) are appended **after** all non-wildcard rules so the
specific match wins.

### D13 — Rewrites, redirects, and their interaction with suexec and php-fpm

Redirects come from `redirect_type` (`no`, `R`, `R=301`, `R=302`, `L`, …) plus
`redirect_path`. Rules:

- A `redirect_path` that is not a URL and does not end in `/` gets a `/` appended.
- A `[scheme]` prefix expands to `http` in the plain vhost and `https` in the SSL vhost
  — the same rule renders differently per `vhosts` loop iteration.
- `redirect_type = 'no'` means "rewrite without a flag" (internal rewrite), everything
  else becomes `[<type>]`.
- Non-URL targets get three exclusion conditions: `!^/webdav/`, `!^/php-fcgi/`, and
  `!^<target>` — the second is what stops a redirect from swallowing the `Action
  php-fcgi` alias and breaking PHP entirely under `mod_fastcgi`.
- Domain patterns are regex-quoted (`.`, `*`, `?`, `+` escaped); wildcard domains use
  `(^|\.)` instead of `^`.
- Apache ≥ 2.4 emits `RewriteCond %{REQUEST_URI} ^/\.well-known/acme-challenge/` +
  `RewriteRule ^ - [END]` at the top of the rewrite block; Apache < 2.4 has no `[END]`
  and instead adds a negative `RewriteCond` to every individual rule. ACME challenges
  must survive every redirect or renewal breaks.
- `rewrite_to_https` is emitted **only in the non-SSL vhost**, and `apache_directives`
  (the directive snippet) is **skipped** in the non-SSL vhost when `rewrite_to_https=y`,
  because the plain vhost is a pure redirect shell.

**Interaction with suexec**: `SuexecUserGroup <system_user> <system_group>` is emitted
once per `<VirtualHost>` inside `<IfModule mod_suexec.c>`. It governs `cgi` and
`fast-cgi` only — it is what makes the starter scripts run as the site user, which is
why those scripts must be owned by that user and mode 0550/0755 or suexec refuses them.
It has **no effect on php-fpm**: an FPM pool's identity comes from `user`/`group` in the
pool file, and the request reaches it over a socket/TCP, never through suexec. A site
switching from `fast-cgi` to `php-fpm` therefore changes *where* its identity is
enforced, and both the starter script and the pool must be reconciled in the same event.
`mpm_itk`'s `AssignUserId` is emitted unconditionally as a third identity mechanism for
hosts that use it.

### D14 — SSL vhost, cert paths, and Let's Encrypt

Cert paths are shared with nginx and unchanged (`letsencrypt.inc.php::
get_website_certificate_paths()`): `{document_root}/ssl/{ssl_domain}.{key,csr,crt,bundle}`,
switching to `{ssl_domain}-le.{key,crt,bundle}` when `ssl=y && ssl_letsencrypt=y`.
`ssl_domain` falls back to `domain`. The `ssl` folder is created on every `web_domain`
event for vhost-type rows, before anything else.

What is Apache-specific:

- **The SSL vhost is a separate `<VirtualHost :443>` block**, not a flag on the
  existing one. It is appended to the `vhosts` loop only when `ssl='y'`,
  `ssl_domain != ''`, and both the crt and key files exist and are non-empty. A site
  with `ssl=y` but no cert on disk silently renders HTTP-only — matching PHP, and the
  reason a failed ACME issuance does not take the site down.
- **`SSLCertificateChainFile` only below Apache 2.4.8.** From 2.4.8 the bundle is
  concatenated into the `.crt` file itself; `has_bundle_cert` is set only when a bundle
  file exists *and* the version is older.
- **OCSP stapling is probed, not assumed**: the plugin runs
  `openssl x509 -noout -ocsp_uri -in <crt>` and only emits `SSLUseStapling` +
  `SSLStaplingResponderTimeout` when the certificate actually carries an OCSP URI. The
  matching `SSLStaplingCache shmcb:/var/run/ocsp(128000)` goes **outside** the
  `</VirtualHost>` — it is a server-scope directive and emitting it inside is a config
  error.
- **`ssl_action` state machine** (`create` / `save` / `del`) is shared logic: `create`
  writes an openssl config with SANs and generates a 4096-bit self-signed cert (3650
  days), `save` validates the submitted material (rejects `Proc-Type: 4,ENCRYPTED`
  keys and any cert containing `.acme.invalid`) and writes it back, `del` removes csr/crt/
  bundle. Every branch clears `ssl_action` afterwards.
- **Let's Encrypt** is requested before the vhost is written, gated on
  `mirror_server_id == 0` and on a transition (`ssl` or `ssl_letsencrypt` newly `y`,
  domain changed, subdomain changed, or `update_letsencrypt` set by a child-row
  cascade). On success the DB's `ssl_request`/`ssl_cert`/`ssl_key` are cleared — the
  files on disk are authoritative. On failure `ssl_letsencrypt` is forced back to `n`
  (and `ssl` too, if it was `n` before) so the next event does not retry in a loop.
- **ACME challenge serving** is the `999-acme.conf` alias (D2) plus the `[END]`
  rewrite guard (D13), both of which must be present or renewal fails.
- On site delete with `le_delete_on_site_remove=y`, the certificate is looked up by
  serial number and removed from the ACME client's store, so renewal does not keep
  failing for a site that no longer exists.

### D15 — Custom `php.ini`, directive snippets, and the `php_ini_changed` action

Custom PHP settings land in `{website_basedir}/conf/{system_user}[_{web_folder}]/php.ini`
and are assembled in a strict order: (1) the master php.ini for the mode
(`php_ini_path_apache` for `mod`, `server_php.php_fpm_ini_dir`/`php_fastcgi_ini_dir` for
a custom version, `fastcgi_phpini_path` or `php_ini_path_cgi` otherwise), (2) the
`web_domain.custom_php_ini` text, (3) any `type='php'` directive snippets listed in the
selected `type='apache'` snippet's `required_php_snippets`. The file is deleted when
both sources are empty.

Apache directive snippets (`directive_snippets` where `type='apache' AND active='y' AND
customer_viewable='y'`) are injected as `apache_directives` after CRLF normalisation and
`{DOCROOT}` / `{DOCROOT_CLIENT}` / `{DOMAIN}` substitution. Unlike nginx, **there is no
directive blacklist** in the PHP source. `internal/nginx/blacklist.go` exists because
nginx directives can trivially escalate; the Apache equivalent (`php_admin_value`,
`SetHandler`, `Alias`) is at least as dangerous. This change therefore adds an Apache
directive blacklist mirroring the nginx one, defaulting to denying
`Include`, `IncludeOptional`, `LoadModule`, `SuexecUserGroup`, `User`, `Group`,
`ScriptAlias*`, `AssignUserId` and `PerlRequire`. This is a **deliberate divergence from
PHP parity**, justified by the same threat model that produced the nginx blacklist.

`php_ini_changed` (raised by `webserver_plugin.inc.php` when a master php.ini's checksum
changes) rewrites every affected site's custom php.ini and requests a single httpd
reload — reload, not restart, because no vhost changed.

### D16 — Log directory bind mounts

`/var/log/ispconfig/httpd/<domain>` is created (0750, group = `system_group`) and
**bind-mounted** onto `<document_root>/<log_folder>`, with a matching `/etc/fstab` line
(`none bind,nofail` plus `_netdev` when `network_filesystem=y`). Renaming a site
unmounts the old path, removes the fstab line, removes the old log dir, and re-mounts.
Deleting a site unmounts (`umount -l`), removes the fstab line and deletes the tree.
`error.log` is touched and chowned `root:<system_group>` 0640 so the FTP user can read
it via the client group.

nginx's Go port uses `DefaultLogBaseDir = "/var/log/ispconfig/httpd"` but does not bind
mount. Bind mounts are a real, root-privileged, reboot-persistent side effect: every
mount/umount and every fstab edit goes through the command runner and the fstab
line-replace helper, is logged with full argv, and is faked in tests. Path guards
(non-empty domain, no `..`) apply before any `rm -rf`.

### D17 — Site deletion and folder-reuse safety

`type=vhost` deletion is a plain `rm -rf <document_root>` (guarded on non-empty and
`..`-free). `vhostsubdomain`/`vhostalias` deletion is **not**: the plugin normalises the
`web_folder`, refuses outright if the first path element is `web` or empty, then walks
the path from the leaf upward, comparing against the set of folders (and their parent
prefixes) still used by sibling `vhostsubdomain`/`vhostalias` rows under the same
parent. It deletes only the deepest still-unused prefix. This logic already exists in
`internal/nginx/delete.go` (`siblingWebFolders`, `subdomainFolderToDelete`) and moves to
`internal/websites` unchanged.

Additionally on delete: vhost file and all three possible symlinks
(`<domain>.vhost`, `100-`, `900-`) removed, starter scripts and FPM pools cleaned per
mode, website symlinks removed, log dir removed, `userdel` for `type=vhost`, and
web-backup files pruned when `backup_delete=y`. `client_delete` removes
`{website_basedir}/clients/client<id>` (symlinks first, then `rmdir`) and `groupdel
client<id>`.

### D18 — Module enablement and mutual exclusion

The Apache plugin loads when `server.web_server = 1` **and**
`getconf.WebConfig.ServerType == "apache"`. The nginx plugin loads on the same
condition with `== "nginx"`. Neither loads otherwise, and the daemon logs a startup
error if `server_type` is unrecognised rather than silently serving nothing.
`web.RegisterServices` registers `httpd` mapped to unit `apache2` instead of `nginx`,
and `GuardedExecutor` gets the `apache2ctl configtest` command (D8). The FPM units
registered alongside are unchanged.

## Risks / Trade-offs

- [Restart-and-probe can take a site down for ~2s] → configtest short-circuit (D8)
  makes the common failure never reach the restart; `check_apache_config=n` is available
  for operators who prefer plain reloads.
- [Double `<Directory>` emission doubles vhost size] → accepted with a named ceiling and
  a documented upgrade path (D4); dropping it is a security regression, not an
  optimisation.
- [Five PHP modes, only one exercised in CI] → `php-fpm` (socket + TCP) gets integration
  tests on the lab VM; `mod`, `cgi`, `fast-cgi`, `suphp` get golden-file tests only.
  Their non-goal status in the installer means a fresh install cannot select them
  without operator action.
- [`chattr +i` on starter scripts] → the immutable flag must be cleared before every
  rewrite and re-set after; a crash between the two leaves a mutable script. Mitigated by
  doing the clear/write/set inside one handler and re-asserting the flag on every update.
- [Bind mounts and fstab edits are persistent root-level side effects] → command runner
  logs every argv, fstab helper does exact-line replace (never regex-rewrites the file),
  and every destructive path is unit-tested with a fake runner.
- [Apache directive blacklist diverges from PHP] → documented as an intentional
  security divergence (D15); the blocked list is exported for the API so the panel
  rejects bad snippets at save time rather than at render time.
- [Template fidelity vs dead branches] → `mod_ruby`/`mod_perl`/`mod_python`/`hhvm`
  blocks are rendered but never provisioned. They cost template complexity and are
  covered by golden files only. Removing them is a follow-up once migration data
  confirms nobody uses them.
- [`server_type` mismatch after migration] → an imported `web_domain` set with
  `server_type=apache` on a host where the installer ran with `--web-server nginx`
  produces a daemon that ignores every web event. The installer writes `server_type`
  into the seeded `[web]` section and the daemon logs loudly on mismatch.
- [Apps vhost port 8081 collides] → `apps_vhost_enabled=n` by default on fresh installs;
  the ISPConfig apps (phpMyAdmin, roundcube) are not part of this port.

## Migration Plan

- Ships as code only; no schema change. Existing `web_domain` rows work as-is.
- Fresh installs: `install --web-server apache2` provisions packages, modules, include
  wiring, log tree, ACME conf, `ispconfig.conf` and (optionally) the apps vhost.
- Migrated installs (`add-legacy-migration` from an Apache ISPConfig): after import, a
  resync touch on every `web_domain` row regenerates vhosts, pools, starter scripts,
  `.htaccess` and `.htpasswd` from the DB. Existing files are backed up as `~` first.
- `--update` re-runs the Apache base step (idempotent `a2enmod`, include-wiring check)
  without touching site vhosts.
- Rollback: set `server_type` back and disable the module; rendered files stay in place
  and Apache keeps serving the last-applied config.

## Open Questions

- Should `--web-server` accept `apache2` only, or also `apache` (matching the getconf
  value `server_type=apache`)? Leaning: accept both on the CLI, normalise to `apache`
  in getconf, since the seeded ini value must match what PHP wrote.
- Do we emit the `mod_fastcgi` branch at all on fresh installs, given the installer only
  enables `proxy_fcgi`? Leaning: yes, for golden-file parity and migrated-host safety —
  it is inert without the module.
- Is the Apache directive blacklist (D15) in scope here or a follow-up shared with
  nginx? Leaning: ship a minimal deny list here, unify with `internal/nginx/blacklist.go`
  in a later refactor.
- `webdav_user` events currently have no subscriber under Apache. Register a no-op that
  at least keeps the `# WEBDAV BEGIN/END` markers intact, or leave unregistered?
  Leaning: leave unregistered (dead code), markers are emitted by the template anyway.
