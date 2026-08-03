# apache-php-fpm

## ADDED Requirements

### Requirement: PHP mode dispatch
The plugin SHALL support the `web_domain.php` values `no`, `mod`, `suphp`, `cgi`,
`fast-cgi` and `php-fpm`, and SHALL select the `DocumentRoot`, the emitted vhost
directives and the on-disk artefacts per mode. `DocumentRoot` SHALL be
`web_document_root` (the real client path) for `suphp`, `cgi`, `php-fpm` and `hhvm`, and
`web_document_root_www` (the legacy `{website_basedir}/{domain}` path) for `mod`,
`fast-cgi` and `no`. Rows with `php='hhvm'` SHALL be treated as `no` (port of
`vhost.conf.master` lines 28–44 and `apache2_plugin.inc.php::update()`).

#### Scenario: php-fpm site uses the client document root
- **WHEN** a site has `php='php-fpm'` and `document_root=/var/www/clients/client1/web1`
- **THEN** each `<VirtualHost>` block declares `DocumentRoot /var/www/clients/client1/web1/web`

#### Scenario: mod_php site uses the legacy document root
- **WHEN** a site has `php='mod'` and domain `example.tld`
- **THEN** each `<VirtualHost>` block declares `DocumentRoot {website_basedir}/example.tld/web`

#### Scenario: hhvm rows degrade to no PHP
- **WHEN** a migrated row carries `php='hhvm'`
- **THEN** no HHVM init script, monit config or FastCGI wiring is written and the vhost denies PHP files

### Requirement: mod_php directives
For `php='mod'` the renderer SHALL emit `AddType application/x-httpd-php .php .php3
.php4 .php5`, `SetEnv TMP`, `TMPDIR` and `TEMP` pointing at `{document_root}/tmp`,
`php_admin_value upload_tmp_dir` and `session.save_path` pointing at the same directory,
and `php_admin_value sendmail_path "/usr/sbin/sendmail -t -i -fwebmaster@<domain>"`
unless the site's custom php.ini already defines `sendmail_path`.
`php_admin_value open_basedir` SHALL be emitted only when `security_level=20` (port of
`vhost.conf.master` lines 258–273).

#### Scenario: Custom sendmail_path suppresses the default
- **WHEN** the site's `custom_php_ini` contains a `sendmail_path` line
- **THEN** the vhost omits the `php_admin_value sendmail_path` directive

#### Scenario: open_basedir only at high security level
- **WHEN** `security_level=10` and `php='mod'`
- **THEN** the vhost contains no `php_admin_value open_basedir` directive

### Requirement: suPHP mode
For `php='suphp'` the renderer SHALL emit a `<Directory {web_document_root}>` block
containing `<IfModule mod_suphp.c>` with `suPHP_Engine on`, `suPHP_ConfigPath
<custom_php_ini_dir>` when a custom php.ini exists, a `<FilesMatch "\.php[345]?$">`
setting handler `x-httpd-suphp`, and `suPHP_AddHandler x-httpd-suphp`. suPHP is the only
mode for which `open_basedir` SHALL be appended into the custom php.ini content when it
is not already present (port of `vhost.conf.master` lines 274–289 and
`apache2_plugin.inc.php::update()` open_basedir handling).

#### Scenario: suPHP gets open_basedir in its php.ini
- **WHEN** a site has `php='suphp'` and a `custom_php_ini` without `open_basedir`
- **THEN** the generated php.ini ends with an `open_basedir` line set to the site's `php_open_basedir`

### Requirement: CGI mode starter script
For `php='cgi'` the plugin SHALL create
`{website_basedir}/php-cgi-scripts/{system_user}/` (chowned to the site user and group,
mode 0755 at `security_level=10` and 0550 otherwise), render `php-cgi-starter.master`
into `php-cgi-starter[_web<domain_id>]`, apply the same ownership and mode, and set the
file immutable. The immutable flag SHALL be cleared before every rewrite and re-set
afterwards. The renderer SHALL emit `ScriptAlias /php-cgi <starter path>`, `Action
php-cgi /php-cgi`, `SetHandler php-cgi` inside `<FilesMatch "\.php[345]?$">` in both
`<Directory>` blocks, and a `<Directory {cgi_starter_path}>` grant (port of
`apache2_plugin.inc.php::update()` cgi block and `vhost.conf.master` lines 290–312).

#### Scenario: CGI starter script is created and locked
- **WHEN** a site is set to `php='cgi'` with `security_level=20`
- **THEN** the starter script exists, is owned by the site user and group, is mode 0550 and carries the immutable attribute

#### Scenario: Leaving CGI mode removes the starter
- **WHEN** a `type=vhost` site changes from `php='cgi'` to `php='php-fpm'`
- **THEN** the cgi starter directory is made mutable and removed

### Requirement: FastCGI mode starter script and mod_fcgid tuning
For `php='fast-cgi'` the plugin SHALL create the directory from
`fastcgi_starter_path` (with `[system_user]` and `[client_id]` substituted), render
`php-fcgi-starter.master` into
`{fastcgi_starter_script}[_web<domain_id>]` with the php.ini path resolved from the
custom php.ini directory, the custom PHP version's `php_fastcgi_ini_dir`, or
`fastcgi_phpini_path`, and apply the same ownership, mode and immutable handling as the
CGI starter. The renderer SHALL emit an `<IfModule mod_fcgid.c>` tuning block using
`Fcgid*` directive names when `fastcgi_config_syntax='2'` and the legacy names
otherwise, and in both `<Directory>` blocks `SetHandler fcgid-script` plus four
`FCGIWrapper` lines (`.php`, `.php3`, `.php4`, `.php5`), `Options +ExecCGI` and
`AllowOverride <allow_override>` (port of `apache2_plugin.inc.php::update()` fast-cgi
block and `vhost.conf.master` lines 313–373).

#### Scenario: FastCGI tuning uses the configured syntax
- **WHEN** `fastcgi_config_syntax='2'`
- **THEN** the vhost emits `FcgidIdleTimeout`, `FcgidMaxRequestsPerProcess` and the other `Fcgid*` directives rather than the legacy `IdleTimeout` names

#### Scenario: Leaving FastCGI mode removes the starter script
- **WHEN** a site changes from `php='fast-cgi'` to `php='no'`
- **THEN** the fastcgi starter script is removed, and for `type=vhost` the starter directory is removed as well

### Requirement: PHP-FPM pool file generation
For `php='php-fpm'` the plugin SHALL render `php_fpm_pool.conf.master` into
`{pool_dir}/web<domain_id>.conf`, where `pool_dir` is `server_php.php_fpm_pool_dir` when
`server_php_id != 0` and `[web] php_fpm_pool_dir` otherwise. The pool SHALL set `user`
and `group` from `system_user`/`system_group`, `listen.owner` from `system_user`,
`listen.group` from `[web] group`, `listen.mode` 0660, the `pm` family from the row, and
`php_admin_value[open_basedir]` from `php_open_basedir` (falling back to
`document_root`). Custom php.ini lines SHALL be converted to `php_admin_flag[...]` for
boolean values (`0`, `1`, `on`, `off`, `true`, `false`, `yes`, `no`, with `0` normalised
to `off`) and `php_admin_value[...]` otherwise, with comment lines (`;`, `#`, `//`)
skipped and `{WEBROOT}` substituted (port of
`apache2_plugin.inc.php::php_fpm_pool_update()`).

#### Scenario: Pool file is written for the site
- **WHEN** a site with `domain_id=1`, `system_user=web1` and `system_group=client1` is set to `php='php-fpm'`
- **THEN** `{pool_dir}/web1.conf` exists with section `[web1]`, `user = web1` and `group = client1`

#### Scenario: Boolean ini values become php_admin_flag
- **WHEN** `custom_php_ini` contains `display_errors = 0`
- **THEN** the pool file contains `php_admin_flag[display_errors] = off`

#### Scenario: Custom PHP version selects a different pool directory
- **WHEN** the row's `server_php_id` points at a `server_php` entry with its own `php_fpm_pool_dir`
- **THEN** the pool is written into that directory and not into the default one

### Requirement: Deterministic per-site FPM port and socket allocation
The plugin SHALL compute the FPM TCP port as
`[web] php_fpm_start_port + web_domain.domain_id - 1` and the socket path as
`{socket_dir}/web<domain_id>.sock`, where `socket_dir` is the custom PHP version's
`php_fpm_socket_dir` when set and `[web] php_fpm_socket_dir` otherwise. The same
computed value SHALL be used by both the pool renderer and the vhost renderer within one
event. `php_fpm_use_socket='y'` SHALL select the socket path and `'n'` the TCP port; the
socket directory SHALL be created when missing (port of
`apache2_plugin.inc.php::update()` and `php_fpm_pool_update()`).

#### Scenario: Port matches between pool and vhost
- **WHEN** a site with `domain_id=1` and `php_fpm_start_port=9010` uses TCP
- **THEN** the pool file listens on `127.0.0.1:9010` and every `SetHandler "proxy:fcgi://..."` in the vhost targets `127.0.0.1:9010`

#### Scenario: Socket mode creates the socket directory
- **WHEN** `php_fpm_use_socket='y'` and the socket directory does not exist
- **THEN** the directory is created before the pool file is written

### Requirement: Triple-emitted, IfModule-guarded PHP-FPM wiring
For `php='php-fpm'` the renderer SHALL emit all three wiring variants in the same file:
an `<IfModule mod_fastcgi.c>` block with a `<Directory {document_root}/cgi-bin>` grant,
`SetHandler php-fcgi` in both document-root `<Directory>` blocks, `Action php-fcgi
/php-fcgi virtual`, an `Alias /php-fcgi
{document_root}/cgi-bin/php-fcgi-{ip}-{port}-{domain}` and a matching
`FastCgiExternalServer` line using `-host 127.0.0.1:<fpm_port>` or `-socket <fpm_socket>`;
and an `<IfModule mod_proxy_fcgi.c>` block emitting
`SetHandler "proxy:fcgi://127.0.0.1:<fpm_port>"` for TCP or
`SetHandler "proxy:unix:<fpm_socket>|fcgi://localhost"` for sockets, in both document-root
`<Directory>` blocks. None of these variants SHALL be omitted (port of
`vhost.conf.master` lines 374–451, confirmed against the `legacy-apache` lab VM).

#### Scenario: Both module variants are present
- **WHEN** a `php='php-fpm'` site is rendered
- **THEN** the file contains both an `<IfModule mod_fastcgi.c>` block with `FastCgiExternalServer` and an `<IfModule mod_proxy_fcgi.c>` block with `SetHandler "proxy:..."`

#### Scenario: Socket mode uses the unix proxy form
- **WHEN** `php_fpm_use_socket='y'`
- **THEN** every `SetHandler` under `mod_proxy_fcgi` is `"proxy:unix:<socket>|fcgi://localhost"` and the `FastCgiExternalServer` line uses `-socket`

### Requirement: PHP-FPM handlers are guarded by a file-existence test
Every `SetHandler` that delegates a request to a PHP-FPM backend SHALL be nested inside
`<FilesMatch "\.php[345]?$">` and, within that, inside
`<If "-f '%{REQUEST_FILENAME}'">`. This guard prevents Apache from forwarding requests
for non-existent `.php` paths to the pool, which would otherwise enable path-info
attacks that resolve to attacker-influenced files. The guard SHALL NOT be omitted or
simplified in any variant (port of `vhost.conf.master` lines 384–449).

#### Scenario: Every php-fpm handler carries the -f guard
- **WHEN** a `php='php-fpm'` site is rendered in either socket or TCP mode
- **THEN** every `SetHandler` line for the FPM backend is enclosed in `<If "-f '%{REQUEST_FILENAME}'">`

#### Scenario: Golden test fails if the guard is removed
- **WHEN** the renderer is changed to emit `SetHandler` without the `<If>` wrapper
- **THEN** the php-fpm golden-file tests fail

### Requirement: Chrooted PHP-FPM environment rewriting
When `php_fpm_chroot='y'` the pool file SHALL set `chroot` to the document root, strip
the document root prefix from `open_basedir`, and the vhost SHALL emit inside
`<IfModule mod_proxy_fcgi.c>` and `<IfVersion >= 2.4.26>` the four `ProxyFCGISetEnvIf`
rewrites for `DOCUMENT_ROOT`, `CONTEXT_DOCUMENT_ROOT`, `HOME` and `SCRIPT_FILENAME`,
using `/{web_folder}` as the chroot-relative web folder (port of `vhost.conf.master`
lines 408–415 and `php_fpm_pool_update()` chroot block).

#### Scenario: Chrooted site rewrites the document root for the pool
- **WHEN** a site has `php_fpm_chroot='y'` and `web_folder='web'`
- **THEN** the vhost contains `ProxyFCGISetEnvIf "true" DOCUMENT_ROOT "/web"` and the pool's `open_basedir` is relative to the chroot

### Requirement: Pool lifecycle and cross-version pruning
When a site leaves `php='php-fpm'` or is deleted, the plugin SHALL remove
`{pool_dir}/web<domain_id>.conf`. On every pool write or delete it SHALL additionally
remove a pool of the same name from the default `php_fpm_pool_dir` and from every
`server_php` row's `php_fpm_pool_dir` on this server that differs from the active one,
and SHALL schedule a reload or restart of each affected FPM unit according to
`php_fpm_reload_mode` (port of `apache2_plugin.inc.php::php_fpm_pool_update()` and
`php_fpm_pool_delete()`).

#### Scenario: Switching PHP version removes the stale pool
- **WHEN** a site's `server_php_id` changes so that its pool directory changes
- **THEN** the pool file in the previous directory is removed and both FPM units are scheduled for reload

#### Scenario: Disabling PHP removes the pool
- **WHEN** a site changes from `php='php-fpm'` to `php='no'`
- **THEN** the pool file is removed from every PHP version's pool directory and the affected FPM units are scheduled

### Requirement: Custom php.ini generation
The plugin SHALL write `{website_basedir}/conf/{system_user}[_{web_folder}]/php.ini`
when `custom_php_ini` is non-empty or a directive snippet is selected, assembling
content in order: the master php.ini for the mode
(`php_ini_path_apache` for `mod`; the custom PHP version's `php_fpm_ini_dir` or
`php_fastcgi_ini_dir`; else `fastcgi_phpini_path` or `php_ini_path_cgi`), then the
row's `custom_php_ini` with CR characters stripped, then every `type='php'` snippet
listed in the selected apache snippet's `required_php_snippets`. When both sources are
empty the file SHALL be deleted (port of
`apache2_plugin.inc.php::get_master_php_ini_content()` and the custom php.ini block in
`update()`).

#### Scenario: Custom php.ini is created for a subdomain vhost
- **WHEN** a `type=vhostsubdomain` row with `web_folder='shop'` and `system_user='web1'` sets `custom_php_ini`
- **THEN** the file is written to `{website_basedir}/conf/web1_shop/php.ini`

#### Scenario: Clearing custom settings removes the file
- **WHEN** a site's `custom_php_ini` is emptied and no directive snippet is selected
- **THEN** the previously generated `php.ini` is deleted

### Requirement: php_ini_changed rewrites affected sites and reloads
On the `php_ini_changed` action the plugin SHALL select every `web_domain` row with a
non-empty `custom_php_ini` matching the reported mode (and `server_php_id` when a PHP
version is given), regenerate each site's custom php.ini, and request a single delayed
`httpd` reload. When no site is affected it SHALL request no service action (port of
`apache2_plugin.inc.php::php_ini_changed()`).

#### Scenario: Master php.ini change refreshes derived files
- **WHEN** `php_ini_changed` reports mode `php-fpm` for a specific PHP version
- **THEN** only sites on that version with custom settings are rewritten and one httpd reload is queued

#### Scenario: No affected sites means no reload
- **WHEN** `php_ini_changed` fires and no site has a custom php.ini for that mode
- **THEN** no service action is queued

### Requirement: Golden-file parity for pool files
Pool-file rendering SHALL be covered by golden files for at minimum: `pm=static`,
`pm=dynamic` and `pm=ondemand`, each in socket and TCP form, plus one with custom php.ini
values and one chrooted. At least one golden file SHALL be captured from the
`legacy-apache` lab VM.

#### Scenario: Rendered pool matches the captured reference
- **WHEN** the golden test renders the fixture matching the lab VM's `web1` pool
- **THEN** the output is byte-identical to `/etc/php/8.3/fpm/pool.d/web1.conf` captured from that VM
