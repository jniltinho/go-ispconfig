# php-fpm-pools

## ADDED Requirements

### Requirement: Pool file generated from php_fpm_pool.conf.master
For every `web_domain` with `php=php-fpm`, the plugin SHALL render a pool file named `web<domain_id>.conf` from the embedded `php_fpm_pool.conf.master` into the pool directory of the site's PHP version, populating: pool name, listen (unix socket in `php_fpm_socket_dir` when `php_fpm_use_socket=y`, else `127.0.0.1:<port>` with a port allocated per site), `user`/`group` from the site's system user, `pm` mode with its mode-specific keys (`dynamic`: start/min_spare/max_spare servers; `ondemand`: process_idle_timeout), `pm.max_children`, `pm.max_requests`, `open_basedir` (unless empty/`none`), tmp/session paths under the document root at security level 20, and each line of `custom_php_ini` as a `custom_php_ini_settings` loop entry.

#### Scenario: Golden-file render for a dynamic socket pool
- **WHEN** a fixture domain with `pm=dynamic`, `php_fpm_use_socket=y` is rendered
- **THEN** the pool file matches the committed golden file, with `listen` pointing at the socket path

#### Scenario: ondemand mode emits idle timeout only
- **WHEN** a domain has `pm=ondemand`
- **THEN** the pool contains `pm.process_idle_timeout` and no `pm.start_servers`/`pm.*_spare_servers` keys

#### Scenario: TCP mode allocates a port
- **WHEN** a domain has `php_fpm_use_socket=n`
- **THEN** the pool listens on `127.0.0.1:<port>` with a per-site port derived as in ISPConfig (base port + increment, persisted so it is stable across re-renders) and the vhost's `fastcgi_pass` uses the same address

### Requirement: PHP version resolution via server_php
The pool SHALL be written into the `php_fpm_pool_dir` of the PHP version referenced by `web_domain.server_php_id`; when `server_php_id` is 0/empty the server's default PHP-FPM (from the `[web]` server config) SHALL be used. After writing, a delayed reload of that version's FPM service SHALL be scheduled.

#### Scenario: Site pinned to an alternate PHP version
- **WHEN** a domain references a `server_php` row for PHP 8.2 with its own pool dir and init script
- **THEN** the pool file is written to the PHP 8.2 pool dir and the PHP 8.2 FPM service gets the delayed reload

### Requirement: Pool lifecycle on changes and deletion
When the PHP version, socket mode or PHP mode of a domain changes, the plugin SHALL delete the pool file from the previous version's pool directory and schedule a reload of the previous FPM service in addition to the new one; when `php` is switched away from `php-fpm` or the domain is deleted, the pool file and its socket SHALL be removed.

#### Scenario: Version switch moves the pool
- **WHEN** a domain moves from default PHP to a `server_php` PHP 8.3
- **THEN** the old pool file is deleted, the new one exists in the 8.3 pool dir, and both FPM services are reloaded once each

#### Scenario: Domain deletion removes the pool
- **WHEN** a `web_domain_delete` for a `php=php-fpm` site is handled
- **THEN** the pool file no longer exists and the owning FPM service is reloaded
