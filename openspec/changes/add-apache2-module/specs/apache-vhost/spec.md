# apache-vhost

## ADDED Requirements

### Requirement: Apache plugin loads only when server_type is apache
The daemon SHALL load the Apache plugin when `server.web_server = 1` and
`getconf.WebConfig.ServerType` is `apache`, and SHALL load the nginx plugin when it is
`nginx`. The two plugins SHALL NOT be active simultaneously. The `httpd` service SHALL
be registered against unit `apache2`, and the guarded executor SHALL run
`apache2ctl configtest` in place of `nginx -t` (port of
`apache2_plugin.inc.php::onInstall()` and `web_module.inc.php` service registration).

#### Scenario: Apache server type activates the Apache plugin
- **WHEN** the daemon starts on a server with `web_server = 1` and `[web] server_type=apache`
- **THEN** the Apache plugin subscribes to `web_domain_*`, `web_folder_*`, `web_folder_user_*`, `server_ip_*`, `server_*` and `client_delete`, and the nginx plugin subscribes to nothing

#### Scenario: Unknown server type fails loudly
- **WHEN** `server_type` is neither `apache` nor `nginx`
- **THEN** the daemon logs an error at startup and no web plugin is registered

### Requirement: Vhost file rendered from vhost.conf.master
The plugin SHALL render `vhost.conf.master` through `internal/mastertpl` for
`web_domain` rows of type `vhost`, `vhostsubdomain` or `vhostalias`, and write the result to
`{vhost_conf_dir}/{domain}.vhost` (extension `.vhost`, not `.conf`). The existing file
SHALL be copied to `{file}~` before being overwritten, and that backup SHALL be removed
only after a successful reload or restart. Template variables SHALL include
`apache_version` and `apache_full_version` so the template's `format="version"`
comparisons at 2.2, 2.4, 2.4.8, 2.4.11, 2.4.26 and 2.4.30 resolve correctly (port of
`apache2_plugin.inc.php::update()`).

#### Scenario: Site insert writes a vhost file
- **WHEN** `web_domain_insert` fires for an active `type=vhost` row
- **THEN** `{vhost_conf_dir}/{domain}.vhost` exists and contains a `<VirtualHost>` block with `ServerName {domain}`

#### Scenario: Missing document_root aborts without writing
- **WHEN** a vhost-type row has an empty `document_root`
- **THEN** the handler logs a warning and no vhost file, user, group or directory is created

#### Scenario: Root-owned system user is rejected
- **WHEN** `system_user` or `system_group` resolves to root or an existing non-website account
- **THEN** the handler logs a warning and returns without writing anything

### Requirement: One VirtualHost block per listener combination
The renderer SHALL build a `vhosts` loop containing one entry per combination of IP
family, SSL state and proxy-protocol port, in this order: IPv4 HTTP, IPv4 HTTP
proxy-protocol port, IPv4 HTTPS, IPv4 HTTPS proxy-protocol port, IPv6 HTTP, IPv6 HTTP
proxy-protocol port, IPv6 HTTPS, IPv6 HTTPS proxy-protocol port. IPv6 addresses SHALL be
bracketed, `*` SHALL become `::` for IPv6, and proxy-protocol entries SHALL only be
emitted when `vhost_proxy_protocol_enabled` is `all`, or is `y` and the row has
`proxy_protocol='y'`, and the corresponding port is greater than zero (port of
`apache2_plugin.inc.php::update()` vhost array assembly).

#### Scenario: Dual-stack SSL site renders four VirtualHost blocks
- **WHEN** a site has an IPv4 address, an IPv6 address, `ssl='y'` and valid cert and key files on disk
- **THEN** the rendered file contains exactly four `<VirtualHost>` blocks: `ip:80`, `ip:443`, `[v6]:80` and `[v6]:443`

#### Scenario: SSL requested but certificate missing renders HTTP only
- **WHEN** `ssl='y'` but the crt or key file is absent or zero-length
- **THEN** no `:443` block is emitted and the site continues to serve over HTTP

### Requirement: Security-deny Directory block precedes the VirtualHost
The renderer SHALL emit, before the first `<VirtualHost>`, a
`<Directory {website_basedir}/{domain}>` block containing `AllowOverride None` and
(Apache > 2.2) `Require all denied` (port of `vhost.conf.master` lines 3–11).

#### Scenario: Domain root is denied
- **WHEN** any vhost is rendered
- **THEN** the file begins with a `<Directory>` block for the domain root carrying `AllowOverride None` and `Require all denied`

### Requirement: Paired Directory blocks for legacy and client paths
For every path the template covers, the renderer SHALL emit two `<Directory>` blocks
with identical bodies — one for `web_document_root_www`
(`{website_basedir}/{domain}/{web_folder}`) and one for `web_document_root`
(`{document_root}/{web_folder}`). This duplication SHALL be reproduced verbatim and
SHALL NOT be deduplicated. Each body SHALL begin with `<FilesMatch ".+\.ph(p[345]?|t|tml)$">`
containing `SetHandler None`, followed by `Options +SymlinksIfOwnerMatch` when
`disable_symlinknotowner='n'`, `AllowOverride <allow_override>`, and `Require all granted`
(port of `vhost.conf.master` lines 108–175, confirmed against the rendered vhost on the
`legacy-apache` lab VM).

#### Scenario: Both document root paths are granted
- **WHEN** a site with document root `/var/www/clients/client1/web1` and domain `wp1a.goisp.test` is rendered
- **THEN** the output contains a `<Directory /var/www/wp1a.goisp.test/web>` block and a `<Directory /var/www/clients/client1/web1/web>` block with identical bodies

#### Scenario: Inherited PHP handlers are cleared first
- **WHEN** either `<Directory>` block is emitted
- **THEN** its first directive is a `<FilesMatch ".+\.ph(p[345]?|t|tml)$">` containing `SetHandler None`

#### Scenario: PHP disabled denies PHP files in both blocks
- **WHEN** `web_domain.php = 'no'`
- **THEN** both `<Directory>` blocks contain a `<Files ~ '.php[s3-6]{0,1}$'>` section with `Require all denied`

### Requirement: ServerAlias assembly from aliases, subdomains and autoalias
The renderer SHALL build the `ServerAlias` list from: the site's own `subdomain`
setting (`www.<domain>` or `*.<domain>`), the `website_autoalias` pattern with
`[client_id]`, `[website_id]`, `[client_username]` and `[website_domain]` substituted,
and every active child `web_domain` row whose type is neither `vhostsubdomain` nor
`vhostalias` (contributing `www.<d> <d>`, `*.<d> <d>` or `<d>` per its own `subdomain`
value). The list SHALL start a new `ServerAlias` line after every 30 names (port of
`apache2_plugin.inc.php::update()` server_alias assembly).

#### Scenario: www subdomain adds an alias
- **WHEN** a site has `subdomain='www'`
- **THEN** the rendered vhost contains `ServerAlias www.<domain>`

#### Scenario: Alias domains are folded into the parent
- **WHEN** an active `type=alias` child row exists with `subdomain='www'`
- **THEN** the parent's `ServerAlias` list contains both `www.<alias_domain>` and `<alias_domain>`

#### Scenario: Alias list wraps every 30 entries
- **WHEN** a site has 65 alias names
- **THEN** the rendered vhost contains three `ServerAlias` lines

### Requirement: Non-vhost rows re-render the parent site
The plugin SHALL, when a `web_domain` row of type `alias` or `subdomain` is inserted,
updated or deleted, load the active parent row identified by `parent_domain_id`, set
`update_letsencrypt`, and re-run the vhost update against the parent instead of writing
a vhost for the child. If `parent_domain_id` itself changed on an update, the previous
parent SHALL be re-rendered first (port of `apache2_plugin.inc.php::update()` parent
cascade and `delete()`).

#### Scenario: Adding an alias re-renders the parent
- **WHEN** `web_domain_insert` fires for a `type=alias` row
- **THEN** the parent site's vhost is regenerated with the new `ServerAlias` and a Let's Encrypt request is considered for the parent

#### Scenario: Re-parenting an alias updates both parents
- **WHEN** an alias row's `parent_domain_id` changes from A to B
- **THEN** site A's vhost is re-rendered without the alias and site B's vhost is re-rendered with it

### Requirement: vhostsubdomain and vhostalias site layout
For `type=vhostsubdomain` and `type=vhostalias` rows the plugin SHALL use
`web_domain.web_folder` in place of `web`, SHALL derive the log subfolder by stripping
the parent domain suffix from the row's domain (falling back to `web<domain_id>`), and
SHALL suffix the custom php.ini directory with `_<web_folder>` and starter script names
with `_web<domain_id>`. A blacklisted web folder SHALL abort the handler with an error
(port of `apache2_plugin.inc.php::update()` web_folder resolution).

#### Scenario: Subdomain vhost uses its own web folder
- **WHEN** a `type=vhostsubdomain` row has `web_folder='shop'` under parent `example.com` with domain `shop.example.com`
- **THEN** the `<Directory>` blocks target `{document_root}/shop` and the log folder is `log/shop`

#### Scenario: Blacklisted web folder is refused
- **WHEN** a `vhostsubdomain` row sets `web_folder` to a blacklisted path
- **THEN** the handler logs an error and writes no vhost, pool or directory

### Requirement: Redirects and rewrite rules
When `redirect_type` and `redirect_path` are both non-empty the renderer SHALL emit a
rewrite rule per applicable domain pattern. A non-URL `redirect_path` not ending in `/`
SHALL have `/` appended; a `[scheme]` prefix SHALL expand to `http` in non-SSL blocks
and `https` in SSL blocks; `redirect_type='no'` SHALL emit no bracketed flag. Domain
patterns SHALL have `.`, `*`, `?` and `+` regex-escaped, using `^` for exact domains and
`(^|\.)` for wildcards. Non-URL targets SHALL additionally emit `RewriteCond` exclusions
for `!^/webdav/`, `!^/php-fcgi/` and `!^<target>`. Wildcard rules SHALL be appended after
all non-wildcard rules (port of `apache2_plugin.inc.php::update()` rewrite_rules
assembly and `_rewrite_quote()`).

#### Scenario: 301 redirect to an external URL
- **WHEN** a site has `redirect_type='R=301'` and `redirect_path='https://other.tld/'`
- **THEN** the rendered vhost contains `RewriteEngine on` and a `RewriteRule ^/(.*)$ https://other.tld/$1  [R=301]`

#### Scenario: Internal redirect excludes the php-fcgi alias
- **WHEN** `redirect_path` is a local path rather than a URL
- **THEN** the rule is preceded by `RewriteCond %{REQUEST_URI} !^/php-fcgi/` and `!^/webdav/`

#### Scenario: Wildcard rules come last
- **WHEN** a site has both an exact-domain redirect and a `subdomain='*'` redirect
- **THEN** the wildcard `RewriteRule` appears after the exact-domain one

### Requirement: ACME challenge survives every rewrite
On Apache 2.4 and newer the renderer SHALL emit
`RewriteCond %{REQUEST_URI} ^/\.well-known/acme-challenge/` followed by
`RewriteRule ^ - [END]` as the first rules inside the rewrite block. On Apache below 2.4
it SHALL instead emit a negative `RewriteCond %{REQUEST_URI} !^/\.well-known/acme-challenge/`
on every individual rule (port of `vhost.conf.master` lines 511–560).

#### Scenario: Redirect does not shadow ACME
- **WHEN** a site with a catch-all redirect is rendered on Apache 2.4.58
- **THEN** the `[END]` acme-challenge guard appears before the redirect rules

### Requirement: rewrite_to_https is emitted only in the plain vhost
`rewrite_to_https='y'` SHALL emit `RewriteCond %{HTTPS} off` and
`RewriteRule (.*) https://%{HTTP_HOST}%{REQUEST_URI} [R=301,L,NE]` inside non-SSL
`<VirtualHost>` blocks only, and `apache_directives` SHALL be omitted from those blocks
when it is set. When `ssl='n'` the flag SHALL be forced off (port of
`vhost.conf.master` lines 552–594).

#### Scenario: HTTPS redirect only in the port 80 block
- **WHEN** a site has `ssl='y'` and `rewrite_to_https='y'`
- **THEN** the `:80` block contains the HTTPS redirect and omits `apache_directives`, and the `:443` block contains `apache_directives` and no redirect

### Requirement: suexec and mpm_itk identity directives
When `suexec='y'` the renderer SHALL emit `SuexecUserGroup <system_user> <system_group>`
inside `<IfModule mod_suexec.c>`, and SHALL unconditionally emit
`AssignUserId <system_user> <system_group>` inside `<IfModule mpm_itk_module>`. These
directives govern `cgi` and `fast-cgi` execution only and SHALL NOT be relied on for
`php-fpm`, whose identity comes from the pool file (port of `vhost.conf.master` lines
252–257 and 563–566).

#### Scenario: suexec identity is emitted per VirtualHost
- **WHEN** a site has `suexec='y'`, `system_user='web1'` and `system_group='client1'`
- **THEN** each `<VirtualHost>` block contains `<IfModule mod_suexec.c>` wrapping `SuexecUserGroup web1 client1`

### Requirement: Error documents and logging
When `errordocs` is set the renderer SHALL emit `Alias /error/ "<web_document_root_www>/error/"`
and `ErrorDocument` lines for 400, 401, 403, 404, 405, 500, 502 and 503. `ErrorLog`
SHALL be `/var/log/ispconfig/httpd/<domain>/error.log` when `logging='yes'` and the
vlogger pipe form when `logging='anon'`; neither SHALL be emitted when logging is off
(port of `vhost.conf.master` lines 62–79).

#### Scenario: Error pages are aliased
- **WHEN** a site has `errordocs` enabled
- **THEN** the vhost contains `Alias /error/` and eight `ErrorDocument` lines

#### Scenario: Anonymous logging uses vlogger
- **WHEN** `[web] logging=anon`
- **THEN** `ErrorLog` is the vlogger pipe form with the `-e -n -P` flags

### Requirement: Custom Apache directives with placeholder substitution and blacklist
Directive snippets selected via `web_domain.directive_snippets_id` SHALL be loaded from
`directive_snippets` where `type='apache' AND active='y' AND customer_viewable='y'`,
normalised to Unix line endings, and have `{DOCROOT}`, `{DOCROOT_CLIENT}` and `{DOMAIN}`
substituted before being emitted as `apache_directives`. Directives on the Apache
blacklist (`Include`, `IncludeOptional`, `LoadModule`, `SuexecUserGroup`, `User`,
`Group`, `ScriptAlias`, `ScriptAliasMatch`, `AssignUserId`, `PerlRequire`) SHALL be
stripped and the removal logged (port of `apache2_plugin.inc.php::update()` snippet
handling; the blacklist is an intentional divergence mirroring
`internal/nginx/blacklist.go`).

#### Scenario: Placeholders are substituted
- **WHEN** a snippet contains `{DOCROOT_CLIENT}`
- **THEN** the rendered vhost contains the site's `{document_root}/{web_folder}` path

#### Scenario: Blacklisted directive is stripped
- **WHEN** a snippet contains `LoadModule foo modules/mod_foo.so`
- **THEN** that line is absent from the rendered vhost and the removal is logged

### Requirement: Activation symlinks
The plugin SHALL symlink the vhost into `{vhost_conf_enabled_dir}` as
`900-{domain}.vhost` when `subdomain='*'` and `100-{domain}.vhost` otherwise, only when
`active='y'`. Any legacy unprefixed `{domain}.vhost` symlink SHALL be removed. When
`subdomain` changes or `active` becomes `n`, both the `100-` and `900-` symlinks SHALL be
removed. When the domain name changes, the old symlinks and the old vhost file SHALL be
deleted (port of `apache2_plugin.inc.php::update()` symlink handling).

#### Scenario: Wildcard site uses the 900 prefix
- **WHEN** an active site has `subdomain='*'`
- **THEN** `{vhost_conf_enabled_dir}/900-{domain}.vhost` is a symlink to the vhost file and no `100-` symlink exists

#### Scenario: Deactivating a site removes its symlinks
- **WHEN** a site is updated to `active='n'`
- **THEN** neither the `100-` nor the `900-` symlink exists and the vhost file remains on disk

#### Scenario: Renaming a site cleans up the old files
- **WHEN** a site's `domain` changes from `old.tld` to `new.tld`
- **THEN** `old.tld.vhost` and all its symlinks are removed and `new.tld.vhost` with a `100-` symlink exists

### Requirement: Config validation with restart probe and rollback
When `check_apache_config='y'` the plugin SHALL run `apache2ctl configtest` first and
abort without restarting if it fails. If configtest passes it SHALL probe TCP
`localhost:80`, restart `httpd`, then re-probe up to five times with a short delay. If
the server was reachable before and is not after, or the restart returned non-zero, the
plugin SHALL copy the vhost to `{file}.err`, restore `{file}~` (or write a warning stub
when no backup exists), restore any `~`-backed SSL key, crt, csr and bundle when the
certificate changed, record the failure output in the datalog error channel, and restart
again. When `check_apache_config='n'` it SHALL request a delayed reload instead (port of
`apache2_plugin.inc.php::update()` config check block).

#### Scenario: Invalid config never reaches a restart
- **WHEN** the rendered vhost fails `apache2ctl configtest`
- **THEN** no restart is attempted, the file is quarantined as `.err`, the backup is restored and the error is written to the datalog

#### Scenario: Restart failure rolls back including SSL material
- **WHEN** Apache was serving before the change and fails to come back after the restart, and the certificate changed in this event
- **THEN** the vhost and the key, crt, csr and bundle files are restored from their backups, the failing versions are kept with `.err` suffixes, and Apache is restarted again

#### Scenario: Config check disabled uses a delayed reload
- **WHEN** `check_apache_config='n'`
- **THEN** the plugin queues a delayed `reload` on `httpd` and performs no probe

### Requirement: Site filesystem provisioning
On insert or update of a vhost-type row the plugin SHALL ensure the system group and
user exist (honouring `connect_userid_to_webid` fixed uid/gid allocation and
`add_web_users_to_sshusers_group`), create `web`, `web/error`, `web/stats`, `ssl`,
`cgi-bin`, `tmp`, `webdav`, `backup`, `.composer`, `.ssh` and `private` under the
document root with the modes and ownership dictated by `security_level` (10 or 20),
install error pages and the skeleton index on insert, apply the disk quota via
`xfs_quota` or `setquota`, and maintain the `website_symlinks` links with `[client_id]`
and `[website_domain]` substituted (port of `apache2_plugin.inc.php::update()`).

#### Scenario: New site gets its directory tree
- **WHEN** `web_domain_insert` fires for a `type=vhost` row
- **THEN** the document root contains `web`, `ssl`, `cgi-bin`, `tmp`, `webdav`, `backup`, `private`, `.ssh` and `.composer` with the expected ownership and modes

#### Scenario: High security level adds the Apache user to the client group
- **WHEN** `security_level=20` and the row is `type=vhost`
- **THEN** the Apache system user is a member of the site's system group and the document root is `root:root` 0755

#### Scenario: Changing the client moves the document root
- **WHEN** a site's `document_root` changes on update
- **THEN** the old symlinks are removed, the log mount is unmounted, the tree is moved, ownership is changed to the new user and group, the user's home directory is updated and `php_open_basedir` is rewritten to the new path

### Requirement: Log directory bind mounts
The plugin SHALL create `/var/log/ispconfig/httpd/<domain>` (mode 0750, group
`system_group`), bind-mount it onto `{document_root}/{log_folder}` and maintain the
matching `/etc/fstab` line (`none bind,nofail`, plus `_netdev` when
`network_filesystem='y'`). Renaming a site SHALL unmount the old path, remove its fstab
line and log directory, and re-mount at the new path. `error.log` SHALL be created and
chowned `root:<system_group>` mode 0640 (port of `apache2_plugin.inc.php::update()` log
mount handling).

#### Scenario: New site log directory is mounted and recorded
- **WHEN** a vhost-type site is created
- **THEN** `/var/log/ispconfig/httpd/<domain>` is bind-mounted at `{document_root}/log` and `/etc/fstab` contains the matching line

#### Scenario: Renaming a site remounts the log directory
- **WHEN** a site's domain changes
- **THEN** the old mount is lazily unmounted, the old fstab line and log directory are removed, and the new path is mounted and recorded

### Requirement: Site deletion
Deleting a `type=vhost` row SHALL unmount and remove the log directory and its fstab
line, remove the vhost file and all three possible symlinks, `rm -rf` the document root
(guarded on non-empty and free of `..`), remove starter scripts and FPM pools for the old
PHP mode, remove the website symlinks, `userdel` the system user, prune web backups when
`backup_delete='y'`, and queue an httpd reload. Deleting a `vhostsubdomain` or
`vhostalias` row SHALL delete only the deepest `web_folder` prefix not still used by a
sibling row under the same parent, and SHALL refuse outright when the first path element
is `web` or empty (port of `apache2_plugin.inc.php::delete()`).

#### Scenario: Deleting a site removes everything it owns
- **WHEN** `web_domain_delete` fires for a `type=vhost` row
- **THEN** the vhost file, its symlinks, the document root, the log directory, the fstab line, the FPM pool and the system user are all gone

#### Scenario: Shared subdomain folder is preserved
- **WHEN** a `vhostsubdomain` with `web_folder='apps/shop'` is deleted while a sibling uses `apps/blog`
- **THEN** only `apps/shop` is removed and `apps` is preserved

#### Scenario: Deleting a subdomain never touches web
- **WHEN** a `vhostsubdomain` row's normalised `web_folder` begins with `web` or is empty
- **THEN** no directory is deleted

### Requirement: Client deletion cleanup
On `client_delete` the plugin SHALL remove every symlink inside
`{website_basedir}/clients/client<id>`, `rmdir` that directory, and `groupdel
client<id>` when the group exists. The client directory path SHALL be rejected if it
contains `..` (port of `apache2_plugin.inc.php::client_delete()`).

#### Scenario: Client directory and group are removed
- **WHEN** `client_delete` fires for client 7
- **THEN** symlinks under `{website_basedir}/clients/client7` are unlinked, the directory is removed and group `client7` is deleted

### Requirement: Server-level ispconfig.conf regeneration
On `server_ip_insert|update|delete` and `server_insert|update` the plugin SHALL render
`apache_ispconfig.conf.master` into `{vhost_conf_dir}/ispconfig.conf`, using the current
`logging` setting and every `server_ip` row for this server where `virtualhost='y'`,
expanding each row's comma-separated `virtualhost_port` list into `ip_adresses` loop
entries with ports validated to be between 1 and 65535 and IPv6 addresses bracketed
(port of `apache2_plugin.inc.php::server_ip()`).

#### Scenario: Adding a server IP regenerates the include
- **WHEN** `server_ip_insert` fires for an IPv6 address with `virtualhost='y'` and `virtualhost_port='80,443'`
- **THEN** `{vhost_conf_dir}/ispconfig.conf` is rewritten and contains two loop entries with the address in brackets

#### Scenario: Global deny blocks are always present
- **WHEN** `ispconfig.conf` is rendered
- **THEN** it contains `Require all denied` blocks for `/`, `/var/www/clients`, `/var/www/conf`, `/var/www/php-cgi-scripts` and `/var/www/php-fcgi-scripts`

### Requirement: ISPConfig apps vhost
When `server_type='apache'` the apps-vhost handler SHALL render
`apache_apps.vhost.master` into `{vhost_conf_dir}/apps.vhost`, performing the
post-render replacement of the legacy `{apps_vhost_ip}`, `{apps_vhost_port}`,
`{apps_vhost_dir}`, `{apps_vhost_servername}`, `{apps_vhost_basedir}` and
`{vhost_port_listen}` placeholders. The `Listen` directive SHALL be commented out when
the port is 80 or 443. SSL directives SHALL be uncommented only when the panel's
`ispserver.crt` and `ispserver.key` exist, and the CA bundle line only when
`ispserver.bundle` also exists. Rspamd passthrough SHALL be emitted and `proxy` plus
`proxy_http` enabled only when `mail.content_filter='rspamd'`. The symlink
`{vhost_conf_enabled_dir}/000-apps.vhost` SHALL be created when `apps_vhost_enabled='y'`
and removed when it is `n`, followed by a delayed `httpd` restart (port of
`apps_vhost_plugin.inc.php`, apache branch).

#### Scenario: Apps vhost enabled on port 8081
- **WHEN** `apps_vhost_enabled='y'` and `apps_vhost_port=8081`
- **THEN** `apps.vhost` contains an uncommented `Listen 8081` and `000-apps.vhost` is a symlink to it

#### Scenario: Apps vhost on port 443 omits Listen
- **WHEN** `apps_vhost_port=443`
- **THEN** the `Listen` line is commented out

#### Scenario: Disabling the apps vhost removes the symlink
- **WHEN** `apps_vhost_enabled` changes to `n`
- **THEN** `000-apps.vhost` no longer exists and a delayed httpd restart is queued

### Requirement: Golden-file parity with the PHP renderer
The Go renderer SHALL produce byte-identical output to the PHP renderer for the same
input rows. Golden files SHALL cover at minimum: plain HTTP, HTTP+HTTPS, `php='no'`,
`php='mod'`, `php='fast-cgi'`, `php='cgi'`, `php='php-fpm'` over socket, `php='php-fpm'`
over TCP, `vhostsubdomain`, `vhostalias`, SEO redirect, `redirect_type` redirect, and a
site with directive snippets. At least one golden file SHALL be captured from the
`legacy-apache` lab VM rather than generated.

#### Scenario: Rendered vhost matches the captured reference
- **WHEN** the golden test renders the fixture row matching the lab VM's `wp1a.goisp.test` site
- **THEN** the output is byte-identical to the vhost captured from that VM
