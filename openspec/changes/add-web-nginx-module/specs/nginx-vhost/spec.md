# nginx-vhost

## ADDED Requirements

### Requirement: Site filesystem provisioning
On `web_domain_insert` (and on `web_domain_update` when paths changed) for types `vhost`, `vhostsubdomain` and `vhostalias`, the nginx plugin SHALL ensure the site directory tree exists under the configured `website_basedir`/`website_path`: `web/` (or the configured `web_folder`), `log/`, `ssl/`, `tmp/` (mode 1777), `private/` and `cgi-bin/`, owned by the site's `system_user`/`system_group`, and SHALL create that system user and group when missing. The operation SHALL be idempotent.

#### Scenario: New vhost domain provisions the tree
- **WHEN** a `web_domain_insert` event for `example.com` with `system_user=web1`, `system_group=client1` is handled
- **THEN** the directory tree exists with correct ownership, `tmp/` has mode 1777, and system user `web1` and group `client1` exist

#### Scenario: Re-running is a no-op
- **WHEN** the same `web_domain_update` event is processed twice
- **THEN** the second run changes nothing and reports no error

#### Scenario: Domain rename moves the docroot
- **WHEN** a `web_domain_update` changes `document_root` (site renamed or moved)
- **THEN** the old document root is moved to the new path, the old vhost file is removed and a new one is written

### Requirement: Vhost rendered from nginx_vhost.conf.master
The nginx plugin SHALL generate the vhost file from the embedded `nginx_vhost.conf.master` via the master-templates renderer, populating at minimum: listen IP/ports (`http_port`, `https_port`), `server_name` with aliases, document root, error/access log paths, PHP-FPM `fastcgi_pass` (socket path or 127.0.0.1:port), SSL certificate/key paths when `ssl=y`, `rewrite_to_https` redirect when enabled, `redirect_type`/`redirect_path` rules, and SEO redirects (`seo_redirect` variants www/non-www/domain) computed as in `get_seo_redirects()`.

#### Scenario: Golden-file render for a standard PHP-FPM site
- **WHEN** a fixture `web_domain` (vhost, php=php-fpm, ssl=n, no redirects) is rendered
- **THEN** the output matches the committed golden file byte-for-byte

#### Scenario: rewrite_to_https adds the redirect server block
- **WHEN** a domain has `ssl=y` and `rewrite_to_https=y`
- **THEN** the rendered vhost redirects HTTP requests to `https://` with the configured `https_port`

#### Scenario: SEO redirect non-www to www
- **WHEN** a domain has `seo_redirect=non_www_to_www`
- **THEN** the rendered vhost contains a permanent redirect from `example.com` to `www.example.com`

### Requirement: Custom nginx directives merged with blacklist enforcement
The nginx plugin SHALL merge the `nginx_directives` field into the rendered vhost (port of `nginx_merge_locations`): custom `location` blocks with the same path replace/extend the template's block, `{FASTCGIPASS}` placeholders are substituted, and other directives are appended inside the `server` block. Before merging, every directive line SHALL be checked against the embedded `security/nginx_directives.blacklist` regex list; matching lines SHALL be stripped from the output and reported as a datalog error, never written to disk.

#### Scenario: Custom location block overrides template
- **WHEN** `nginx_directives` contains `location / { try_files $uri /index.php?$args; }`
- **THEN** the rendered vhost's `location /` block contains the custom content merged with the template content

#### Scenario: Blacklisted directive is stripped and reported
- **WHEN** `nginx_directives` contains `load_module /tmp/evil.so;`
- **THEN** the rendered vhost does not contain the line and a datalog error records the rejection

### Requirement: Vhost activation with nginx -t validation and rollback
The nginx plugin SHALL write the rendered vhost to the configured `vhost_conf_dir`, keep a backup of the previous file, ensure the `vhost_conf_enabled_dir` symlink for active domains, and validate with `nginx -t` before requesting a delayed `httpd` reload. When validation fails, the plugin SHALL save the broken file as `<vhost>.err`, restore the previous vhost (or write a placeholder comment file when none existed), restore backed-up SSL files when the certificate changed in the same run, and write the `nginx -t` output as a datalog error.

#### Scenario: Valid vhost goes live
- **WHEN** the rendered vhost passes `nginx -t`
- **THEN** the file is in `vhost_conf_dir`, symlinked from `vhost_conf_enabled_dir`, and one delayed `httpd` reload is scheduled

#### Scenario: Broken vhost is quarantined and rolled back
- **WHEN** the rendered vhost fails `nginx -t`
- **THEN** the broken content is saved as `.err`, the previous vhost is back in place, nginx is not reloaded with the broken file, and the error text is available to the panel via datalog error

#### Scenario: Deactivated domain removes the enabled symlink
- **WHEN** a `web_domain_update` sets `active=n`
- **THEN** the `vhost_conf_enabled_dir` symlink is removed and a delayed reload is scheduled

### Requirement: Site deletion
On `web_domain_delete` the nginx plugin SHALL remove the vhost file and its enabled symlink, delete the site's PHP-FPM pool, remove the site directory tree and the system user/group when no other domain uses them, and schedule a delayed reload. Deletion SHALL refuse to remove paths outside the configured `website_basedir` or equal to it.

#### Scenario: Delete removes vhost and directories
- **WHEN** a `web_domain_delete` event for a provisioned site is handled
- **THEN** vhost file, enabled symlink, pool file and document root are gone and nginx reload is scheduled

#### Scenario: Unsafe path is never deleted
- **WHEN** the payload's `document_root` resolves to `/` or outside `website_basedir`
- **THEN** no filesystem deletion is performed and an error is logged

### Requirement: Per-folder HTTP auth files
On `web_folder` and `web_folder_user` events the nginx plugin SHALL maintain `.htpasswd`-style auth files (port of `_create_web_folder_auth_configuration`) and re-render the parent domain's vhost so the protected `location` block with `auth_basic`, rendered from the embedded `nginx_http_authentication.auth.master` template, is added or removed. This capability is in scope of this module (not phase 2).

#### Scenario: Folder user creates auth entry
- **WHEN** a `web_folder_user_insert` for folder `/admin` of `example.com` is handled
- **THEN** the auth file contains the user's crypted password and the vhost protects `location /admin` with `auth_basic`
