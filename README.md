# go-ispconfig

[![CI](https://github.com/jniltinho/go-ispconfig/actions/workflows/ci.yml/badge.svg)](https://github.com/jniltinho/go-ispconfig/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/jniltinho/go-ispconfig)](https://github.com/jniltinho/go-ispconfig/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

Port of the [ISPConfig3](https://www.ispconfig.org/) hosting control panel to Go —
a **single static binary** containing the web panel (Vue 3 + Tailwind, embedded),
the REST API (Echo v5 + Swagger), the configuration daemon and the installer CLI.
The panel is standalone: it serves HTTPS itself, no nginx/apache in front.

Phase 1 manages **nginx** (websites, PHP-FPM, SSL) and **Bind** (DNS zones).

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

Foundation implemented (see [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)):

- Cobra CLI: `serve`, `daemon`, `migrate`, `init`, `version`
- Identical DB schema (embedded DDL, adopt-existing-DB detection) + GORM models
- Auth: legacy crypt (`$6$`/`$1$`) + bcrypt, sessions, CSRF, brute-force lockout
- riud permission model (admin/reseller/client) as a single GORM scope
- sys_datalog engine: JSON diff writer, module/plugin registries, daemon with
  per-row crash recovery, delayed service restarts, remote actions
- asynq task queue: per-server queues, periodic jobs, instant datalog wake
- `.master` template engine (golden-file tested against nginx/bind templates)
- REST API with CRUD framework, tform-style validators, Swagger UI at `/swagger/`
- Vue 3 + Tailwind v4 panel skeleton (login, layout, DataTable, TabbedForm, i18n)

Next up: nginx and Bind modules, installer CLI, panel theme, legacy migration
wizard — the full plan is in [`docs/ROADMAP.md`](docs/ROADMAP.md).

## Quick start

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
./bin/go-ispconfig daemon    # config daemon (root, applies nginx/bind configs)
```

`migrate` prints the generated admin password once. Pointing the DSN at an
existing ISPConfig3 3.3.x database skips DDL and seeds nothing — your data is
adopted as-is ([`docs/MIGRATION.md`](docs/MIGRATION.md)).

TLS: set `server.tls_cert`/`server.tls_key` in `config.toml`. Automatic
self-signed certificate generation is on the roadmap (design D13).

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

Everything is specified as OpenSpec changes under
[`openspec/changes/`](openspec/changes/); implementation always follows the
corresponding change.

## License

MIT
