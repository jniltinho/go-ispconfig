# Installing go-ispconfig

One binary installs and configures the whole stack (nginx + bind + MariaDB +
Redis, panel and daemon) on a clean **Debian 11/12/13** or **Ubuntu
22.04/24.04** host. Every step is idempotent: re-running `install` after a
failure converges to a complete installation.

## Quickstart

```sh
# as root, on a clean supported host
go-ispconfig install --yes
```

That is the whole install. At the end the summary prints the panel URL
(`https://<fqdn>:8080/`, self-signed certificate) and the generated `admin`
password — **shown exactly once**.

What it does, in order: preflight checks → OS packages (apt, noninteractive)
→ MariaDB database + `ispconfig` user + original ISPConfig3 schema →
server record + detected IPs → `/etc/go-ispconfig/config.toml` → panel user
→ self-signed TLS cert → nginx base include → bind base config → PowerDNS
database + `pdns.local` config (only with `--dns-backend powerdns`, skips
the bind-base step instead) → optional ACME client → systemd units
(`go-ispconfig-serve`, `go-ispconfig-daemon`) → admin seed → summary. No
crontab is ever touched: the daemon's internal scheduler owns all periodic
work.

## Flags and answers

Answers resolve as: **CLI flags > `--answers file.toml` > interactive
prompt > default**. Without `--yes` the installer prompts for anything not
provided; with `--yes` nothing is prompted and a missing required answer
aborts naming its flag.

| Flag | Default | Answer |
|------|---------|--------|
| `--hostname` | system hostname | server FQDN (panel URL, TLS cert CN) |
| `--panel-port` | `8080` | panel HTTPS port (>= 1024, the panel runs unprivileged) |
| `--db-name` | `dbispconfig` | ISPConfig database name |
| `--db-user` | `ispconfig` | ISPConfig database user |
| `--db-root-password` | *(empty)* | MariaDB root password — only when unix-socket auth is disabled |
| `--web` | `y` | configure nginx |
| `--dns` | `y` | configure the DNS server (backend below) |
| `--dns-backend` | `bind` | `bind` or `powerdns` — see [powerdns-module.md](powerdns-module.md) for the PowerDNS path (packages, database, `pdns.local`) |
| `--php-fpm` | `y` | install the distro php-fpm package for hosted sites |
| `--acme` | `n` | install an ACME client for site Let's Encrypt certs |
| `--acme-client` | `acme.sh` | `acme.sh` or `certbot` |
| `--admin-email` | *(empty)* | administrator email |

Control flags (not answers): `--yes`, `--answers <file>`, `--update`,
`--dry-run` (print the resolved plan without changes, works as any user),
`--write-credentials`.

### Answers file

`--answers install.toml` — keys are the flag names with `_` instead of `-`;
flags still override the file:

```toml
hostname    = "server1.example.com"
panel_port  = 8080
web         = "y"
dns         = "y"
dns_backend = "bind"
php_fpm     = "y"
acme        = "n"
admin_email = "admin@example.com"
```

```sh
go-ispconfig install --yes --answers install.toml
```

## Update mode

```sh
go-ispconfig install --update --yes
```

Re-renders only the base configs and systemd units (with the same
backup-before-overwrite and validate-before-reload rules), then restarts
the units. It **never** touches the database, `config.toml` credentials,
TLS certificates or the admin user. Use it after upgrading the binary.

## Uninstall

```sh
go-ispconfig uninstall              # asks you to type "uninstall"
go-ispconfig uninstall --yes        # no confirmation
go-ispconfig uninstall --keep-config   # keep /etc/go-ispconfig/
go-ispconfig uninstall --purge-db      # ALSO drop database + DB user
```

Stops, disables and removes the two units and the rendered configs. OS
packages (nginx, bind9, mariadb-server, …) are never removed — apt state
belongs to the admin. Without `--purge-db` the database survives, so a
later `install` re-adopts it with the existing data.

## Credentials

- The `admin` panel password is generated on first install and printed
  once in the summary. With `--write-credentials` it is also written
  root-only (0600) to `/root/.go-ispconfig-credentials` — delete that file
  after storing the password.
- Re-runs never regenerate or reprint the admin password.
- The DB password of the `ispconfig` user lives in the DSN inside
  `/etc/go-ispconfig/config.toml` (mode 0640, `root:go-ispconfig`);
  re-runs reuse it, never rotate it.
- Lost the admin password? Reset the bcrypt hash directly:
  `UPDATE sys_user SET passwort = '<bcrypt-hash>' WHERE username = 'admin';`
  (generate a hash with any bcrypt tool), or `uninstall --purge-db` +
  fresh `install` if the data is disposable.

## Backups made by the installer

Before overwriting any config file whose content differs, the installer
copies it to `<file>.bak-<unix-timestamp>` next to the original (identical
content = no write, no backup). Files this applies to:

- `/etc/go-ispconfig/config.toml`
- `/etc/systemd/system/go-ispconfig-serve.service`, `…-daemon.service`
- `/etc/nginx/conf.d/go-ispconfig-sites.conf` (only written when the
  distro nginx.conf does not already include `sites-enabled`)
- `/etc/bind/named.conf.options` (+ the local include)
- `/etc/powerdns/pdns.d/pdns.local` (only with `--dns-backend powerdns`)

nginx/bind configs are validated (`nginx -t`, `named-checkconf`) before any
reload; on validation failure the original file is restored and the step
fails loudly.

## ACME clients

`--acme y` installs **one client and no webserver plugin**: `acme.sh` through
its own installer script (pulling `curl` first if the box has none), or the
distro's `certbot` package. No `python3-certbot-nginx`, no
`python3-certbot-apache`.

Site certificates are issued with `--authenticator webroot`, which needs no
plugin — and neither does the legacy, checked against the ISPConfig `3.2dev`
tree in `base/ispconfig3_install/`:

- the PHP installer **detects** a client rather than apt-installing one, and
  only falls back to installing acme.sh when neither is present
  (`installer_base.lib.php:3183`);
- the panel certificate is issued with
  `certonly … --authenticator webroot --webroot-path /usr/local/ispconfig/interface/acme`,
  dropping to `--standalone` only on a host with no webserver
  (`installer_base.lib.php:3464`);
- site certificates take the same shared path — `letsencrypt.inc.php` builds
  `certonly … --authenticator webroot --webroot-map {…}`, called by both the
  apache2 and nginx plugins.

Webroot is the right authenticator for a site the panel manages: a plugin
writes its own `ssl_certificate` and redirect directives into the vhost, and
those are exactly the lines the next apply overwrites, because vhosts are
rendered from templates. Webroot only drops a challenge file under
`/usr/local/ispconfig/interface/acme` and touches no config.

The plugin would not help with the panel's own certificate either:
`go-ispconfig-serve` terminates TLS itself on port 8080 and reads the cert from
`/etc/go-ispconfig/`, with no nginx or Apache vhost in front of it, so
`certbot --nginx` has nothing to edit there. To replace the self-signed panel
certificate, issue one however you like (`certbot certonly --webroot` works)
and point `[server] tls_cert` / `tls_key` at it. If you maintain hand-written
vhosts on the box and want `certbot --nginx` for those, install the plugin
yourself — it is your certbot, outside what the panel manages.

**Known gap:** issuance lives in `internal/nginx` only. Apache sites get
renewal (`internal/apache2/le_renew.go` over the shared `internal/web` helper)
but cannot *issue* from the panel yet — issue once by hand on an Apache host
and the renewal job takes over. `openspec/changes/acme-as-go/` proposes
replacing both paths with an in-process client.

## Deferred installer capabilities (other modules)

The following configure steps are **not** part of the current
`go-ispconfig install` pipeline. They are tracked as **Modified
Capabilities of `add-installer-cli`** and will land when those modules
are co-scheduled with an installer update:

| Capability | Owning module | Notes |
|------------|---------------|-------|
| `configure_jailkit` | [`add-ftp-shell-module`](ftp-shell-module.md) | jailkit package + base chroot sections; daemon plugins already use server `[jailkit]` getconf |
| `configure_ufw` / firewall package defaults | `add-firewall-module` | UFW apply already in the daemon; package install is installer scope |
| PureFTPd TLS (FTPS) | [`add-ftp-shell-module`](ftp-shell-module.md) | the PHP installer optionally symlinks the panel cert to `/etc/ssl/private/pure-ftpd.pem` and writes `conf/TLS`; plain FTP works without it |

Until those steps ship, operators who need jailkit on a fresh host must
install it themselves (see [ftp-shell-module.md](ftp-shell-module.md)).
Cross-link: [ROADMAP.md](ROADMAP.md).

## Testing the installer (developers)

A real-VM Vagrant rig proves the whole cycle end to end — see
[`vagrant/README.md`](../vagrant/README.md):

```sh
make vagrant-up && make vagrant-test    # Ubuntu 24.04 E2E + smoke test
make vagrant-up VM=debian && make vagrant-test VM=debian   # Debian 12
```
