# web-module-events Specification

## Purpose
TBD - created by archiving change add-web-nginx-module. Update Purpose after archive.
## Requirements
### Requirement: Web module registers table hooks
The web module SHALL register table hooks for `web_domain`, `web_folder` and `web_folder_user` in the module registry, and SHALL announce the events `web_domain_insert`, `web_domain_update`, `web_domain_delete`, `web_folder_insert`, `web_folder_update`, `web_folder_delete`, `web_folder_user_insert`, `web_folder_user_update`, `web_folder_user_delete` so plugins can subscribe to them.

#### Scenario: Datalog row fans out to a named event
- **WHEN** the daemon processes a `sys_datalog` row with `dbtable=web_domain` and `action=u`
- **THEN** the web module raises the `web_domain_update` event with the decoded `{old,new}` payload

#### Scenario: Unhooked table is ignored
- **WHEN** the daemon processes a `sys_datalog` row with `dbtable=ftp_user`
- **THEN** the web module raises no event and processing continues without error

### Requirement: client_delete cascade contract
The nginx plugin SHALL register a handler for the `client_delete` event (PHP parity: `nginx_plugin` subscribes to `client_delete`) that tears down every `web_domain` site owned by the deleted client's group, using the same removal path as site deletion. The event contract is declared by this module; the event itself is emitted by the future `add-client-module` — until that change lands the handler is registered but never invoked.

#### Scenario: client_delete removes the client's sites
- **WHEN** a `client_delete` event carrying the deleted client's group id is dispatched
- **THEN** each `web_domain` owned by that group is removed as in site deletion (vhost file, enabled symlink, PHP-FPM pool and site directories gone, delayed reload scheduled)

### Requirement: httpd service with config-check guard
The web module SHALL register an `httpd` service in the services registry whose restart/reload runs `nginx -t` first and SHALL abort the restart/reload, returning the `nginx -t` output as error, when the configuration test fails.

#### Scenario: Reload with valid configuration
- **WHEN** a delayed `httpd` reload is flushed and `nginx -t` exits 0
- **THEN** the nginx service is reloaded via its systemd unit

#### Scenario: Reload blocked by broken configuration
- **WHEN** a delayed `httpd` reload is flushed and `nginx -t` exits non-zero
- **THEN** no reload is executed and the `nginx -t` output is logged as an error

### Requirement: php-fpm services per PHP version
The web module SHALL register `php-fpm` services keyed per PHP version (the server default and each `server_php` row used), so a delayed reload targets only the FPM instance whose pools changed and duplicate reload requests within one daemon run are deduplicated.

#### Scenario: Only the affected FPM version is reloaded
- **WHEN** two sites on PHP 8.3 change pools and no PHP 8.2 pool changes in one daemon run
- **THEN** the PHP 8.3 FPM service is reloaded exactly once and the PHP 8.2 service is not touched

