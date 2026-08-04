# Spec: acme-issuance

## ADDED Requirements

### Requirement: Native certificate issuance

The panel SHALL obtain Let's Encrypt certificates without requiring an external
ACME client (`acme.sh` or `certbot`) to be installed on the host.

#### Scenario: A site on a host with no ACME client

- **GIVEN** a host where neither `acme.sh` nor `certbot` is installed
- **WHEN** an operator enables Let's Encrypt on a site
- **THEN** a certificate is obtained and the vhost serves it

#### Scenario: An Apache node issues its first certificate

- **GIVEN** an Apache-only node with no certificate for the site
- **WHEN** Let's Encrypt is enabled on that site
- **THEN** the certificate is issued, written to the shared datastore, and
  Apache is gracefully reloaded
- **AND** the vhost does not reference certificate files that do not exist

### Requirement: Issuance is idempotent

Issuance SHALL NOT contact the CA when a certificate covering the same domain
set is already stored with more than 30 days of validity remaining.

#### Scenario: Re-applying an unchanged site

- **GIVEN** a stored certificate for `example.com`, `www.example.com` valid for
  60 more days
- **WHEN** the site's configuration is applied again
- **THEN** no request is made to the CA

#### Scenario: An alias is added

- **GIVEN** the same stored certificate
- **WHEN** `shop.example.com` is added to the site
- **THEN** a new certificate covering all three names is issued

### Requirement: Challenge solving matches the deployment

The http-01 solver SHALL write challenge tokens to the shared ACME webroot
already routed by both vhost templates, and SHALL NOT bind a network port.

The dns-01 solver SHALL operate through this panel's own DNS records, and SHALL
refuse a zone the panel does not host, with an error naming that zone.

#### Scenario: http-01 on a busy web server

- **GIVEN** nginx serving on port 80
- **WHEN** a certificate is issued via http-01
- **THEN** the token is served from the shared webroot
- **AND** no port bind is attempted

#### Scenario: dns-01 for an externally hosted zone

- **GIVEN** a site whose zone has no `dns_soa` row in this panel
- **WHEN** dns-01 issuance is attempted
- **THEN** it fails with an error naming the zone
- **AND** no challenge record is left behind

### Requirement: Existing external-client certificates keep working

Certificates previously issued by `acme.sh` or `certbot` SHALL continue to be
served, and SHALL NOT be moved, rewritten or revoked by this change.

#### Scenario: An adopted install is not disturbed

- **GIVEN** a host with certificates under `/etc/letsencrypt/live`
- **WHEN** the daemon starts with native issuance enabled
- **THEN** those paths are untouched and the vhosts continue to serve them

### Requirement: The external client remains selectable

A `[server] acme_client` setting SHALL select between the native client and the
external ones, so an operator with a working external setup can keep it.

#### Scenario: An operator pins the external client

- **GIVEN** `acme_client` is set to `acme.sh`
- **WHEN** a certificate is issued or renewed
- **THEN** the external binary is invoked exactly as it is today

### Requirement: The account key is not replicated

The ACME account private key SHALL be stored on the node's filesystem and SHALL
NOT be written to the database or journalled through `sys_datalog`.

#### Scenario: Issuance on a multi-server install

- **WHEN** a certificate is issued on a node of a multi-server install
- **THEN** no private key material appears in `sys_datalog` or any panel table

### Requirement: Tests never contact the production CA

Automated tests SHALL NOT reach the production Let's Encrypt directory. Tests
that contact a real CA SHALL use the staging directory and SHALL be skipped
unless explicitly enabled.

#### Scenario: CI run

- **WHEN** the test suite runs without `GOISP_ACME_STAGING`
- **THEN** no request leaves the machine for any ACME directory
