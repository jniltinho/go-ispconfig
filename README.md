# go-ispconfig

Port of the [ISPConfig3](https://www.ispconfig.org/) hosting control panel to Go —
a single static binary containing the web panel (Vue 3 + Tailwind, embedded), REST
API (Echo v5 + Swagger), the configuration daemon and the installer CLI.

**Status: specification phase.** All modules are specified as OpenSpec changes
under [`openspec/changes/`](openspec/changes/); implementation is starting with
the foundation. See [`docs/ROADMAP.md`](docs/ROADMAP.md).

## Highlights

- **Database 100% identical to ISPConfig3** — an existing `dbispconfig` can be
  adopted directly; client migration is a first-class feature
  ([`docs/MIGRATION.md`](docs/MIGRATION.md)).
- Initial modules: **nginx** (websites, PHP-FPM, SSL/Let's Encrypt) and
  **Bind** (DNS zones, DNSSEC).
- Modern daemon with an internal scheduler — replaces all ISPConfig cron jobs.
- Targets Debian 11–13 and Ubuntu 22.04–24.04. Install testing via Vagrant.

## Development

See [`AGENTS.md`](AGENTS.md) for the full environment/build/test runbook.

```bash
make all                 # build frontend + binary
./go-ispconfig init      # generate config.toml
./go-ispconfig migrate   # create schema (embedded ispconfig3.sql) + seed
./go-ispconfig serve     # panel + API + swagger at /swagger/
./go-ispconfig daemon    # config daemon (requires root)
```

## License

MIT
