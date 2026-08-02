# install-test-rig

## ADDED Requirements

### Requirement: Vagrant environment for install testing
The repository SHALL contain `vagrant/Vagrantfile` defining an Ubuntu 24.04 machine (`bento/ubuntu-24.04` or equivalent) as the default, and an optional Debian 12 machine (`bento/debian-12`) started only when named explicitly. Provisioning SHALL copy the host-built linux/amd64 `go-ispconfig` binary into the guest and run `go-ispconfig install --yes` with fixed non-interactive answers.

#### Scenario: Provision runs unattended install
- **WHEN** `vagrant up` runs the default machine
- **THEN** the guest ends provisioning with `go-ispconfig install --yes` completed successfully using the synced binary

#### Scenario: Debian machine is opt-in
- **WHEN** `vagrant up` is invoked with no machine name
- **THEN** only the Ubuntu 24.04 machine is created

### Requirement: Smoke test script
`vagrant/smoke-test.sh` SHALL run inside the guest and fail (non-zero exit) unless all checks pass: `go-ispconfig-serve` and `go-ispconfig-daemon` systemd units active; panel responds over HTTPS (self-signed accepted); REST API login as admin succeeds; creating a website via the API results in an nginx vhost that passes `nginx -t`; creating a DNS zone via the API results in a zone file that passes `named-checkzone`; a second `go-ispconfig install --yes` run exits 0 with units still active (idempotency check).

#### Scenario: All checks pass on fresh install
- **WHEN** `smoke-test.sh` runs in a freshly provisioned Ubuntu 24.04 guest
- **THEN** it exits 0 having verified units, panel HTTPS, API site creation with `nginx -t`, API zone creation with `named-checkzone`, and install idempotency

#### Scenario: Broken service fails the test
- **WHEN** `go-ispconfig-daemon` is not active when the script runs
- **THEN** the script exits non-zero naming the failed check

### Requirement: Makefile targets for the Vagrant rig
The Makefile SHALL provide: `vagrant-up` (builds the linux/amd64 binary, then `vagrant up`), `vagrant-test` (ensures the machine is up, runs `smoke-test.sh` in the guest, propagates its exit code), and `vagrant-destroy` (`vagrant destroy -f`).

#### Scenario: vagrant-test propagates failure
- **WHEN** `make vagrant-test` runs and one smoke check fails
- **THEN** `make` exits non-zero

### Requirement: Private network on 192.168.56.x
All Vagrant machines SHALL attach a host-only private network in the `192.168.56.0/24` range with fixed addresses (e.g. go-ispconfig Ubuntu `192.168.56.10`, Debian `192.168.56.11`, legacy PHP ISPConfig `192.168.56.20`), so the host and the guests can reach every panel directly by IP.

#### Scenario: Panels reachable from the host
- **WHEN** the machines are up
- **THEN** the go-ispconfig panel responds at `https://192.168.56.10:<port>` and the legacy panel at `https://192.168.56.20:8080` from the host

### Requirement: Legacy PHP ISPConfig comparison VM
The rig SHALL define an opt-in machine `legacy` (Ubuntu 24.04) provisioned with the original PHP ISPConfig3 (nginx + bind variant, via the official ISPConfig auto-installer, using the existing ISPConfig auto-install scripts as step-sequence reference), serving as (a) the behavior reference for parity validation and (b) a live migration source for `add-legacy-migration` testing.

#### Scenario: Legacy panel provisioned
- **WHEN** `vagrant up legacy` completes
- **THEN** the PHP ISPConfig3 panel is reachable at its fixed 192.168.56.x address and admin login works

### Requirement: Parity validation via agent-browser
An agent-browser E2E suite SHALL drive both panels through equivalent flows and compare outcomes, with parity scope limited to **clients + sites + DNS** while there is no mail module: on the legacy panel create test clients, websites, DNS zones and email accounts; on go-ispconfig create the same clients, websites and DNS zones. Email accounts are created on the legacy side only as a future-mail-module baseline and as migration-source data — they SHALL NOT be compared. The suite SHALL assert per-flow parity — equivalent records in each database, equivalent generated configs (nginx vhost on both; zone files when DNS flows are exercised) — and produce a parity report listing intended UI/UX differences versus unintended behavioral divergences. Screenshots of both panels go to `docs/prints/` (not committed) for human validation.

#### Scenario: Same site created on both panels
- **WHEN** the suite creates client + website on the legacy panel and repeats the flow on go-ispconfig
- **THEN** both hosts serve the site (HTTP 200 on the test vhost) and the go-ispconfig record set matches the legacy one on the shared schema columns

#### Scenario: Divergence is reported
- **WHEN** a flow produces a different outcome on go-ispconfig than on the legacy panel (missing field, different default, config diff)
- **THEN** the parity report lists it as a divergence and the suite exits non-zero for unintended ones

### Requirement: Documented Debian 12 E2E run
Documentation (`vagrant/README.md` or `docs/`) SHALL describe how to run the same install + smoke test cycle on the Debian 12 machine (`vagrant up debian`, `make vagrant-test VM=debian` or equivalent), and state that Ubuntu 24.04 is the default/required rig while Debian 12 is an additional supported target.

#### Scenario: Debian run reproducible from docs
- **WHEN** a developer follows the documented Debian 12 steps
- **THEN** the install and smoke test execute on the Debian guest without modifying the Vagrantfile
