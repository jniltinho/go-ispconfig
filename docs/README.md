# go-ispconfig — Technical Documentation

The deep dive. For the "should I use this?" pitch, see the
[top-level README](../README.md).

- [What this is](#what-this-is)
- [Architecture overview](#architecture-overview)
- [Module status](#module-status)
- [Build from source](#build-from-source)
- [Install](#install)
- [Lab VMs](#lab-vms)
- [API](#api)
- [Daemon](#daemon)
- [Migration from PHP ISPConfig3](#migration-from-php-ispconfig3)
- [Failure modes](#failure-modes)
- [Configuration](#configuration)
- [Operations](#operations)
- [Testing](#testing)
- [Security](#security)
- [Releases](#releases)
- [Contributing](#contributing)
- [License](#license)

## What this is

go-ispconfig is a port of the [ISPConfig3](https://www.ispconfig.org/) hosting
control panel to Go, distributed as one static binary. That binary contains the
web panel (Vue 3 + Tailwind v4, embedded as `web/dist`), the REST API
(Echo v5 + Swagger), the `sys_datalog` config daemon and the installer CLI. It
serves HTTPS itself — there is no reverse proxy in front of it.

The database schema is ISPConfig3's, unchanged: the original `ispconfig3.sql`
DDL is embedded in the binary, all 78 tables are mapped to GORM structs, and an
existing `dbispconfig` database is adopted rather than converted. The
architecture is ISPConfig3's too — the panel writes JSON diffs to `sys_datalog`
and a privileged daemon applies them to the OS. What was replaced is the
runtime: PHP-FPM, system cron jobs and pidfile locking became one supervised Go
process with an internal scheduler and an
[asynq](https://github.com/hibiken/asynq) task queue on Redis/Valkey.

## Architecture overview

```
   Browser / API client
          │  HTTPS :8080 (session cookie + CSRF, or bearer token)
          ▼
   ┌──────────────────────────────────────────────┐
   │ Panel process  (go-ispconfig serve)          │  unprivileged
   │  Echo router → CRUD framework → validators   │  (user: go-ispconfig)
   │  GORM + riud permission scope                │
   │  embedded SPA + Swagger UI                   │
   └──────────────┬───────────────────────────────┘
                  │ INSERT sys_datalog (table, action, JSON diff, server_id)
                  ▼
          ┌───────────────────┐
          │  MariaDB          │  ◄── the single source of truth
          │  dbispconfig      │
          └────────┬──────────┘
                   │ polled every tick_seconds, plus instant wake via asynq
                   ▼
   ┌──────────────────────────────────────────────┐
   │ Daemon process (go-ispconfig daemon)         │  root
   │  modules   → table hooks, announce events    │
   │  plugins   → subscribe events, touch the OS  │
   │  scheduler → daily/nightly jobs, no crontab  │
   └──────────────┬───────────────────────────────┘
                  │ render .master template → validate → reload service
                  ▼
   nginx · apache2 · bind · powerdns · postfix · dovecot · rspamd
   pure-ftpd · jailkit · fail2ban · ufw · mariadb (client DBs)
```

Two properties carry most of the design:

**Nothing is applied directly.** A form submit is a database write, nothing
more. If the daemon is stopped the row sits in `sys_datalog` until it starts
again — no change is lost, and no half-applied state exists.

**Validation gates the reload.** Templates render to a staging path, the
service's own checker runs (`nginx -t`, `apachectl configtest`,
`named-checkzone`, `postconf`), and only a passing check promotes the file and
schedules a reload. A failing check restores the previous file and marks the
datalog row with its error.

The engine internals — registry, event announcement, per-row crash recovery,
delayed service restarts, remote actions, the `.master` template system —
are documented in [ARCHITECTURE.md](ARCHITECTURE.md).

## Module status

Daemon modules consume `sys_datalog` rows; installer steps only run during
`install`.

| Module | Status | Scope | Docs |
|---|---|---|---|
| `web` (nginx) | stable | vhosts, PHP-FPM pools, aliases/subdomains, redirects, web folders, SSL + Let's Encrypt | [nginx-module.md](nginx-module.md) |
| `web` (apache2) | stable | same surface on apache2; picked with `--web-server apache2` | [nginx-module.md](nginx-module.md) |
| `dns` (bind) | stable | zones gated by `named-checkzone`, `named.conf.local` reconstruction, secondaries, DNSSEC + daily re-sign | [dns-module.md](dns-module.md) |
| `dns` (powerdns) | stable | same UI/API, gmysql zone sync, AXFR allow-list, `pdnsutil`; `--dns-backend powerdns` | [powerdns-module.md](powerdns-module.md) |
| `mail` | stable | domains, mailboxes, aliases, forwardings, catchall, transports, relay, DKIM, quotas | [mail-module.md](mail-module.md) |
| `rspamd` | stable | spam policy API, global `actions.conf`, per-domain settings, white/blacklists, DKIM signing | [mail-module.md](mail-module.md) |
| `database` | stable | client MySQL databases and users, remote-access grants, `mysql_clientdb.conf` | [database-module.md](database-module.md) |
| `ftp` / `shell` | stable | virtual PureFTPd accounts (MySQL auth, no OS users), shell users, jailkit chroots | [ftp-shell-module.md](ftp-shell-module.md) |
| `cron` | stable | site cron jobs run by the daemon scheduler — no system crontab | [cron-module.md](cron-module.md) |
| `firewall` | stable | per-server UFW rule sets | [firewall-module.md](firewall-module.md) |
| `fail2ban` | stable | panel-managed jails, live ban view, unban action, orphan pruning | — |
| `client` | stable | clients/resellers, limits, client templates, messaging | [client-module.md](client-module.md) |
| `monitor` | stable | service/state/quota collectors, `monitor_data` history, dashlets and charts | [monitor-module.md](monitor-module.md) |
| `getmail` | stable | fetch external POP3/IMAP accounts via systemd timer | [mail-module.md](mail-module.md) |
| multi-server | partial | per-`server_id` datalog routing, role-gated module loading and node identity all work; setup is manual (no `install --join`, no master/slave DB split, no mirrors) | [multi-server.md](multi-server.md) |
| API tokens | stable | scoped, revocable machine credentials for automation, optional JWT exchange | [api-tokens.md](api-tokens.md) |
| legacy migration | stable | adopt an existing `dbispconfig`, or import over the PHP panel's remote API | [MIGRATION.md](MIGRATION.md), [legacy-migration.md](legacy-migration.md) |
| `postfix` (installer) | stable | idempotent convergence via `postconf -e/-M/-P`, submission + submissions, virtual maps | [mail-module.md](mail-module.md) |
| `dovecot` (installer) | stable | IMAP/POP3/LMTP, dual-dialect config (2.3 and 2.4 detected at runtime), SQL auth | [mail-module.md](mail-module.md) |
| XMPP | planned | `xmpp_domain` / `xmpp_user` — tables exist, no module | [ROADMAP.md](ROADMAP.md) |
| VM (OpenVZ) | planned | `openvz_*` — tables exist, no module | [ROADMAP.md](ROADMAP.md) |
| APS installer | planned | `aps_*` — tables exist, no module | [ROADMAP.md](ROADMAP.md) |
| Mailing lists | planned | `mail_mailinglist` — table exists, no module | [ROADMAP.md](ROADMAP.md) |
| MyDNS | not planned | legacy backend; tables kept for schema compatibility | [ROADMAP.md](ROADMAP.md) |

## Build from source

Requirements: Go >= 1.26, Node >= 22, and (to run it) MariaDB plus Redis or
Valkey reachable from the host.

```bash
make all          # clean + frontend + build
```

That runs three things:

| Step | Output |
|---|---|
| `make frontend` | `frontend/` built by Vite into `web/dist/` — embedded with `go:embed` |
| `make build` | `bin/go-ispconfig` for the host platform, version stamped via `-ldflags` |
| `make build-linux` | `bin/go-ispconfig-linux-amd64`, `CGO_ENABLED=0`, used by the Vagrant rig |

The main package is the repository root, not `cmd/` — `cmd/` holds the Cobra
subcommand definitions. Packaging targets: `make deb`, `make rpm`.

Local run against throwaway services:

```bash
docker run -d --name mariadb-ispconfig -e MARIADB_ROOT_PASSWORD=root -p 3306:3306 mariadb:11
docker run -d --name redis-ispconfig -p 6379:6379 redis:7-alpine

./bin/go-ispconfig init      # write config.toml
./bin/go-ispconfig migrate   # create schema from embedded DDL + seed admin
./bin/go-ispconfig serve     # panel + API + Swagger
sudo ./bin/go-ispconfig daemon

LOG_LEVEL=debug ./bin/go-ispconfig serve   # verbosity without a rebuild
```

`migrate` prints the generated admin password once. Against an existing
ISPConfig3 3.3.x database it validates the schema, skips the DDL and seeds
nothing.

## Install

```bash
go-ispconfig install --yes \
  --hostname srv1.example.com \
  --admin-email admin@example.com \
  --web-server apache2 \
  --dns-backend powerdns \
  --write-credentials
```

Without `--yes` the installer prompts for each answer (hostname, admin email,
MariaDB root password, web server, DNS backend) and echoes the plan before
touching anything. `--write-credentials` drops the generated admin password
into a root-only file instead of only printing it.

The pipeline is 21 idempotent steps, in order — re-running converges rather
than reinstalls:

| # | Step | What it does |
|---|---|---|
| 1 | `preflight` | distro/version check, root check, port conflicts, existing install detection |
| 2 | `packages` | apt install of everything the chosen roles need |
| 3 | `mariadb` | secure the server, create `dbispconfig`, run the embedded DDL, seed admin |
| 4 | `server-ips` | detect and register this node's addresses in `server_ip` |
| 5 | `panel-user` | create the unprivileged `go-ispconfig` user and the `sshusers` group |
| 6 | `config-toml` | write `/etc/go-ispconfig/config.toml` with the resolved DSN and roles |
| 7 | `tls-cert` | 10-year self-signed pair under `/etc/go-ispconfig/ssl/` if none configured |
| 8 | `nginx-base` | base nginx config and the panel's own site (skipped on apache2 nodes) |
| 9 | `apache2` | base apache2 config, modules, PHP-FPM wiring (skipped on nginx nodes) |
| 10 | `bind-base` | `named.conf` skeleton, ACLs, zone directory (skipped on powerdns nodes) |
| 11 | `powerdns` | gmysql schema and backend config (skipped on bind nodes) |
| 12 | `pure-ftpd` | MySQL auth backend, TLS, passive port range |
| 13 | `fail2ban` | jail set (sshd, dovecot, postfix, pure-ftpd, apache-auth), visudo staging |
| 14 | `vmail` | `vmail` system user/group so maildirs are never root-owned |
| 15 | `postfix` | `postconf -e/-M/-P` convergence, virtual maps, submission/submissions |
| 16 | `dovecot` | dialect detection (2.3 vs 2.4), SQL auth, LMTP + auth sockets in Postfix's chroot |
| 17 | `rspamd` | milter wiring, DKIM path, global actions |
| 18 | `getmail` | fetch user, spool, systemd timer |
| 19 | `install-acme` | acme.sh or certbot for Let's Encrypt |
| 20 | `systemd-units` | `go-ispconfig-serve.service` and `go-ispconfig-daemon.service`, enabled |
| 21 | `summary` | print URL, admin credentials and what was skipped |

Mail steps (14–18) run only on nodes whose server row has `mail_server = 1`;
web steps only with `web_server = 1`, and so on. `install --update` re-runs a
five-step subset (`preflight`, `nginx-base`, `apache2`, `bind-base`,
`systemd-units`) and never touches the database, config, certificates or admin
seed. Pass `--web-server` again on update — the choice is not persisted in
`config.toml`.

Full flag list and every file written: [install.md](install.md).

## Lab VMs

`vagrant/Vagrantfile` defines five VMs on the `192.168.56.0/24` host-only
network — three running go-ispconfig, two running stock PHP ISPConfig3 as the
parity baseline:

| VM | Box | IP | Role |
|---|---|---|---|
| `ubuntu` (default) | bento/ubuntu-24.04 | 192.168.56.10 | go-ispconfig, nginx |
| `debian` | bento/debian-12 | 192.168.56.11 | go-ispconfig, nginx |
| `apache-test` | bento/ubuntu-24.04 | 192.168.56.22 | go-ispconfig, apache2 |
| `legacy` | bento/ubuntu-24.04 | 192.168.56.20 | PHP ISPConfig3, nginx, full mail stack + Roundcube |
| `legacy-apache` | bento/ubuntu-24.04 | 192.168.56.21 | PHP ISPConfig3, apache2, full stack |

```bash
make vagrant-up            # build linux binary + boot the default VM and install
make vagrant-test          # smoke test against 192.168.56.10
make vagrant-lab-up        # boot the legacy PHP baseline
make vagrant-lab-fixtures  # seed the legacy lab with clients/sites/mail
make vagrant-parity-test   # compare Go panel against the PHP panel
make vagrant-destroy
```

Per-VM admin credentials are written by `--write-credentials`; see
[`vagrant/README.md`](../vagrant/README.md).

## API

The REST API is mounted under `/api`, served by the same process as the panel.
Swagger UI is at `/swagger/`.

| Auth transport | How | CSRF |
|---|---|---|
| Session cookie | `POST /api/login`, cookie set `HttpOnly` + `Secure` | Required — `X-CSRF-Token` on every mutating request |
| Bearer token | `Authorization: Bearer <session id>` | Not required |

Everything is scoped by the riud permission model (admin / reseller / client)
applied as a single GORM scope, so a client session physically cannot read
another client's rows — the filter is on the query, not on the handler.

```bash
curl -sk https://localhost:8080/healthz
curl -sk 'https://localhost:8080/api/health?full=1' | jq
```

| Endpoint | Purpose |
|---|---|
| `GET /healthz` | Liveness for load balancers: plain-text `ok`, touches no dependency |
| `GET /api/health` | Status, build identity, uptime |
| `GET /api/health?full=1` | Probes database, task queue, TLS expiry and daemon datalog backlog |

Both are unauthenticated. `?full=1` returns `503` only when the database is
unreachable; a failing queue, an expiring certificate or a stalled daemon
report `"status": "degraded"` with `200`, so a degraded node stays in the pool
instead of dropping out of it.

Swagger is admin-gated by default. The `[swagger]` config section can move it
(`path`), open it to anonymous requests (`public` — development only) or drop
the route entirely (`disabled`). Regenerate the spec after changing handlers
with `make swagger`; CI enforces freshness with `make swagger-check`.

## Daemon

`go-ispconfig daemon` runs as root and is the only component that touches the
operating system. Each cycle (`daemon.tick_seconds`, default 10s, plus an
instant asynq wake when the panel writes):

1. Read unprocessed `sys_datalog` rows for this node's `server_id`.
2. For each row, call the registered table hooks — the module layer.
3. Modules announce events; plugins subscribed to those events render
   templates, validate and reload services.
4. Mark the row processed, or record the error and continue. One bad row never
   stalls the queue.
5. Service restarts requested during the cycle are coalesced and executed once
   at the end.

Inspecting it:

```bash
systemctl status go-ispconfig-daemon
journalctl -u go-ispconfig-daemon -f

# backlog: rows written but not yet applied on this node
mysql dbispconfig -e "SELECT datalog_id, dbtable, action, tstamp
  FROM sys_datalog WHERE datalog_id > (SELECT updated FROM server WHERE server_id=1)
  ORDER BY datalog_id DESC LIMIT 20;"
```

The scheduler replaces every ISPConfig cron job: SSL renewal, DNSSEC re-sign,
monitoring collection, traffic aggregation, getmail fetches, and a daily
`sys_datalog` prune at 03:00 (`daemon.datalog_retention_days`, default 30).

`server_id` is resolved from `config.toml`; `0` auto-detects by hostname, or
falls back to the single active server row. Set it explicitly on multi-server
installs — it decides which datalog rows this node applies.

## Migration from PHP ISPConfig3

**Adopt in place.** Point `database.dsn` at an existing `dbispconfig` 3.3.x
database. `migrate` validates the schema, skips the DDL and seeds nothing. The
existing password hashes (`$1$`, `$6$` crypt) verify natively;
`auth.rehash_legacy` upgrades them to bcrypt on login — keep it `false` until
the cutover is final, because PHP cannot verify bcrypt and enabling it burns
the rollback path.

**Import over the wire.** `migrate-from` pulls clients, sites and DNS from a
running PHP panel through its remote API:

```bash
go-ispconfig migrate-from --url https://legacy:8080 --user remote --dry-run
go-ispconfig migrate-from --url https://legacy:8080 --user remote \
  --only clients,sites,dns --reset-passwords
```

| Flag | Effect |
|---|---|
| `--dry-run` | Build and print the plan; write nothing |
| `--only` | Subset to import (default `clients,sites,dns`) |
| `--reset-passwords` | Issue one-time reset tokens for every recreated panel user |
| `--map-all-to-local-server` | Confirm collapsing a multi-server legacy panel onto this node |
| `--assign-orphan-zones-to-admin` | Give zones with a missing owner to admin instead of failing |
| `--insecure` | Skip TLS verification (loudly echoed in the report) |

The password is prompted when `--password` is omitted, keeping it out of shell
history. Walkthrough with screenshots: [legacy-migration.md](legacy-migration.md).

## Failure modes

| What breaks | What happens |
|---|---|
| **Database down** | Panel returns 503 on `/api/health?full=1` and errors on requests. The daemon logs and retries next tick. No config is applied from stale state. |
| **Redis/Valkey down** | Instant wake stops; the daemon falls back to tick polling. `sys_datalog` is the source of truth, so nothing is lost — only latency increases. Scheduled jobs still run. |
| **Daemon stopped or crashed** | Panel keeps accepting changes; rows accumulate in `sys_datalog` and apply in order on restart. Per-row recovery means a crash mid-row re-processes that row, not the whole backlog. |
| **Bad template / invalid config** | The service's own validator rejects it before promotion. The previous file is restored, the service is not reloaded, and the error is recorded on the datalog row. |
| **TLS certificate expired** | `?full=1` reports `degraded` with `200`. A self-signed pair is regenerated only if no explicit `tls_cert`/`tls_key` is configured; an invalid explicit pair is a startup error, never silently replaced. |
| **Panel process down** | Nothing is applied and nothing is lost — the daemon continues applying whatever is already in `sys_datalog`. |

## Configuration

`config.toml` holds only static process configuration. Runtime server behavior
(per-server web/dns/mail settings) lives in the database and is edited from the
panel. Search order: `--config` → `./config.toml` → `/etc/go-ispconfig/config.toml`.
Every key can be overridden by environment with the `GOISP_` prefix
(`GOISP_SERVER_PORT=9000`).

| Section | Keys |
|---|---|
| *(root)* | `server_id` — this node's row in `server`; `0` = auto-detect |
| `[server]` | `host`, `port`, `https`, `tls_cert`, `tls_key`, `trusted_proxies` |
| `[database]` | `dsn`, `clientdb_conf` (root-only credentials file for client DB provisioning) |
| `[daemon]` | `tick_seconds`, `datalog_retention_days`, `disable_*_module` gates |
| `[queue]` | `addr`, `db`, `password` — Redis/Valkey for asynq |
| `[templates]` | `custom_dir` — checked before the embedded `.master` templates |
| `[auth]` | `rehash_legacy` — upgrade legacy crypt hashes to bcrypt on login |
| `[powerdns]` | `dsn` — gmysql override; empty reuses the `[database]` host with db `powerdns` |
| `[swagger]` | `disabled`, `public`, `path` |
| `[log]` | `level` — `panic|error|warn|info|debug`; `LOG_LEVEL` overrides at runtime |

Annotated reference: [`config.toml.example`](../config.toml.example).

Template overrides follow ISPConfig3's `conf-custom` convention — a file in
`templates.custom_dir` with the same name shadows the embedded one:

```bash
go-ispconfig templates list           # embedded templates, custom overrides marked
go-ispconfig templates export --all   # copy originals into the custom dir to edit
```

## Operations

**Backup.** Two things matter: the database and `/etc/go-ispconfig/`.

```bash
mysqldump --single-transaction --routines dbispconfig | zstd > dbispconfig.sql.zst
tar czf go-ispconfig-etc.tar.gz /etc/go-ispconfig   # config.toml, ssl/, templates-custom/
```

Site content (`/var/www`), maildirs (`/var/vmail`) and zone files are managed
by the daemon but not owned by it — back them up as you would on any host.
Zones and vhosts can be regenerated from the database; mail and web data cannot.

**Restore.** Restore the dump, restore `/etc/go-ispconfig/`, then
`go-ispconfig install --update` to re-render base configs and units. Anything
in `sys_datalog` that never applied applies on daemon start.

**Upgrade.** Install the new package, then:

```bash
dpkg -i go-ispconfig_<new>_amd64.deb
go-ispconfig migrate                  # applies schema changes; no-op when there are none
systemctl restart go-ispconfig-serve go-ispconfig-daemon
```

Downgrades are safe within a minor series. Across a schema change, restore the
dump first.

## Testing

```bash
make test                                   # go test ./internal/...
make test-race
make lint                                   # golangci-lint (godoc on exports enforced)
make swagger-check                          # fails if the committed spec is stale
go test -tags=integration ./internal/...    # throwaway MariaDB/Redis via docker

cd frontend && npm run type-check && npm test && npm run build
```

E2E suites drive a real panel over HTTP — set `PANEL_URL` and `ADMIN_PASSWORD`,
or run them against a Vagrant VM:

| Target | Covers |
|---|---|
| `make e2e-theme` | Login, layout, dark mode, topnav parity |
| `make e2e-clients` | Client and reseller CRUD, limits, templates |
| `make e2e-mail` | Domains, mailboxes, forwardings, DKIM, spam policy |
| `make e2e-firewall` | UFW rule sets |
| `make e2e-cron` | Site cron jobs (`CRON_SQL=` to point at the DB) |
| `make e2e-database` | Client databases and users |
| `make e2e-ftp-shell` | FTP accounts, shell users, jailkit |
| `make e2e-dns-powerdns` | PowerDNS zone sync |
| `make e2e-ui-qa` | UI baseline sweep + cron regression |

Parity against the PHP panel: `make vagrant-parity-test`. Findings live in
[ui-qa-checklist.md](ui-qa-checklist.md) and [ui-qa-inventory.md](ui-qa-inventory.md).

## Security

| Control | Implementation |
|---|---|
| **Passwords** | bcrypt for new hashes; legacy ISPConfig crypt (`$1$`, `$6$`) verified natively, optionally rehashed on login |
| **Sessions** | Server-side in `sys_session`, `HttpOnly` + `Secure` cookie; bearer transport uses the same session id |
| **CSRF** | Token required on every mutating cookie-authenticated request; bearer requests are exempt by construction |
| **Brute force** | Per-IP and per-user login lockout; behind a proxy, the client IP is only trusted from `server.trusted_proxies` |
| **Authorization** | riud model (admin/reseller/client) enforced as a GORM query scope, not per-handler checks |
| **Audit** | `sys_log` records datalog events and their apply results; `sys_datalog` itself is an append-only change history |
| **Privilege split** | Panel runs as the unprivileged `go-ispconfig` user; only the daemon is root |
| **Client DB provisioning** | Uses a dedicated MySQL admin account from a root-only `mysql_clientdb.conf` — never the MySQL root account |
| **Transport** | HTTPS by default; a self-signed pair is generated only when no certificate is configured |
| **Swagger** | Admin-gated by default — the spec enumerates the whole attack surface |

## Releases

Tagged releases publish a tarball, a `.deb` and an `.rpm` for linux/amd64:
[github.com/jniltinho/go-ispconfig/releases](https://github.com/jniltinho/go-ispconfig/releases).
Version history is in [CHANGELOG.md](../CHANGELOG.md).

## Contributing

Every module is specified before it is written. Proposals live in
[`openspec/changes/`](../openspec/changes/), consolidated capabilities in
[`openspec/specs/`](../openspec/specs/), and implementation always follows the
corresponding change.

[AGENTS.md](../AGENTS.md) is the developer runbook: environment setup, build
and test commands, and the validation each change must pass before it can be
merged. Read it first.

## License

MIT — see [LICENSE](../LICENSE). go-ispconfig is an independent
reimplementation and is not affiliated with or endorsed by the ISPConfig
project.
