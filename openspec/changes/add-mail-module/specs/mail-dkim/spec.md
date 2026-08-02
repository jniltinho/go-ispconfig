# mail-dkim

## ADDED Requirements

### Requirement: DKIM key generation on the API
When DKIM is enabled for a mail domain and no valid private key is supplied, the system SHALL generate an RSA private key of `dkim_strength` bits (default 2048 from mail getconf), derive the PEM public key, and store both in `mail_domain.dkim_private` and `mail_domain.dkim_public` with `dkim_selector` (default `default`). Supplied private keys SHALL be validated (RSA check) and public keys derived when missing (port of `ajax_get_json.php` create_dkim + `mail_domain_edit.php`).

#### Scenario: Generate key for new DKIM domain
- **WHEN** a domain is created or updated with `dkim=y` and empty `dkim_private`
- **THEN** `dkim_private` and `dkim_public` are populated with a matching key pair and selector is non-empty

#### Scenario: Invalid private key rejected
- **WHEN** a domain update submits a malformed `dkim_private`
- **THEN** the API returns a validation error (`dkim_private_key_error` parity) and no datalog row is written

### Requirement: DKIM key files and Rspamd signing maps on the daemon
On `mail_domain_insert|update` when `active=y` and `dkim=y`, the dkim plugin SHALL write `<dkim_path>/<domain>.private` and `.public` from the DB key material, ensure `dkim_path` exists with ownership suitable for the rspamd user, and maintain Rspamd map lines in `dkim_domains.map` and `dkim_selectors.map` (`domain private-path`, `domain selector`), then request a delayed `rspamd` reload (port of `mail_plugin_dkim` Rspamd branch). Amavis config paths SHALL NOT be written.

#### Scenario: Enabling DKIM writes keys and maps
- **WHEN** `mail_domain_update` sets `dkim=y` on an active domain with keys in the DB
- **THEN** private/public key files exist under `dkim_path` and both Rspamd maps contain the domain line

#### Scenario: Disabling DKIM removes keys and map lines
- **WHEN** `mail_domain_update` sets `dkim=n` (or `active=n` while dkim was y)
- **THEN** key files for that domain are removed, map lines are removed, and rspamd reload is queued

#### Scenario: Domain rename rewrites DKIM materials
- **WHEN** an active DKIM domain changes `domain` from `old.com` to `new.com`
- **THEN** old key files and map lines are removed and new ones are written for `new.com`

### Requirement: DNS TXT publication via DNSPublisher
When a domain is active with DKIM enabled, the mail domain save path SHALL publish a TXT record through the `DNSPublisher` interface — never by executing SQL against `dns_rr` directly from the mail package. Record name is `<selector>._domainkey.<domain>.`, data is `v=DKIM1; t=s; p=<public-key-without-PEM-headers>`, TTL default 3600. Publication SHALL upsert (delete prior DKIM TXT for old selector/domain, insert/update new, bump parent `dns_soa` serial via existing DNS serial helper) when a managed zone exists. When no zone matches, save succeeds and the response/UI exposes the suggested TXT for manual publication (`dns_published=false`).

#### Scenario: Managed zone receives DKIM TXT
- **WHEN** domain `example.com` enables DKIM and an active `dns_soa` origin `example.com.` exists
- **THEN** a `dns_rr` TXT for `default._domainkey.example.com.` (or the chosen selector) is created with `v=DKIM1` data, SOA serial is bumped, and datalog rows for DNS are written by the DNS layer

#### Scenario: No zone leaves manual TXT only
- **WHEN** DKIM is enabled and no matching `dns_soa` exists
- **THEN** the mail domain is saved, no `dns_rr` row is written, and the API returns the suggested DNS record text

#### Scenario: DKIM disable removes published TXT
- **WHEN** DKIM is turned off for a domain that previously published a TXT
- **THEN** `DNSPublisher.DeleteTXT` removes the DKIM record(s) and the SOA serial is bumped

### Requirement: Selector validation
`dkim_selector` SHALL match lowercase alphanumeric characters only, length 1–63 (port of `mail_domain.tform.php` / `validate_selector`).

#### Scenario: Invalid selector rejected
- **WHEN** a domain is saved with `dkim_selector=Bad_Selector!`
- **THEN** the API returns a validation error and nothing is persisted

### Requirement: Private key not exposed on list endpoints
List endpoints for mail domains SHALL NOT return `dkim_private` (and SHOULD omit or redacted-handle it on non-admin detail if required by security policy); create/update responses may return public key and suggested DNS text but MUST NOT log private key material.

#### Scenario: Domain list omits private key
- **WHEN** `GET /api/mail/domains` is called
- **THEN** items do not include `dkim_private` values
