# web-letsencrypt

## ADDED Requirements

### Requirement: Certificate issuance is available on every web module

A site with `ssl = y` and `ssl_letsencrypt = y` SHALL have a certificate
requested when it is created or when either flag or its domain changes,
regardless of which web module the node runs.

#### Scenario: Apache node issues for a Let's Encrypt site

- **WHEN** a `web_domain` row with `ssl = y` and `ssl_letsencrypt = y` reaches
  a node installed with `--web-server apache`
- **THEN** the detected ACME client is invoked for that domain
- **AND** the resulting key and certificate are written to the site's
  `-le.key` / `-le.crt` paths
- **AND** the vhost is re-rendered so it serves them rather than the
  self-signed pair

#### Scenario: A node that cannot issue says so

- **WHEN** no ACME client is installed on the node
- **THEN** the request fails with an error naming the domain
- **AND** the error is recorded to the datalog rather than logged and dropped

### Requirement: Issuance behaviour does not depend on the web server

The issued certificate SHALL be identical whichever web module requested it:
same client preference (acme.sh before certbot), same webroot authenticator,
same ECDSA/RSA version gates, same domain assembly and reachability filter.

#### Scenario: nginx is unchanged by the extraction

- **WHEN** the same site row is processed by the nginx module before and
  after the issuer moves to `internal/web`
- **THEN** the argv passed to the ACME client is byte for byte identical

#### Scenario: Both modules assemble the same domain list

- **WHEN** a site has active subdomains and aliases, some marked
  `ssl_letsencrypt_exclude = y`
- **THEN** both modules request the same de-duplicated list, capped at 100
  domains, with the excluded ones absent

### Requirement: Renewal has something to renew

The daily renewal job SHALL NOT report success on a node whose certificates
were never issued.

#### Scenario: Apache renewal after issuance

- **WHEN** the renewal job runs on an Apache node with an issued certificate
- **THEN** it renews that certificate and reloads Apache only when a file
  actually changed
