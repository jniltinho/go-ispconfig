# go-ispconfig

[![CI](https://github.com/jniltinho/go-ispconfig/actions/workflows/ci.yml/badge.svg)](https://github.com/jniltinho/go-ispconfig/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/jniltinho/go-ispconfig)](https://github.com/jniltinho/go-ispconfig/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

Port of the [ISPConfig3](https://www.ispconfig.org/) hosting control panel to Go —
a **single static binary** containing the web panel (Vue 3 + Tailwind, embedded),
the REST API (Echo v5 + Swagger), the configuration daemon and the installer CLI.
The panel is standalone: it serves HTTPS itself, no nginx/apache in front.

It manages websites (nginx + PHP-FPM + SSL), DNS (Bind or PowerDNS), mail
(Postfix/Dovecot/Rspamd), client databases, FTP/shell/jailkit accounts, cron
jobs, the firewall, clients/resellers and server monitoring — and installs
itself on Debian 11–13 / Ubuntu 22.04–24.04 with `go-ispconfig install`.

## Why

- **Database 100% identical to ISPConfig3.** The original `ispconfig3.sql` DDL
  is embedded in the binary; an existing `dbispconfig` database can be adopted
  directly. Migrating clients from a PHP ISPConfig3 install is a first-class
  feature — see [`docs/MIGRATION.md`](docs/MIGRATION.md).
- **Same architecture, modern runtime.** The proven ISPConfig design is kept —
  the panel never touches the OS; every change flows through `sys_datalog` and
  is applied by a daemon — but the PHP runtime, system cron jobs and pidfile
  locking are replaced by one supervised Go daemon with an internal scheduler
  and an [asynq](https://github.com/hibiken/asynq) task queue (Redis/Valkey).
- **Single binary.** Frontend, API, Swagger UI, SQL schema and `.master`
  config templates are all embedded. Deploy = copy one file.

## Status

Foundation (see [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)):

- Cobra CLI: `install`, `uninstall`, `serve`, `daemon`, `migrate`,
  `migrate-from`, `templates`, `init`, `version`
- Identical DB schema (embedded DDL, adopt-existing-DB detection) + GORM models
- Auth: legacy crypt (`$6$`/`$1$`) + bcrypt, sessions, CSRF, brute-force lockout
- riud permission model (admin/reseller/client) as a single GORM scope
- sys_datalog engine: JSON diff writer, module/plugin registries, daemon with
  per-row crash recovery, delayed service restarts, remote actions
- asynq task queue: per-server queues, periodic jobs, instant datalog wake
- `.master` template engine (golden-file tested against nginx/bind templates)
- REST API with CRUD framework, tform-style validators, Swagger UI at `/swagger/`
- Vue 3 + Tailwind v4 panel (login, layout, DataTable, TabbedForm, dashlets,
  Chart.js metrics, i18n), embedded in the binary

### Modules

| Module | Scope | Docs |
|---|---|---|
| Web (nginx) | vhosts from the ISPConfig `.master` templates gated by `nginx -t` with rollback, PHP-FPM pools per site, SSL incl. Let's Encrypt (acme.sh/certbot) + daily renewal, web folders/folder users | [nginx-module.md](docs/nginx-module.md) |
| DNS (Bind) | zones gated by `named-checkzone`, `named.conf.local` reconstruction, secondary zones, DNSSEC signing + daily re-sign, record editor for 18 types | [dns-module.md](docs/dns-module.md) |
| DNS (PowerDNS) | alternative backend, same UI/API, gmysql zone sync; picked at install time with `--dns-backend powerdns` | [powerdns-module.md](docs/powerdns-module.md) |
| Mail | Postfix/Dovecot maps, mailboxes, forwardings, transports, DKIM, Rspamd spamfilter and white/blacklists | [mail-module.md](docs/mail-module.md) |
| Database | client MySQL databases and users, remote-access grants, `mysql_clientdb.conf` | [database-module.md](docs/database-module.md) |
| FTP / shell | virtual PureFTPd accounts (MySQL auth, no OS users), shell users, jailkit chroots | [ftp-shell-module.md](docs/ftp-shell-module.md) |
| Cron | site cron jobs run by the daemon's own scheduler — no system crontab | [cron-module.md](docs/cron-module.md) |
| Firewall | per-server UFW rule sets | [firewall-module.md](docs/firewall-module.md) |
| Clients | clients/resellers, limits, templates, messaging | [client-module.md](docs/client-module.md) |
| Monitor | service/state/quota collectors, `monitor_data` history, dashboard dashlets and charts | [monitor-module.md](docs/monitor-module.md) |
| Installer | `go-ispconfig install` for Debian 11–13 / Ubuntu 22.04–24.04, idempotent step pipeline, Vagrant test rig | [install.md](docs/install.md) |
| Legacy migration | adopt an existing `dbispconfig`, or import from a running PHP ISPConfig3 over its remote API | [MIGRATION.md](docs/MIGRATION.md), [legacy-migration.md](docs/legacy-migration.md) |

Remaining scope (XMPP, VM, Apache2 backend, APS, mailing lists) is in
[`docs/ROADMAP.md`](docs/ROADMAP.md).

## Quick start

### On a server (Debian 11–13 / Ubuntu 22.04–24.04)

The installer brings its own dependencies (nginx, MariaDB, Redis, PHP-FPM,
Bind or PowerDNS, PureFTPd) and leaves two systemd units behind — no crontab
entries, all periodic work lives in the daemon:

```bash
go-ispconfig install --yes --hostname srv1.example.com \
  --admin-email admin@example.com --write-credentials
# panel on https://<host>:8080/ — admin password printed once
```

Flags, files touched and the full step list: [`docs/install.md`](docs/install.md).

### From source (development)

Requirements: **MariaDB** and **Redis or Valkey** reachable from the host
(the task queue degrades gracefully if Redis is down — datalog polling
continues), plus Go >= 1.26 and Node >= 22 to build.

```bash
docker run -d --name mariadb-ispconfig -e MARIADB_ROOT_PASSWORD=root -p 3306:3306 mariadb:11
docker run -d --name redis-ispconfig -p 6379:6379 redis:7-alpine

make all                 # build frontend (web/dist) + Go binary (bin/go-ispconfig)
./bin/go-ispconfig init      # generate config.toml (DB DSN, listen port, [queue])
./bin/go-ispconfig migrate   # create schema (embedded ispconfig3.sql) + seed admin
./bin/go-ispconfig serve     # panel + API + Swagger UI at /swagger/
./bin/go-ispconfig daemon    # config daemon (root, applies the module configs)

# Crank up logging without a rebuild (config: [log] level, default info)
LOG_LEVEL=debug ./bin/go-ispconfig serve
```

`migrate` prints the generated admin password once. Pointing the DSN at an
existing ISPConfig3 3.3.x database skips DDL and seeds nothing — your data is
adopted as-is ([`docs/MIGRATION.md`](docs/MIGRATION.md)).

TLS: `serve` generates a self-signed certificate under `<config-dir>/ssl/` on
first start; set `server.tls_cert`/`server.tls_key` in `config.toml` to use
your own.

## Development

See [`AGENTS.md`](AGENTS.md) for the full environment/build/test/validation
runbook. Highlights:

```bash
make lint            # golangci-lint (godoc on exported identifiers enforced)
make test            # go test ./internal/...
go test -tags=integration ./internal/...   # spins up throwaway MariaDB/Redis via docker
make swagger         # regenerate Swagger docs after changing handlers
cd frontend && npm run dev   # Vite dev server, /api proxied to :8080
```

Real VMs are one command away — the Vagrant rig installs the panel on
`https://192.168.56.10:8080` and keeps a standing PHP ISPConfig3 lab on
`https://192.168.56.20:8080` as the parity baseline (credentials are written
per-VM by `--write-credentials`; see [`vagrant/README.md`](vagrant/README.md)):

```bash
make vagrant-up && make vagrant-test      # install + E2E on Ubuntu 24.04
make vagrant-lab-up                       # standing legacy lab (parity source)
```

Every module is specified first: the consolidated capabilities live in
[`openspec/specs/`](openspec/specs/) and the changes that produced them are
archived under [`openspec/changes/archive/`](openspec/changes/archive/).
Implementation always follows the corresponding change.

## License

MIT
