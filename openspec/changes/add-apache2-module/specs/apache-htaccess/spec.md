# apache-htaccess

## ADDED Requirements

### Requirement: Folder protection path resolution and guards
For `web_folder` and `web_folder_user` events the plugin SHALL resolve the protected
folder as `{web_domain.document_root}/{web_folder}/{web_folder.path}/`, where
`web_folder` is `web` for `type=vhost` rows and `web_domain.web_folder` for
`vhostsubdomain` and `vhostalias` rows. Leading and trailing slashes SHALL be stripped
from `web_folder.path` before joining, and a trailing slash SHALL be appended to the
result. The handler SHALL refuse and log when the resolved path contains `..`, `./` or
`\`, or when it does not start with the site's `document_root`. Missing folders SHALL be
created mode 0755 owned by `system_user:system_group` (port of
`apache2_plugin.inc.php::web_folder_user()`, `web_folder_update()` and
`web_folder_delete()`).

#### Scenario: Path traversal is refused
- **WHEN** a `web_folder` row has `path='../../etc'`
- **THEN** the handler logs the rejection and writes no `.htaccess` or `.htpasswd`

#### Scenario: Protected folder is created on demand
- **WHEN** a `web_folder_user` is added for a folder path that does not yet exist
- **THEN** the directory is created mode 0755 owned by the site's system user and group

#### Scenario: Subdomain site resolves against its own web folder
- **WHEN** the parent site is `type=vhostsubdomain` with `web_folder='shop'` and the folder path is `admin`
- **THEN** the resolved path is `{document_root}/shop/admin/`

### Requirement: .htpasswd maintenance
The plugin SHALL create an empty `.htpasswd` in the protected folder when it does not
exist, mode 0751 owned `system_user:system_group`. For each `web_folder_user` event it
SHALL upsert a `username:password` line when the row is active, and remove the line when
the row is deleted, deactivated, or when the username changed (removing the old username
first) (port of `apache2_plugin.inc.php::web_folder_user()`).

#### Scenario: Adding an active user writes a line
- **WHEN** `web_folder_user_insert` fires with `active='y'`, `username='alice'`
- **THEN** `.htpasswd` contains a line beginning `alice:` and the file is mode 0751 owned by the site user

#### Scenario: Deactivating a user removes the line
- **WHEN** a `web_folder_user` row is updated to `active='n'`
- **THEN** that username's line is removed from `.htpasswd` and no other lines are touched

#### Scenario: Renaming a user replaces the line
- **WHEN** a `web_folder_user` row's username changes from `alice` to `bob`
- **THEN** the `alice:` line is removed and a `bob:` line is written

#### Scenario: Deleting a user removes the line
- **WHEN** `web_folder_user_delete` fires
- **THEN** the username's line is removed from `.htpasswd`

### Requirement: Marker-delimited .htaccess block
The plugin SHALL write into the protected folder's `.htaccess` a block delimited by
`### ISPConfig folder protection begin ###` and
`### ISPConfig folder protection end ###` followed by two newlines, containing
`AuthType Basic`, `AuthName "Members Only"`, `AuthUserFile <absolute .htpasswd path>`
and `require valid-user`. When the file already exists and contains the markers, only
the marked region SHALL be replaced; when it exists without the markers, the block SHALL
be prepended and the existing content preserved. The file SHALL be mode 0751 owned
`system_user:system_group` (port of `apache2_plugin.inc.php::web_folder_user()`).

#### Scenario: New .htaccess is created with the marked block
- **WHEN** folder protection is enabled for a folder with no `.htaccess`
- **THEN** the file is created containing only the marked ISPConfig block, mode 0751

#### Scenario: Existing user content is preserved
- **WHEN** the folder already has an `.htaccess` with unrelated user directives and no markers
- **THEN** the ISPConfig block is prepended and every original line is still present

#### Scenario: Re-running replaces only the marked region
- **WHEN** protection is updated on a folder whose `.htaccess` already contains the markers plus user content below them
- **THEN** only the text between the markers changes and the user content below is untouched

### Requirement: Removing protection strips only the ISPConfig block
The plugin SHALL, on `web_folder_delete` or when a `web_folder` row is set to
`active='n'`, remove `.htpasswd` (delete case only) and strip the marked block from `.htaccess`,
falling back to removing the literal legacy four-line auth text when the markers are
absent. The `.htaccess` file SHALL be deleted only when nothing but whitespace remains;
otherwise the remaining content SHALL be written back (port of
`apache2_plugin.inc.php::web_folder_delete()` and `web_folder_update()`).

#### Scenario: Deleting protection removes both files when nothing else remains
- **WHEN** `web_folder_delete` fires for a folder whose `.htaccess` contains only the ISPConfig block
- **THEN** both `.htaccess` and `.htpasswd` are removed

#### Scenario: User content survives protection removal
- **WHEN** protection is removed from a folder whose `.htaccess` also holds user directives
- **THEN** `.htaccess` remains with the user directives and without the ISPConfig block

#### Scenario: Legacy unmarked block is still recognised
- **WHEN** an `.htaccess` written by an older ISPConfig contains the auth lines without markers
- **THEN** those four lines are removed

### Requirement: Folder rename moves protection files
When a `web_folder` row's `path` changes, the plugin SHALL create the new folder if
missing, move `.htpasswd` from the old path to the new one, strip the ISPConfig block
from the old `.htaccess` (deleting the file when nothing remains), and write the block
into the new `.htaccess` with the updated `AuthUserFile` path. Both the old and the new
resolved paths SHALL pass the traversal and docroot guards before anything is moved
(port of `apache2_plugin.inc.php::web_folder_update()`).

#### Scenario: Renaming a protected folder carries the users across
- **WHEN** a `web_folder` row's path changes from `admin` to `staff`
- **THEN** `.htpasswd` exists under `staff` with the same users, `staff/.htaccess` points its `AuthUserFile` at the new path, and `admin/.htaccess` no longer contains the ISPConfig block

#### Scenario: Rename with an unsafe new path is refused
- **WHEN** the new path resolves outside the document root
- **THEN** nothing is moved or written and the rejection is logged

### Requirement: Statistics folder protection
When `web_domain.stats_type` is non-empty the plugin SHALL create
`{document_root}/{web_folder}/stats` if missing and write an unmarked `.htaccess`
containing `AuthType Basic`, `AuthName "Members Only"`,
`AuthUserFile .../stats/.htpasswd_stats`, `require valid-user`,
`DirectoryIndex index.html index.php`, a `Header set Content-Security-Policy` line and a
`<Files "goaindex.html">` block setting `AddDefaultCharset UTF-8`, mode 0640 owned
`system_user:system_group`. `.htpasswd_stats` SHALL be written with a single
`admin:<stats_password>` line, mode 0640, whenever it is missing or `stats_password`
changed. When `stats_type` is cleared, the stats directory SHALL be removed (port of the
stats block in `apache2_plugin.inc.php::update()`).

#### Scenario: Enabling statistics writes the auth pair
- **WHEN** a site sets `stats_type='goaccess'` with a `stats_password`
- **THEN** `{document_root}/{web_folder}/stats/.htaccess` and `.htpasswd_stats` exist, both mode 0640 owned by the site user

#### Scenario: Changing the stats password rewrites the file
- **WHEN** `stats_password` changes on an existing site
- **THEN** `.htpasswd_stats` is rewritten with the new `admin:` line

#### Scenario: Disabling statistics removes the folder
- **WHEN** `stats_type` is cleared on a vhost-type site
- **THEN** the stats directory under the web folder is removed

### Requirement: AllowOverride policy
The renderer SHALL emit `AllowOverride <web_domain.allow_override>` in the document-root
`<Directory>` blocks, defaulting to `All` when the column is empty. The pre-vhost
`<Directory {website_basedir}/{domain}>` block and the `ispconfig.conf` `<Directory />`,
`/var/www/clients`, `/var/www/conf`, `/var/www/php-cgi-scripts` and
`/var/www/php-fcgi-scripts` blocks SHALL always emit `AllowOverride None` regardless of
the site setting. The getconf key `htaccess_allow_override` SHALL supply the panel's
default for new sites and SHALL NOT override a site's explicit value (port of
`apache2_plugin.inc.php::update()` `allow_override` assignment,
`vhost.conf.master` lines 3–11 and `apache_ispconfig.conf.master`).

#### Scenario: Empty allow_override defaults to All
- **WHEN** a site's `allow_override` column is empty
- **THEN** both document-root `<Directory>` blocks emit `AllowOverride All`

#### Scenario: Restricted allow_override is honoured
- **WHEN** a site sets `allow_override='AuthConfig Limit'`
- **THEN** both document-root `<Directory>` blocks emit `AllowOverride AuthConfig Limit`

#### Scenario: Domain root always denies overrides
- **WHEN** any vhost is rendered, regardless of the site's `allow_override`
- **THEN** the pre-vhost `<Directory {website_basedir}/{domain}>` block emits `AllowOverride None` and `Require all denied`

### Requirement: Protection files are written even when overrides are disabled
The plugin SHALL write `.htaccess` and `.htpasswd` for active folder protection even
when the site's `allow_override` would cause Apache to ignore them, so that re-enabling
overrides takes effect without a resync. The API and panel SHALL surface a warning when
folder protection is configured on a site whose `allow_override` excludes `AuthConfig`
and is not `All`.

#### Scenario: Files written on a site with AllowOverride None
- **WHEN** folder protection is enabled on a site with `allow_override='None'`
- **THEN** `.htaccess` and `.htpasswd` are still written to the folder

#### Scenario: Panel warns about ineffective protection
- **WHEN** the panel saves folder protection for a site whose `allow_override` excludes `AuthConfig`
- **THEN** the response carries a warning that the protection will not be enforced by Apache
