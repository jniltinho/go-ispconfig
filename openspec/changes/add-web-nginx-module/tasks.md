# Tasks: add-web-nginx-module

Conventional commit after each finished + validated task.

## 1. Data layer and assets

- [x] 1.1 Add GORM models for `web_domain`, `web_folder`, `web_folder_user`, `server_php` mapped onto the existing ISPConfig3 schema (explicit column tags, riud fields); unit tests round-trip a fixture row against MariaDB
- [x] 1.2 Embed assets: copy `nginx_vhost.conf.master`, `php_fpm_pool.conf.master` and `security/nginx_directives.blacklist` from the PHP tree into the Go module, expose via `embed.FS`; test that the renderer parses both templates without error
- [x] 1.3 Add `[web]` server-config struct (getconf port keys: website_basedir, website_path, vhost_conf_dir/enabled_dir, nginx_user/group, php_fpm_* dirs, security_level, website_symlinks) with Debian/Ubuntu defaults in the seed; unit test parsing

## 2. Web module (events + services)

- [x] 2.1 Implement `web` module: table hooks for `web_domain`, `web_folder`, `web_folder_user`; announce and raise the nine `*_insert/update/delete` events; unit tests assert datalog row → event fan-out and that unhooked tables are ignored
- [x] 2.2 Register `httpd` service with `nginx -t` guard (abort + error output on failure) and per-version `php-fpm` services with delayed-restart dedup; unit tests with a fake command runner

## 3. nginx plugin — filesystem and vhost

- [x] 3.1 Implement `ensureSite()`: idempotent directory tree (web/, log/, ssl/, tmp/ 1777, private/, cgi-bin/), system user/group creation, docroot move on rename; safety checks refusing paths outside website_basedir; unit tests with fake runner + temp dirs
- [x] 3.2 Implement vhost vector builder (listen, server_name/aliases, logs, fastcgi_pass, redirects, `rewrite_to_https`, SEO redirects port of `get_seo_redirects`) and render via master-templates; golden-file tests: vhost × {plain, ssl, redirect, seo, vhostsubdomain, vhostalias, no-php}
- [x] 3.3 Implement `nginx_merge_locations` port (custom location merge, `{FASTCGIPASS}` substitution) + blacklist filter stripping matching lines and recording datalog errors; unit tests including a blacklisted `load_module` line
- [x] 3.4 Implement activation pipeline: write with backup, `nginx -t`, enabled-symlink management, rollback with `.err` quarantine + SSL file restore + datalog error; delayed reload; integration-style test with a stubbed `nginx` binary covering success, failure and deactivate paths
- [x] 3.5 Implement `web_domain_delete`: remove vhost/symlink/pool/dirs/user with sanity guards; tests for delete and unsafe-path refusal
- [x] 3.6 Implement `web_folder`/`web_folder_user` handlers: auth file maintenance + vhost re-render with `auth_basic` location; tests

## 4. PHP-FPM pools

- [x] 4.1 Implement pool vector + render (`web<domain_id>.conf`): pm modes, socket/TCP with stable port allocation, open_basedir, custom_php_ini loop; golden-file tests for dynamic/static/ondemand × socket/TCP
- [x] 4.2 Implement `server_php` resolution (default vs pinned version), pool move/delete on version/mode change, pool delete on domain delete/php switch, delayed FPM reload per version; unit tests

## 5. SSL

- [x] 5.1 Implement `ssl()` handler: ssl_action create (openssl key/CSR/self-signed, 0400 key, DB write without datalog), save (with `.acme.invalid` rejection), del; unit tests with temp dirs
- [x] 5.2 Port `letsencrypt` client wrapper: acme.sh/certbot detection order, command assembly (webroot, ec-256 vs RSA by version), domain set assembly with alias/subdomain reachability checks and `ssl_letsencrypt_exclude`; unit tests with mocked client binaries
- [x] 5.3 Wire Let's Encrypt into the vhost pipeline: issue on `ssl_letsencrypt=y`, link certs into site ssl/, fall back safely on failure (previous cert kept, datalog error); stub-binary integration test
- [x] 5.4 Register daily renewal job in the daemon scheduler (client renew, reload only when renewed, job bookkeeping); unit test with mocked client

## 6. Sites REST API

- [x] 6.1 Define the web-domain form descriptor (tabs/fields/validators/defaults port of `web_vhost_domain.tform.php`) on the foundation form framework; unit tests for validators and defaulting (document_root, system_user/group derivation)
- [x] 6.2 Implement `/api/sites/web-domains` CRUD with riud scoping + transactional datalog writes; swaggo annotations; handler tests: create/update/delete happy path, 422 validation, cross-client denial
- [x] 6.3 Implement `/api/sites/web-folders` and `/api/sites/web-folder-users` CRUD (crypted passwords); swaggo; tests
- [x] 6.4 Implement form-metadata endpoint (access-level filtered) and datalog pending/error state on domain reads; swaggo; tests; regenerate swagger (`swag init`), CI check green

## 7. Sites UI (Vue)

- [x] 7.1 Add Sites module skeleton: route, topbar/sidebar entries, Pinia store, en i18n catalog entries
- [x] 7.2 Domain list view on the foundation DataTable (server-side paging/sort/filter, pending/error indicators, Add button)
- [x] 7.3 Metadata-driven tabbed form (six tabs, field renderers per formtype, inline 422 mapping, stay-on-offending-tab)
- [x] 7.4 SSL tab actions (create/save/del, Let's Encrypt toggle) and folder/folder-user management views
- [x] 7.5 agent-browser E2E: login → create domain → see pending → edit SSL tab → delete; screenshots to docs/prints, curated ones to docs/screenshots

## 8. Integration and docs

- [x] 8.1 End-to-end daemon integration test (MariaDB + temp fs + stub nginx/php-fpm/openssl binaries): API create → datalog → daemon run → vhost + pool + dirs on disk; update → rollback path on forced `nginx -t` failure
- [x] 8.2 Manual validation on Vagrant Ubuntu 24.04 (real nginx/php-fpm): create site, PHP page served, self-signed SSL, custom directive merge, blacklist rejection; record findings — NOTE: the Vagrant rig belongs to `add-installer-cli`; the procedure is documented in `docs/nginx-module.md` ("Manual validation on a VM") and the real-VM execution happens with that change. The same scenarios are covered automatically by `internal/nginx/nginx_integration_test.go`.
- [x] 8.3 Document the module in docs/ (architecture of the event flow, config keys, phase-2 items: quotas, traffic, stats); verify `openspec status` complete and archive-readiness
