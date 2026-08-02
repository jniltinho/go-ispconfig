# web-ssl Specification

## Purpose
TBD - created by archiving change add-web-nginx-module. Update Purpose after archive.
## Requirements
### Requirement: Self-signed certificate lifecycle (ssl_action)
The nginx plugin's `ssl` handler SHALL process `web_domain.ssl_action` on insert/update events: `create` generates a private key, CSR and self-signed certificate (openssl, subject from the ssl_* fields) into `<document_root>/ssl/<ssl_domain>.{key,csr,crt}` with the key at mode 0400, and stores their contents in `ssl_request`/`ssl_cert`/`ssl_key`; `save` writes the panel-provided `ssl_cert`/`ssl_key`/`ssl_bundle` contents to those files; `del` removes the files and clears the DB fields. All resulting `web_domain` field writes SHALL NOT produce new `sys_datalog` rows. Certificates whose content matches `.acme.invalid` SHALL be rejected with a datalog error.

#### Scenario: ssl_action=create produces usable files
- **WHEN** a `web_domain_update` arrives with `ssl_action=create` and subject fields filled
- **THEN** key/csr/crt exist under the site's `ssl/` dir, the key has mode 0400, the DB holds their contents and `ssl_action` is reset

#### Scenario: Pasted cert with .acme.invalid is rejected
- **WHEN** `ssl_action=save` carries a certificate containing `.acme.invalid`
- **THEN** no file is written and a datalog error explains the rejection

### Requirement: Let's Encrypt issuance via acme.sh with certbot fallback
When `ssl_letsencrypt=y`, the plugin SHALL obtain a certificate using the first available client — acme.sh preferred, certbot otherwise (port of `use_acme()` detection order) — using webroot validation against the acme challenge path that every rendered vhost serves. The requested domain set SHALL include the main domain plus reachable aliases/subdomains (port of `assemble_domains_to_request`, excluding domains marked `ssl_letsencrypt_exclude`), key type ECDSA ec-256 when the client version supports it, RSA otherwise. Issued cert/key/bundle SHALL be linked into `<document_root>/ssl/` and referenced by the vhost. Issuance failure SHALL leave the previous vhost/certificate untouched and record a datalog error.

#### Scenario: Issuance with acme.sh
- **WHEN** acme.sh is installed and a domain enables `ssl_letsencrypt`
- **THEN** the plugin invokes acme.sh issue+install-cert with webroot validation and the vhost is re-rendered pointing at the Let's Encrypt files

#### Scenario: No client available
- **WHEN** neither acme.sh nor certbot is found
- **THEN** the site stays on its previous SSL state and a datalog error reports the missing client

#### Scenario: Failed issuance does not break the site
- **WHEN** the client exits non-zero (e.g. DNS not pointing at the server)
- **THEN** the vhost keeps its previous certificate configuration and the error is surfaced via datalog error

### Requirement: Scheduled certificate renewal
The daemon SHALL register a daily scheduled job that runs the installed client's renewal (acme.sh `--cron` or `certbot renew`) and schedules a delayed `httpd` reload only when certificates were renewed. Job outcome SHALL be persisted in the scheduler's job bookkeeping.

#### Scenario: Renewal run with nothing due
- **WHEN** the renewal job runs and the client reports no certificate renewed
- **THEN** nginx is not reloaded and the job records a successful run

