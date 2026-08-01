# Tasks: add-installer-cli

Rule: every finished task = validated (tests pass / manual check noted) + conventional commit.

## 1. Distro profiles and answers

- [ ] 1.1 `internal/installer/distro.go`: `/etc/os-release` parsing + `Profile` data for debian11/12/13, ubuntu22.04/24.04 (packages, service names, nginx/bind/php-fpm paths ported from `install/dist/conf/{debian110,debian120,debian130,ubuntu2204,ubuntu2404}.conf.php`); unsupported-distro error; unit tests with fixture os-release files
- [ ] 1.2 `internal/installer/answers.go`: `Answers` struct, resolution order flags > `--answers file.toml` > interactive prompt > default; `--yes` aborts on missing required answer naming the flag; prompt helpers (port of `simple_query`/`free_query`); unit tests for precedence and `--yes` gaps
- [ ] 1.3 `cmd/install.go`: Cobra command with flags (`--yes`, `--answers`, `--update`, `--dry-run`, `--panel-port`, `--db-root-password`, service toggles); root/`--dry-run` gate; wire to pipeline

## 2. Step pipeline

- [ ] 2.1 `internal/installer/pipeline.go`: `Step` interface, ordered runner with done/skipped/failed logging, `State` carrying profile+answers+generated secrets; unit test with fake steps (order, failure stops, re-run skips)
- [ ] 2.2 Preflight step: root, systemd, apt present, abort if `/usr/local/ispconfig` exists (points to add-legacy-migration); tests
- [ ] 2.3 Packages step: apt noninteractive (`DEBIAN_FRONTEND`, force-confold), dpkg-lock wait with timeout, php-fpm only when answered yes, idempotent (skip installed); test with mocked exec
- [ ] 2.4 File-write helper: backup differing file to `<file>.bak-<ts>`, no-op+no-backup on identical content, restore-on-failure; unit tests (this helper is used by every config step)

## 3. Database, server record, config.toml

- [ ] 3.1 MariaDB step: root connect via unix socket (fallback `--db-root-password`), create `dbispconfig` + `ispconfig` user with generated password, then invoke the foundation `migrate` code path (embedded ispconfig3.sql, skip when schema exists); integration test against docker MariaDB
- [ ] 3.2 IP detection + server record step: enumerate global IPv4/IPv6, insert `server_ip` rows, create/reuse `server` row (web_server=1, dns_server=1, default config INI for web/dns sections); integration test
- [ ] 3.3 config.toml step: write `/etc/go-ispconfig/config.toml` 0600 with DSN/panel/TLS paths; on re-run reuse existing DB credentials, backup only when content differs; tests
- [ ] 3.4 Admin seed + summary step: create admin user with generated password only when absent; print credentials once; write root-only `/root/.go-ispconfig-credentials` only with `--write-credentials` (default print-only); re-run never regenerates/reprints; tests

## 4. Services configuration

- [ ] 4.1 Self-signed cert step: crypto/x509 cert+key (CN/SAN=FQDN, 10y) in `/etc/go-ispconfig/ssl/`, skip when valid cert exists; consumed directly by `serve` (no nginx panel vhost/proxy); unit test verifying cert fields and skip logic
- [ ] 4.2 Embedded assets: systemd units (`go-ispconfig-serve.service`, `go-ispconfig-daemon.service`) + bind base templates (`named.conf.options` port, local zones include) derived from `install/tpl/named.conf.options.master`, and nginx snippet/include dir assets for the web module (no panel vhost — `serve` terminates TLS itself)
- [ ] 4.3 nginx base step: render web-module snippet/include dirs (no panel vhost), `nginx -t` gate with restore-on-failure, reload only on change; test with mocked exec
- [ ] 4.4 bind base step: render named.conf.options + include, `named-checkconf` gate with restore-on-failure; test with mocked exec
- [ ] 4.5 systemd step: write units, `daemon-reload`, `enable --now`; assert no crontab is ever touched; test with mocked exec
- [ ] 4.6 Optional `install-acme` step: install acme.sh (or certbot when chosen) for site Let's Encrypt, default off, idempotent (existing client → no-op), panel stays self-signed; test with mocked exec

## 5. Update and uninstall

- [ ] 5.1 `install --update`: pipeline subset (preflight + config renders + unit restart), never touches DB/credentials/certs/admin; test asserting untouched paths
- [ ] 5.2 `cmd/uninstall.go`: stop/disable/remove units, remove rendered configs and `/etc/go-ispconfig/` (`--keep-config`), `--purge-db` drops db+user, typed confirmation unless `--yes`, never removes packages; tests

## 6. Vagrant test rig

- [ ] 6.1 `vagrant/Vagrantfile`: default machine `ubuntu` (bento/ubuntu-24.04), opt-in machine `debian` (bento/debian-12); provisioner syncs host-built linux/amd64 binary and runs `go-ispconfig install --yes`
- [ ] 6.2 `vagrant/smoke-test.sh`: units active, panel HTTPS via curl -k, API login, API create site + `nginx -t`, API create zone + `named-checkzone`, second `install --yes` idempotency check; non-zero exit names the failed check
- [ ] 6.3 Makefile targets: `vagrant-up` (build linux/amd64 first), `vagrant-test` (runs smoke test in guest, propagates exit code), `vagrant-destroy`
- [ ] 6.4 `vagrant/README.md`: usage, Ubuntu default, documented Debian 12 run (`vagrant up debian` + smoke test), troubleshooting (dpkg lock, box updates)
- [ ] 6.5 Full E2E validation: run `make vagrant-up vagrant-test` on Ubuntu 24.04 green, then documented Debian 12 run green; record results in the PR description

## 7. Legacy comparison lab (192.168.56.x)

- [ ] 7.1 Private network: fixed IPs for all machines in `192.168.56.0/24` (go-ispconfig `.10`, debian `.11`, legacy `.20`); panels reachable from host; update smoke tests to use the fixed IPs
- [ ] 7.2 `legacy` machine: Ubuntu 24.04 box provisioned with PHP ISPConfig3 nginx+bind via the official auto-installer (script adapted from the existing ISPConfig auto-install scripts), fixed admin password for tests, panel on `https://192.168.56.20:8080`
- [ ] 7.3 agent-browser flows on the legacy panel: create N test clients, websites, DNS zones and email accounts (email accounts are baseline/migration-source only, excluded from parity); capture screenshots to docs/prints/; export created record set (DB dump of relevant tables) as the parity baseline
- [ ] 7.4 agent-browser flows on go-ispconfig: create the same clients, websites and DNS zones (parity scope: clients + sites + DNS only; no email comparison until the mail module ships); parity assertions (shared-schema columns, `nginx -t`, vhost serves HTTP 200) + parity report (intended UI differences vs unintended divergences, non-zero exit on unintended)
- [ ] 7.5 Makefile targets `vagrant-lab-up` / `vagrant-parity-test`; ensure `.gitignore` covers `.vagrant/`, boxes, dumps and any test data

## 8. Docs

- [ ] 8.1 `docs/install.md`: quickstart (one command), all flags/answers file format, update mode, uninstall, credential recovery, backup file locations
