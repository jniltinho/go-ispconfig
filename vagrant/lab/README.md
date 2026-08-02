# Standing legacy lab

Two fully-provisioned legacy PHP ISPConfig3 VMs — the permanent base for
every read/write test, migration validation and module-parity check in
go-ispconfig (openspec `add-legacy-test-lab`).

**The standing rule: read/write tests point HERE — never at the real
production server** (AGENTS.md "Servidor legado real": that one is
read-only, always). The lab VMs are the sanctioned read *and write*
playground; break them freely, reprovision cheaply.

## Machines

| Machine         | Box                | IP            | Stack                                      | Purpose |
|-----------------|--------------------|---------------|--------------------------------------------|---------|
| `ubuntu`        | bento/ubuntu-24.04 | 192.168.56.10 | go-ispconfig (nginx + bind)                | The panel under development |
| `debian`        | bento/debian-12    | 192.168.56.11 | go-ispconfig (opt-in)                      | Second supported install target |
| `legacy`        | bento/ubuntu-24.04 | 192.168.56.20 | PHP ISPConfig3, **nginx**, mail + Roundcube | Parity baseline, nginx migration source |
| `legacy-apache` | bento/ubuntu-24.04 | 192.168.56.21 | PHP ISPConfig3, **apache2**, mail + Roundcube | apache2 migration source (vhost/FPM layout differs) |

Panels: `https://<ip>:8080` (self-signed). Roundcube webmail (legacy VMs):
`https://<ip>:8081/webmail` (on the nginx VM this lands on `/squirrelmail/`,
which the auto-installer symlinks to Roundcube). All four sit on the
VirtualBox host-only network `192.168.56.0/24`.

## Installed versions

Recorded at provision time (task 1.3):

| Component | legacy (nginx)       | legacy-apache        |
|-----------|----------------------|----------------------|
| ISPConfig | 3.3.1p1              | 3.3.1p1              |
| OS        | Ubuntu 24.04         | Ubuntu 24.04         |
| Web       | nginx 1.24.0         | Apache 2.4.58        |
| MariaDB   | 10.11.14             | 10.11.14             |
| Postfix   | 3.8.6                | 3.8.6                |
| Dovecot   | 2.3.21               | 2.3.21               |
| Rspamd    | 4.1.4                | 4.1.4                |
| Roundcube | 1.6.6 (distro)       | 1.6.6 (distro)       |
| PHP (panel) | 8.3 (5.6–8.5 FPM pools installed) | 8.3 (5.6–8.5 FPM pools installed) |

## Credentials (test-only fixed values)

| What | Where |
|------|-------|
| Legacy panel admin | `admin` / `GoIspParity2026` (both legacy VMs; pinned in `vagrant/Vagrantfile`) |
| Legacy remote-API user | `goisp-lab` / `GoIspRemote2026` (full grant set, provisioned by `provision-legacy.sh`) |
| go-ispconfig admin | generated at install; `/root/.go-ispconfig-credentials` inside the `ubuntu`/`debian` guest |
| Fixture entities (clients, mailboxes, FTP/shell/DB users, WordPress) | `vagrant/lab/dataset.md` |

These lab passwords are deliberately committed (same convention as the
parity rig) — they only ever exist inside throwaway host-only VMs.

## Lifecycle

```sh
make vagrant-lab-up        # bring up ubuntu + legacy + legacy-apache
make vagrant-lab-fixtures  # idempotent fixture dataset on both legacy VMs
make vagrant-lab-status    # IP/URL/service-health table for all lab VMs
```

`make vagrant-destroy` never touches the standing lab (`legacy`,
`legacy-apache`). To reprovision one from scratch:

```sh
cd vagrant
vagrant destroy -f legacy          # or legacy-apache
vagrant up legacy                  # auto-installer run, 30-60 min
make vagrant-lab-fixtures          # recreate the dataset
```

Provisioning is idempotent — `vagrant provision legacy` re-runs safely
after transient apt/mirror failures.

## Snapshots (fast rollback)

Before a destructive experiment, snapshot the VM; roll back instead of
reprovisioning:

```sh
cd vagrant
vagrant snapshot save legacy pristine        # take
vagrant snapshot restore legacy pristine     # roll back
vagrant snapshot list legacy
```

## What runs where

- **Parity suite** (`make vagrant-parity-test`, `vagrant/parity/`):
  compares the parity dataset between `legacy` and `ubuntu`.
- **Migration validation** (`docs/legacy-migration.md`): `migrate-from`
  CLI / wizard against `legacy` and `legacy-apache` via the `goisp-lab`
  remote user.
- **Future module fixtures** (mail, client, database, ftp-shell, cron):
  the dataset of `vagrant/lab/dataset.md` on both legacy VMs.

## Migration inventory (validated 2026-08-02)

`migrate-from --dry-run` (CLI on the `ubuntu` guest) and the panel wizard
(connect → inventory → dry-run) were both run against BOTH lab VMs with
the `goisp-lab` remote user — the API happy path that was blocked on the
real read-only server:

| Inventory        | legacy (.20) | legacy-apache (.21) |
|------------------|--------------|---------------------|
| clients          | 8            | 5                   |
| web domains      | 6            | 4                   |
| dns zones        | 5            | 4                   |
| dns records      | 39           | 32                  |
| dns templates    | 1            | 1                   |

- `legacy-apache`: plan fully clean — "Dry-run: no conflicts; nothing
  was written" (5 clients, 4 web_domain, 4 dns_soa, 32 dns_rr create).
- `legacy`: 10 conflicts, all legitimate — parity1/parity2 site + zone
  records already exist on the go-ispconfig panel (created as admin by
  the parity flows), correctly reported as owned-by-a-different-user.
- The runs exposed and fixed two real bugs in the legacy client/importer
  (paged-filter JSON key order; owner resolution via `client_get_id` on
  3.3.x panels) — see the `fix(legacy):` commits.

Curated screenshots: `docs/screenshots/lab-migration-*.png`.
