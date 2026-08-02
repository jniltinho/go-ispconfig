# Tasks: add-legacy-test-lab

## 1. VMs and provisioning

- [x] 1.1 `Vagrantfile`: add `legacy-apache` (bento/ubuntu-24.04, `192.168.56.21`, 2 GB) and raise `legacy` resources for the full stack; both marked standing-lab (excluded from `vagrant-destroy`); document the IP table in the file header. Commit.
- [x] 1.2 `provision-legacy.sh`: parameterize web server (`--use-nginx` vs apache2 default) and full stack (drop `--no-mail`, add Roundcube); pin admin password per VM; enable Remote API in `sys_config`/Main Config; provision a read-write `remote_user` (full grant set) with a fixed test password via SQL. Idempotent re-run. Commit.
- [x] 1.3 Bring both VMs up for real; verify: panels answer on :8080, `nginx -t`/`apache2ctl configtest`, postfix/dovecot/rspamd units active, Roundcube reachable; record versions in `vagrant/lab/README.md`. Screenshots `docs/prints/lab-provision-*.png`. Commit.

## 2. Fixture dataset (both VMs, scripted + idempotent)

- [x] 2.1 `vagrant/lab/dataset.md` + `vagrant/lab/fixtures.sh` skeleton (remote-API driven via curl/jq or the go client; per-entity idempotency by natural key). Commit.
- [x] 2.2 Clients: ≥4 per VM incl. one reseller with one child client, distinct limit profiles; screenshots of client lists. Commit.
- [x] 2.3 Sites: vhosts per client (static, plain PHP, and WordPress placeholders), auto-subdomain www, one site with custom nginx/apache directives; HTTP 200 checks. Commit.
- [x] 2.4 DNS: zones for all site domains via wizard template + extra records (AAAA/CNAME/SRV/TXT); named-checkzone green on both VMs. Commit.
- [x] 2.5 Email: mail domains, ≥2 mailboxes per VM with known passwords, one alias, one autoresponder; local delivery test between two lab mailboxes (swaks or sendmail + Dovecot check). Commit.
- [x] 2.6 FTP users (one per WordPress site, verified real FTP login + upload) and shell users (one plain, one jailkit-chrooted, verified via SSH command). Commit.
- [x] 2.7 Client databases + DB users for the WordPress installs, verified by SQL connect; document credentials in `vagrant/lab/dataset.md` (test-only values). Commit.

## 3. Applications

- [x] 3.1 WordPress: install 4 total (2 per VM) into fixture sites using the fixture DBs/FTP users — wp-cli unattended install; verify front page HTTP 200 with rendered content and wp-admin login; screenshots. Commit.
- [x] 3.2 Roundcube: verify webmail login with a fixture mailbox on both VMs, send a mail from Roundcube to the other lab mailbox and see it arrive; screenshots. Commit.

## 4. Lab contract and integration

- [ ] 4.1 Make targets: extend `vagrant-lab-up` (both legacies), add `vagrant-lab-fixtures` and `vagrant-lab-status` (IP/URL/health table output). Commit.
- [ ] 4.2 `vagrant/lab/README.md`: IP/URL table (.10/.11/.20/.21), credential locations, per-VM purpose, reprovision + VirtualBox snapshot/rollback guide, and the standing rule: read/write tests target the lab, NEVER the real server. Link from vagrant/README.md and AGENTS.md. Commit.
- [ ] 4.3 Re-run the parity suite against the upgraded `legacy` VM (mail present) and adjust intended-differences/dataset docs if provisioning changed defaults; suite must stay green. Commit.
- [ ] 4.4 Validate `migrate-from --dry-run` (CLI) and the wizard connect/inventory/dry-run (UI) against BOTH lab VMs using the provisioned remote_user — closing the API happy path that was blocked on the real server; record inventory numbers in the README; curated screenshots to docs/screenshots/. Commit.
