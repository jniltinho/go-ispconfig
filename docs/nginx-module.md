# Web module (nginx)

Go port of ISPConfig3's `web_module.inc.php`, `nginx_plugin.inc.php` and
`letsencrypt.inc.php` (OpenSpec change `add-web-nginx-module`). A change to a
`web_domain` row ends as a live, `nginx -t`-validated vhost with a PHP-FPM
pool, provisioned site directories and system user — or a clean rollback with
the error surfaced in the panel.

## Manual validation on a VM

The full Vagrant rig (Ubuntu 24.04 box, provisioning, real nginx/php-fpm)
belongs to the `add-installer-cli` change; the module-level validation below
is **documented here and executed there** once the installer can provision
the VM. Do not build an ad-hoc VM for this module.

Procedure (Ubuntu 24.04, root):

1. **Install dependencies**: `apt install nginx php8.3-fpm mariadb-server redis-server`
   (or point `config.toml` at external MariaDB/Redis). Optional for Let's
   Encrypt: acme.sh or certbot.
2. **Initialize**: copy the `go-ispconfig` binary, run `go-ispconfig init`,
   set the DB DSN and `[queue]` address in `config.toml`, then
   `go-ispconfig migrate` (note the printed admin password).
3. **Run**: `go-ispconfig serve` (panel/API) and `go-ispconfig daemon`
   (as root — it writes vhosts and runs useradd/nginx/php-fpm).
4. **Create a site via the panel**: Sites → Add website (type vhost,
   PHP-FPM). Watch the pending indicator clear after the daemon cycle.
5. **Verify on disk**: vhost in `/etc/nginx/sites-available/<domain>.vhost`,
   symlink in `sites-enabled/`, pool in `/etc/php/8.3/fpm/pool.d/web<id>.conf`,
   site tree under `/var/www/clients/client<cid>/web<id>/`.
6. **Serve a PHP page**: drop `<?php phpinfo();` into the site's `web/` dir
   and `curl -H "Host: <domain>" http://127.0.0.1/` — expect the page via
   the domain's php-fpm socket.
7. **Self-signed SSL**: SSL tab → create certificate; expect
   `ssl/<domain>.{key,csr,crt}` in the site dir and an https listen in the
   vhost; `curl -k https://<domain>/`.
8. **Custom directive merge**: add a custom `location` block in the Options
   tab and confirm it replaces/extends the template block in the rendered
   vhost.
9. **Blacklist rejection**: try `load_module ...;` as a custom directive —
   the API must reject it with a per-field validation error.
10. **Rollback**: force a broken directive into the DB (bypassing the API),
    trigger an update and confirm the previous vhost is restored, the broken
    render is quarantined as `.err` and the datalog row shows the
    `nginx -t` output.

The end-to-end behavior of steps 4–10 is already pinned by the automated
integration suite (`internal/nginx/nginx_integration_test.go`, run with
`go test -tags=integration ./internal/nginx/`) against docker MariaDB/Redis
with a faked OS seam; the VM pass validates the real nginx/php-fpm binaries.
