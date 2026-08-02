# Migrating from ISPConfig3 (PHP) to go-ispconfig

go-ispconfig uses a database schema **100% identical** to ISPConfig3 (the original
`ispconfig3.sql` DDL is embedded in the binary). That makes client migration a
first-class feature, with two supported paths.

## Path A — Shared-database cutover (same host / same database)

Use when go-ispconfig will take over the existing server and its `dbispconfig` database.

1. **Drain the datalog queue.** Let the PHP daemon run until everything pending is
   applied — check that `SELECT MAX(datalog_id) FROM sys_datalog` equals
   `SELECT updated FROM server WHERE server_id = <id>`.
2. **Stop the PHP side.** Disable the ISPConfig cron entries (`server.sh`,
   `cron_daily.sh`) and stop the panel vhost.
3. **Point go-ispconfig at the database.** Set the DSN in `config.toml`
   (`go-ispconfig init` generates a template).
4. **Validate.** Run `go-ispconfig migrate` — it detects the existing ISPConfig
   schema via `server.dbversion`, executes **no DDL**, and only validates
   compatibility. Nothing is seeded; your data is untouched.
5. **Start services.** `systemctl enable --now go-ispconfig-serve go-ispconfig-daemon`.
6. **Log in.** Users keep their existing credentials — legacy crypt hashes
   (SHA-512 `$6$`, MD5-crypt `$1$`) are verified in place. Re-hashing to bcrypt
   is opt-in (`auth.rehash_legacy`, see notes below).

Notes:
- **Minimum version: ISPConfig 3.3.x.** Older installs (3.1/3.2) must first be
  updated with the PHP updater (it applies the incremental DDL); `migrate`
  aborts otherwise.
- Migration is a **cutover** — running both panels against the same database at
  the same time is unsupported. New `sys_datalog` rows are written as JSON, which
  the PHP daemon cannot read. Pre-cutover datalog history stays readable only as
  raw text.
- Multi-server / mirror setups are **not supported**: the daemon refuses to start
  if `mirror_server_id` is set or more than one active server row exists.
- Password hashes keep working (crypt `$6$`/`$1$`). Re-hash to bcrypt is opt-in
  (`auth.rehash_legacy`) — leave it off until you are sure you won't roll back,
  because PHP ISPConfig cannot verify bcrypt.
- Remote API automations (`/remote/json.php` with `remote_user`) **stop working**
  at cutover — the granular remote-grant model is not ported yet. Plan to rewrite
  scripts against the new REST API (Swagger at `/swagger/`).
- After cutover a one-shot resync re-renders all active vhosts/zones.
- Rollback checklist: stop go-ispconfig → drain the JSON datalog rows it wrote →
  confirm `auth.rehash_legacy` was never enabled (else affected users need
  password resets on PHP) → re-enable the PHP crons.

## Path B — Remote API import (legacy panel on another host/database)

Use when the legacy ISPConfig3 stays where it is and you are populating a fresh
go-ispconfig server. Implemented by the `add-legacy-migration` module —
**full walkthrough: [legacy-migration.md](legacy-migration.md)**.

- **Web UI wizard** (System → Migrate from ISPConfig3): enter the legacy panel
  URL and a `remote_user`'s credentials → test connection → review the
  inventory (clients, sites, DNS zones) → dry-run with conflict report →
  import with live progress.
- **CLI**: `go-ispconfig migrate-from --url https://legacy:8080
  --user <remote_user> [--dry-run] [--only clients,sites,dns]`
  (the `/remote/json.php` path is appended automatically; the password is
  prompted when omitted).

Behavior:
- Imports clients (resellers first, preserving `parent_client_id` hierarchy),
  web domains, protected folders (`web_folder`/`web_folder_user`), DNS zones,
  records, slave zones and zone templates 1:1 (the schemas match), preserving
  ownership (`sys_userid`/`sys_groupid`) and riud permissions.
- **Panel passwords cannot be imported** (the remote API does not expose hashes):
  the wizard/CLI runs a bulk password-reset flow and the final report lists every
  user needing a reset.
- Refuses (or requires explicit confirmation) when the legacy install has more
  than one active server (multi-server is unsupported).
- Idempotent: re-running does not duplicate (natural keys: domain, zone origin,
  username).
- After import, `sys_datalog` entries are written so the daemon applies the
  configuration (nginx vhosts, Bind zones) on the new host.
- Website files are **not** copied — sync them yourself **before enabling
  SSL/Let's Encrypt** (webroot challenges fail on empty docroots), remapping
  owners since uid/gid differ between hosts, e.g.
  `rsync -aHAX --usermap=all:web1 --groupmap=all:client1 legacy:/var/www/clients/web1/ /var/www/clients/web1/`
  (per site, using the new host's system user/group).
- Imported SSL fields are kept, but certificates are re-issued on the new host.
- Quota values are imported but **stored-only** until the quota enforcement phase
  of the web module ships.

Prerequisites on the legacy side: enable the remote API and create a
`remote_user` with read access to the relevant functions
(ISPConfig panel → System → Remote Users).

## Which path should I use?

| Situation | Path |
|---|---|
| Same server, take over the existing DB | A — cutover |
| New server, legacy stays up during transition | B — API import |
| New server, but you have a DB dump | Restore dump, then Path A |

## Scope of the initial release

Web (nginx) and DNS (Bind) data is fully managed after migration. Tables of other
modules (mail, FTP, …) are preserved untouched in the database and become
manageable as their go-ispconfig modules ship (see `docs/ROADMAP.md`).
