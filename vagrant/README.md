# Vagrant test rig

Real-VM end-to-end proof that `go-ispconfig install --yes` works on a clean
host (openspec `add-installer-cli`, spec `install-test-rig`). Requires
Vagrant + VirtualBox on the host.

| Machine  | Box                | IP             | Purpose                                   |
|----------|--------------------|----------------|-------------------------------------------|
| `ubuntu` | bento/ubuntu-24.04 | 192.168.56.10  | Default rig — installer E2E + smoke test  |
| `debian` | bento/debian-12    | 192.168.56.11  | Opt-in second supported target            |
| `legacy` | bento/ubuntu-24.04 | 192.168.56.20  | Standing lab: PHP ISPConfig3 nginx, full stack (see [lab/](lab/README.md)) |
| `legacy-apache` | bento/ubuntu-24.04 | 192.168.56.21 | Standing lab: PHP ISPConfig3 apache2, full stack (see [lab/](lab/README.md)) |

All machines sit on the VirtualBox host-only network `192.168.56.0/24`, so
the panels are reachable from the host: go-ispconfig at
`https://192.168.56.10:8080` (self-signed) and the legacy panel at
`https://192.168.56.20:8080`.

## Usage

```sh
make vagrant-up        # cross-build linux/amd64 binary, vagrant up ubuntu
make vagrant-test      # run vagrant/smoke-test.sh inside the guest
make vagrant-destroy   # destroy every rig VM
```

`vagrant up` with no machine name creates only `ubuntu`. Provisioning copies
the host-built binary to `/usr/local/bin/go-ispconfig` and runs
`go-ispconfig install --yes --write-credentials --hostname <name>.goisp.test
--admin-email admin@goisp.test`.

The smoke test checks: both systemd units active, panel HTTPS, API login
(password read from `/root/.go-ispconfig-credentials`), website creation via
API + rendered vhost + `nginx -t`, DNS zone creation via the wizard API +
rendered zone file + `named-checkzone`, and that a second
`install --yes` run is idempotent. It exits non-zero naming the failed check.

## Debian 12 run

Ubuntu 24.04 is the default/required rig; Debian 12 is an additional
supported target. Same cycle, no Vagrantfile changes:

```sh
make vagrant-up VM=debian     # vagrant up debian (192.168.56.11)
make vagrant-test VM=debian   # smoke test against the Debian guest
```

## Standing lab (legacy PHP ISPConfig3, nginx + apache2)

```sh
make vagrant-lab-up        # ubuntu (.10) + legacy (.20) + legacy-apache (.21)
make vagrant-lab-fixtures  # idempotent fixture dataset + WordPress on both legacies
make vagrant-lab-status    # IP/URL/health table
make vagrant-parity-test   # parity suite: see ../vagrant/parity/
```

The two `legacy*` machines are the **standing lab** — full ISPConfig3
stacks (web + bind + mail + Roundcube) provisioned by the official
auto-installer, with fixed test credentials and a full-grant remote-API
user. The lab contract (IP/credential tables, per-VM purpose,
reprovision and snapshot guide, and the read/write rule) lives in
[lab/README.md](lab/README.md). Provisioning pulls the current ISPConfig
release from the network; a full-stack run takes 30–60 minutes and can
fail transiently on mirror hiccups — `vagrant provision <machine>`
retries idempotently. `make vagrant-destroy` never touches the lab VMs.

## Troubleshooting

- **dpkg lock / apt failures during provision**: cloud-init or
  unattended-upgrades may hold the dpkg lock right after boot. The installer
  waits on the lock with a timeout; if provisioning still fails, re-run
  `vagrant provision <machine>` — every step is idempotent.
- **Box update available**: `vagrant box update` (old box versions keep
  working; the rig does not pin a box version).
- **Stale VM / weird state**: `make vagrant-destroy && make vagrant-up`
  is always safe — nothing persists outside the VMs.
- **Host-only network missing**: VirtualBox creates `vboxnet0`
  (192.168.56.1/24) on demand. On Linux hosts with VirtualBox ≥ 6.1.28,
  ranges outside 192.168.56.0/21 require `/etc/vbox/networks.conf`.
- **Credentials**: the generated admin password lives in
  `/root/.go-ispconfig-credentials` inside each go-ispconfig guest
  (written because provisioning passes `--write-credentials`).

Nothing under `.vagrant/`, boxes, DB dumps or `docs/prints/` is ever
committed (see `.gitignore`).
