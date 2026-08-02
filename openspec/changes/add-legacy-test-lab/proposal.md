# Proposal: add-legacy-test-lab

> Standing test infrastructure — two fully-populated legacy PHP ISPConfig3
> Vagrant VMs (one nginx, one apache2) that become the permanent base for
> every read/write test, migration validation and module-parity check in
> go-ispconfig.

## Why

The current Vagrant rig provisions a single minimal legacy VM
(`--use-nginx --no-mail`, two parity clients, two empty sites, one DNS
zone) — enough for the installer parity suite, but far from a realistic
migration source. Validating `add-legacy-migration` end to end (and,
later, the mail/ftp-shell/database/client modules) needs a legacy
environment that actually looks like production: many clients, real
WordPress sites serving content, mailboxes behind Roundcube, DNS zones,
FTP/shell users and client databases — on both web servers ISPConfig3
supports (nginx and apache2), since vhost generation, PHP-FPM wiring and
site layouts differ between them.

The real production server (AGENTS.local.md) is read-only by rule — it
can never exercise the write paths (`remote_user` creation, imports,
module CRUD). These two lab VMs have no such restriction: they are the
sanctioned read *and write* playground.

## What Changes

- **Two legacy machines in `vagrant/Vagrantfile`** (standing lab, never
  auto-destroyed by test targets):
  - `legacy` — Ubuntu 24.04, **nginx** stack, upgraded from the current
    minimal provision to the full auto-installer run **with mail**
    (Postfix/Dovecot/Rspamd) and **Roundcube**, fixed IP
    `192.168.56.20`, panel `https://192.168.56.20:8080`.
  - `legacy-apache` — Ubuntu 24.04, **apache2** stack, same full
    provision (mail + Roundcube), fixed IP `192.168.56.21`, panel
    `https://192.168.56.21:8080`.
  - Both: fixed admin password for tests (Vagrantfile env, same pattern
    as today), Remote API enabled, and a **read-write `remote_user`**
    provisioned with the full grant set — so API-driven fixtures and
    `migrate-from` runs need no manual panel step.
- **Fixture dataset** created on both panels via the remote API and/or
  agent-browser flows (scripted, idempotent, documented in
  `vagrant/lab/dataset.md`):
  - **Clients**: several (≥4 per VM), mixed direct clients and one
    reseller with a child client, distinct limits.
  - **Sites**: multiple vhosts per VM; **4 WordPress installs total**
    (2 on nginx, 2 on apache2) actually serving HTTP 200 with wp-admin
    reachable; plus plain PHP and static sites.
  - **Email**: mail domains + mailboxes (with known passwords), aliases,
    at least one autoresponder; **Roundcube webmail** reachable and able
    to log into a lab mailbox; test mail delivered locally between two
    lab mailboxes.
  - **DNS**: zones for the site domains (wizard template), extra record
    types (A/AAAA/MX/TXT/CNAME/SRV) beyond the wizard defaults.
  - **FTP users**: per site, verified with a real FTP login + upload.
  - **Shell users**: including one jailkit-chrooted, verified via SSH.
  - **Databases**: client MySQL databases + users, verified by
    connecting and creating a table (WordPress uses these).
- **Screenshots**: full agent-browser walkthrough of both panels and of
  the running sites/webmail — working set to `docs/prints/lab-*.png`
  (gitignored), curated set to `docs/screenshots/` (committed).
- **Make targets**: `vagrant-lab-up` extended to bring up both legacy
  machines; new `vagrant-lab-fixtures` (idempotent dataset creation),
  `vagrant-lab-status` (IPs, URLs, service health of all lab VMs).
- **Docs**: `vagrant/lab/README.md` — the lab contract: IP/URL table
  (go-ispconfig `.10`, debian `.11`, legacy nginx `.20`, legacy apache2
  `.21`), credential locations (never plaintext in the repo beyond the
  already-public test passwords), what each VM is for, reprovision and
  snapshot/rollback instructions, and the rule that read/write tests
  point HERE — never at the real server.

## Capabilities

### New Capabilities

- `legacy-lab-vms`: the two fully-provisioned legacy ISPConfig3 VMs
  (nginx + apache2, mail + Roundcube, fixed IPs, remote API enabled)
  and their Make/Vagrant lifecycle.
- `legacy-lab-fixtures`: the scripted, idempotent, verified dataset
  (clients, resellers, sites, WordPress, mail, DNS, FTP, shell,
  databases) on both VMs, with screenshots.

### Modified Capabilities

- `install-test-rig`: the parity suite keeps working against the
  upgraded `legacy` VM (mail present no longer "n/a"; parity scope
  unchanged until the mail module ships).

## Impact

- **Consumes**: `add-installer-cli`'s Vagrant rig (Vagrantfile,
  provision-legacy.sh, parity scripts) — extended, not replaced.
- **Unblocks**: full `add-legacy-migration` validation with a rich
  write-allowed source (incl. task 8.1's blocked API happy path,
  exercised against the lab instead of production), plus realistic
  fixtures for the future mail, client, database, ftp-shell and cron
  modules and their migration paths.
- **Host requirements**: VirtualBox RAM for two extra full-stack VMs
  (~2 GB each); WordPress/Roundcube installed from distro/upstream
  tarballs during provision (downloads at provision time only).
- **No go-ispconfig source changes** — this is lab infrastructure:
  Vagrantfile, provision scripts, fixture scripts, docs, screenshots.
