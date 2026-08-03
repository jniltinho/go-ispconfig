# DNS module — PowerDNS backend

Go port of ISPConfig3's `powerdns_plugin.inc.php` and
`installer_base.lib.php::configure_powerdns` (OpenSpec change
`add-dns-powerdns-module`). PowerDNS is an **alternative applying backend**
for the DNS module documented in [dns-module.md](dns-module.md): the panel
screens, the REST API, the `dns_soa`/`dns_rr`/`dns_slave`/`dns_template`
tables, the datalog events and the zone wizard are exactly the same —
only the daemon plugin that turns those events into a running DNS server
differs. Read dns-module.md first; this document covers only what is
different on the PowerDNS path.

## Backend selection

Exactly one applying plugin runs per server, chosen by the `dns_backend`
key of the `[dns]` section of `server.config` (getconf):

| `dns_backend` | Daemon loads |
|---|---|
| `bind` (default, empty, or unrecognized) | `internal/dns.Plugin` + `bind` service |
| `powerdns` | `internal/powerdns.Plugin` + `powerdns` service |

`cmd/daemon.go` (`dnsWiringFor`) enforces mutual exclusivity: on the
PowerDNS path the Bind plugin is never loaded, the `bind` service is never
registered, and the Bind-only `dns_resign` scheduler job (periodic
re-signing) is never registered either — PowerDNS keeps its keys and does
online signing itself (see [DNSSEC](#dnssec) below). The panel UI does not
choose the backend per zone; operators set it once per server, same as the
PHP original's install-time "which DNS software is installed" choice.

## Dual-database layout

Panel rows (`dns_soa`, `dns_rr`, `dns_slave`, `dns_template`) always live
in the ISPConfig database and are never altered by this backend — the
plugin only reads them. PowerDNS's own zone data lives in a **second**
MariaDB database, name fixed as `powerdns` (like the PHP original),
holding the tables from the embedded `powerdns.sql` (gmysql backend
schema): `domains`, `records`, `supermasters`, `domainmetadata`. `domains`
and `records` carry an extra `ispconfig_id` bridge column the plugin uses
to upsert/delete by origin ID (`dns_soa.id`/`dns_slave.id`) or record ID
(`dns_rr.id`); the SOA row of a zone reuses the zone's own id in that
column (same as PHP).

The daemon opens a dedicated GORM connection for this database: same
host/user/password as the panel DSN with the database swapped to
`powerdns` (`internal/powerdns.DeriveDSN`), or an explicit override via
`config.toml`:

```toml
[powerdns]
dsn = "ispconfig:secret@tcp(127.0.0.1:3306)/powerdns?parseTime=true"
```

If `dns_backend=powerdns` and this database is unreachable, the daemon
fails to start naming the database (fail loud, not partial).

## Event → SQL mapping

The plugin subscribes to the same nine events as the Bind plugin
(`dns_soa_insert/update/delete`, `dns_rr_insert/update/delete`,
`dns_slave_insert/update/delete`) and translates them into `domains`/
`records` writes instead of zone files:

- **SOA insert/update** (active only): upserts a `MASTER` domain row plus
  its `SOA` record (`content = "<ns> <hostmaster> <serial> <refresh>
  <retry> <expire> <minimum>"`); an inactive→active transition re-imports
  every active `dns_rr` of the zone.
- **SOA delete**: deletes the domain and all its records.
- **RR insert/update/delete** (active only): name/content follow the same
  absolute/relative-dot and HINFO-quoting rules as Bind's zone renderer;
  duplicate `ispconfig_id` and missing parent domain (SOA not active yet)
  are silent no-ops; the SOA record is never touched by the RR path.
- **Slave insert/update/delete**: `SLAVE` domain rows (`master =
  dns_slave.ns`); an update purges AXFR-cached records
  (`ispconfig_id = 0`) before re-fetching.

After a write, the plugin best-effort shells out (non-fatal on missing
binaries): `pdns_control rediscover` + `notify` after SOA changes,
`pdns_control retrieve` after slave changes, `pdnsutil|pdnssec
rectify-zone` after SOA/RR changes. Tool discovery prefers `pdnssec`
(PowerDNS 3.x) then falls back to `pdnsutil` (4.x); zone-maintenance
commands are gated on a supported major version (3 or 4).

## DNSSEC

Diverges from Bind by design — no `dnssec-keygen`/`dnssec-signzone` files
on disk, no periodic re-sign job. Instead, `dns_soa.dnssec_wanted`
transitions drive `pdnsutil`/`pdnssec` directly (ported
`handle_dnssec`/`soa_dnssec_*`):

| Transition | Action |
|---|---|
| N/null → Y | `add-zone-key ksk active 2048 rsasha256` + `add-zone-key zsk active 1024 rsasha256` + `set-nsec3 "1 0 10 deadbeef"`, then `show-zone` parsed into `dns_soa.dnssec_info`, `dnssec_initialized='Y'` |
| Y → N | `disable-dnssec`; `dnssec_initialized='N'` (keys/info retained in the panel field, PHP parity) |
| origin changed on an initialized zone | `disable-dnssec` against the *old* origin first, `dnssec_info` replaced with the raw command log |

The panel `dns_soa.dnssec_algo` field is Bind-oriented (ECDSAP256SHA256 /
NSEC3RSASHA1) and is **ignored** on the PowerDNS path — key creation is
fixed to `rsasha256`, matching the PHP plugin. `dns_soa.rendered_zone`
(the Bind "Zone rendering" tab cache) is also Bind-specific and stays
empty/stale on PowerDNS-backed zones; this is a documented parity gap,
not a bug.

## AXFR allow-list

Like the PHP original, the AXFR allow-list is **global**, not per-zone:
on every delayed `powerdns` service **restart**, the plugin collects the
distinct non-empty `xfer` values across every active `dns_soa` and
`dns_slave` row for the server, builds `allow-axfr-ips=127.0.0.1,<unique
IPs>` (localhost always included) and atomically rewrites the config file
at `[dns] powerdns_axfr_conf` (default
`/etc/powerdns/pdns.d/pdns.ispconfig-axfr`). Restarts are queued from
SOA/slave mutations only (not pure record changes, which rely on SQL +
`rectify-zone`/`notify`). Any IP allowed to AXFR one master zone can AXFR
every master zone on the server — same limitation as ISPConfig, documented
here rather than silently inherited.

The `powerdns` service key resolves its systemd unit the same way PHP
did: `powerdns` if that unit exists, else `pdns`.

## Installer

`go-ispconfig install` gained an optional PowerDNS path (`internal/installer`):

- `--dns-backend bind|powerdns` (default `bind`; also settable via the
  answers file `dns_backend = "powerdns"` or the interactive prompt).
- Package set: on the PowerDNS path, `bind9` is dropped and
  `pdns-server` + `pdns-backend-mysql` are installed instead (same names
  on Debian 11–13 and Ubuntu 22.04–24.04) — both daemons would otherwise
  fight over port 53.
- New `powerdns` install step (port of `configure_powerdns`, skipped
  entirely on the bind path): `CREATE DATABASE IF NOT EXISTS powerdns` +
  `GRANT ALL` to the ISPConfig DB user, applies the embedded
  `powerdns.sql` schema, renders the embedded `pdns.local.master`
  template into `/etc/powerdns/pdns.d/pdns.local` (mode 0600, gmysql
  host/port/user/password/dbname filled from the panel DB credentials),
  writes `[dns] dns_backend=powerdns` into the local server row's
  `server.config`, then enables and restarts the `pdns` unit.
- The bind-base step is skipped (returns an error surfaced as a skip) on
  the PowerDNS path — it never touches `/etc/bind` when PowerDNS is
  selected.

See [install.md](install.md) for the full flag/answers reference.

## Migration notes

- No `ispconfig3.sql` changes: this is code + installer only.
- A PHP ISPConfig3 host already running the `powerdns_plugin` reuses its
  existing `powerdns` database as-is — rows bridge by `ispconfig_id`, and
  the daemon's upserts are self-healing (the first datalog event after
  cutover re-syncs each row). Running the PHP and Go daemons against the
  same `powerdns` database at the same time is **not supported**.
- Rollback: set `dns_backend=bind` (or disable the DNS module entirely);
  the `powerdns` database is left untouched and must be cleaned up
  manually if no longer wanted.

## Testing

- Unit: pure mappers (name/content/SOA-content rules, HINFO transform),
  `pdnsutil`/`pdns_control` version gate and command wrappers (stubbed
  `CommandRunner`), the DNSSEC `show-zone` output parser, the AXFR
  allow-list builder, the daemon bootstrap wiring matrix
  (`cmd/daemon_test.go`) — all under `internal/powerdns/`.
- Integration (`go test -tags=integration ./internal/powerdns/...`, real
  MariaDB via `docker run`): event-to-SQL golden tests for master/RR/slave
  scenarios, model round-trips, and an end-to-end pipeline test
  (`TestDatalogToPowerDNSPipeline`) that goes API-repository insert →
  `sys_datalog` → daemon cycle → `powerdns.domains`/`records` rows, with
  `pdns_control`/`pdnsutil` stubbed.
- Panel/API: `e2e/panel-dns-powerdns.sh` (agent-browser) exercises the DNS
  screens against a server row provisioned with `dns_backend=powerdns` —
  login, zone wizard with DNSSEC ticked, record CRUD — proving the panel
  needs no backend-aware code (see [dns-module.md](dns-module.md) and
  `scripts/e2e-dns-powerdns.sh` for the self-contained bootstrap).
- A real `dig` smoke against a PowerDNS container is optional and not
  part of the automated suite; the SQL state it would resolve from is
  already asserted by the integration test above.

## Manual VM validation (deferred — see below)

The `vagrant/` rig ([vagrant/README.md](../vagrant/README.md)) only
provisions the bind path today (`go-ispconfig install --yes` with no
`--dns-backend` flag, `vagrant/smoke-test.sh` asserts bind + named). Wiring
an opt-in PowerDNS VM/toggle and a matching smoke-test branch is real work
(new pdns-aware assertions, a real `pdns_control`/`pdnsutil` on the box)
that needs a VirtualBox host to develop and verify against — not available
in the environment this module was built in. It is deferred rather than
landed untested. Until it exists, validate the installer's PowerDNS path
manually on a disposable Debian/Ubuntu host:

1. `go-ispconfig install --yes --dns-backend powerdns --write-credentials
   --hostname <fqdn>`.
2. Expect: `pdns-server` + `pdns-backend-mysql` installed, `bind9` absent;
   `powerdns` MariaDB database with the `powerdns.sql` tables; readable
   `/etc/powerdns/pdns.d/pdns.local` (mode 0600, gmysql pointed at that
   database); `systemctl is-active pdns` (or `powerdns`, whichever unit
   the distro ships); the local server row's `server.config` containing
   `dns_backend=powerdns`.
3. Create a zone via the panel wizard (same UI as bind, see
   `e2e/panel-dns-powerdns.sh` for the automated version of this step)
   and confirm the `go-ispconfig-daemon` log shows no dial errors against
   the `powerdns` database, then `SELECT * FROM powerdns.domains /
   powerdns.records` shows the zone and its records.
4. `dig @127.0.0.1 <domain> SOA +norec` and an A record — expect
   authoritative answers once PowerDNS has picked up the SQL rows
   (`rediscover`/`notify` are triggered automatically; `pdns_control
   rediscover` can be run by hand if needed).
5. Toggle DNSSEC on the zone; expect `dns_soa.dnssec_info` filled from
   `pdnsutil show-zone` and `dig @127.0.0.1 <domain> DNSKEY +dnssec`
   returning keys once PowerDNS re-signs.
6. `go-ispconfig install --update --yes` a second time; expect no change
   to the `powerdns` database or `pdns.local` (the update pipeline never
   runs the `powerdns` step, same rule as the mariadb/config.toml steps).
