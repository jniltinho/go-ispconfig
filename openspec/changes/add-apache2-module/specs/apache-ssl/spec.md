# apache-ssl

## ADDED Requirements

### Requirement: SSL folder and certificate paths
The plugin SHALL ensure `{document_root}/ssl` exists before any other SSL work, for
every `web_domain` event on a `vhost`, `vhostsubdomain` or `vhostalias` row. The
certificate domain SHALL be `ssl_domain` when non-empty and `domain` otherwise, and the
file paths SHALL be `{document_root}/ssl/{ssl_domain}.{key,csr,crt,bundle}`, switching to
`{ssl_domain}-le.{key,crt,bundle}` (no csr) when `ssl='y'` and `ssl_letsencrypt='y'`
(port of `letsencrypt.inc.php::get_website_certificate_paths()` and the ssl handler in
`apache2_plugin.inc.php`).

#### Scenario: SSL directory is created for a new site
- **WHEN** `web_domain_insert` fires for a vhost-type row
- **THEN** `{document_root}/ssl` exists before the vhost is rendered

#### Scenario: Let's Encrypt sites use the -le file names
- **WHEN** a site has `ssl='y'` and `ssl_letsencrypt='y'` for domain `example.tld`
- **THEN** the vhost references `{document_root}/ssl/example.tld-le.crt` and `example.tld-le.key`

#### Scenario: ssl_domain overrides the site domain
- **WHEN** a site has `domain='example.tld'` and `ssl_domain='www.example.tld'`
- **THEN** the certificate paths are built from `www.example.tld`

### Requirement: ssl_action create generates a self-signed certificate
When `ssl_action='create'` and `mirror_server_id=0` the plugin SHALL back up any
existing key, csr and crt to `.bak` (key at mode 0400), generate an OpenSSL config
carrying the subject fields from `ssl_country`, `ssl_state`, `ssl_locality`,
`ssl_organisation` and `ssl_organisation_unit` plus `CN=<ssl_domain>` and
`emailAddress=webmaster@<domain>`, generate a 4096-bit RSA self-signed certificate valid
3650 days plus a matching CSR, sign it with the local CA when
`{CA_path}/openssl.cnf` exists (falling back to the self-signed result when CA signing
produces an empty file), chmod the key to 0400, delete the temporary config, random and
extension files, write `ssl_request`, `ssl_cert` and `ssl_key` back to the database and
clear `ssl_action` (port of `apache2_plugin.inc.php::ssl()`).

#### Scenario: Create produces a key and certificate
- **WHEN** `ssl_action='create'` on a non-mirror server
- **THEN** the key and crt files exist under `{document_root}/ssl`, the key is mode 0400, the DB row holds the generated material and `ssl_action` is empty

#### Scenario: Mirror servers do not generate certificates
- **WHEN** `ssl_action='create'` and `mirror_server_id` is greater than zero
- **THEN** no OpenSSL command is executed and no files are written

#### Scenario: Failed CA signing falls back to self-signed
- **WHEN** a CA config exists but `openssl ca` produces a zero-length certificate
- **THEN** the self-signed certificate is generated instead and the failure is logged

### Requirement: ssl_action save validates before writing
When `ssl_action='save'` the plugin SHALL reject the submission and clear `ssl_action`
without writing files if the supplied key contains `Proc-Type: 4,ENCRYPTED`, or if the
certificate text contains `.acme.invalid`, logging a warning and recording a datalog
error in both cases. On a valid submission it SHALL back up key, csr, crt and bundle to
`~` copies, write the CSR when supplied, write the certificate — concatenating the
bundle into the crt file on Apache 2.4.8 and newer, or writing a separate bundle file on
older versions — write the key at mode 0400 when supplied or import the on-disk key into
the database when not, and clear `ssl_action` (port of
`apache2_plugin.inc.php::ssl()`).

#### Scenario: Encrypted private key is rejected
- **WHEN** the submitted `ssl_key` contains `Proc-Type: 4,ENCRYPTED`
- **THEN** no files are written, a datalog error is recorded and `ssl_action` is cleared

#### Scenario: acme.invalid certificate is rejected
- **WHEN** the certificate on disk contains a `.acme.invalid` subject
- **THEN** the save is aborted, a datalog error is recorded and `ssl_action` is cleared

#### Scenario: Bundle is concatenated on modern Apache
- **WHEN** a certificate and bundle are saved on Apache 2.4.58
- **THEN** the crt file contains the certificate followed by the bundle and no separate bundle file is written

#### Scenario: Empty key field imports the on-disk key
- **WHEN** `ssl_action='save'` with a certificate but an empty `ssl_key`
- **THEN** the existing key file is read and stored into the database

### Requirement: ssl_action del removes certificate material
When `ssl_action='del'` the plugin SHALL revoke the certificate through the local CA
when `{CA_path}/openssl.cnf` exists and is not a symlink, remove the csr, crt and bundle
files, clear `ssl_request` and `ssl_cert` in the database for that domain and server, and
clear `ssl_action` (port of `apache2_plugin.inc.php::ssl()`).

#### Scenario: Deleting a certificate clears files and columns
- **WHEN** `ssl_action='del'` fires for a site
- **THEN** the csr, crt and bundle files are removed and `ssl_request`, `ssl_cert` and `ssl_action` are empty in the database

### Requirement: SSL VirtualHost block generation
The renderer SHALL add an SSL entry to the `vhosts` loop only when `ssl='y'`,
`ssl_domain` is non-empty, and both the crt and key files exist and are non-empty. The
SSL block SHALL emit, inside `<IfModule mod_ssl.c>`, `SSLEngine on`,
`SSLProtocol All -SSLv2 -SSLv3 -TLSv1 -TLSv1.1`, `SSLHonorCipherOrder on`,
`SSLCertificateFile` and `SSLCertificateKeyFile`. It SHALL additionally emit, outside
`<IfModule mod_ssl.c>` but inside the `<VirtualHost>`, `Protocols h2 http/1.1` under
`<IfModule mod_http2.c>` and the Brotli output filter under `<IfModule mod_brotli.c>`
(port of `vhost.conf.master` lines 52–106).

#### Scenario: Valid certificate produces a 443 block
- **WHEN** a site has `ssl='y'` with a non-empty crt and key on disk
- **THEN** the rendered file contains a `<VirtualHost ip:443>` block with `SSLEngine on` and the certificate paths

#### Scenario: Zero-length certificate suppresses the SSL block
- **WHEN** the crt file exists but is zero bytes
- **THEN** no `:443` block is emitted and the site continues to serve over HTTP

### Requirement: Separate bundle file only below Apache 2.4.8
The renderer SHALL emit `SSLCertificateChainFile <bundle>` only when a bundle file
exists on disk **and** the detected Apache version is below 2.4.8 (port of
`apache2_plugin.inc.php::update()` `has_bundle_cert` assignment and `vhost.conf.master`
lines 100–104).

#### Scenario: Modern Apache omits the chain file directive
- **WHEN** a bundle file exists and Apache is 2.4.58
- **THEN** the vhost contains no `SSLCertificateChainFile` directive

### Requirement: OCSP stapling gated on the certificate's OCSP URI
Before rendering, the plugin SHALL run `openssl x509 -noout -ocsp_uri` against the
certificate when `ssl='y'` and `ssl_domain` is non-empty, and SHALL set the stapling
flag only when the output is non-empty. When set and Apache is 2.4 or newer, the SSL
block SHALL emit `SSLUseStapling on`, `SSLStaplingResponderTimeout 5` and
`SSLStaplingReturnResponderErrors off`, and `SSLStaplingCache shmcb:/var/run/ocsp(128000)`
SHALL be emitted inside an `<IfModule mod_ssl.c>` block placed **after** the closing
`</VirtualHost>` (port of `apache2_plugin.inc.php::update()` OCSP probe and
`vhost.conf.master` lines 92–98 and 599–607).

#### Scenario: Certificate without an OCSP URI omits stapling
- **WHEN** the certificate carries no OCSP responder URI
- **THEN** no `SSLUseStapling` and no `SSLStaplingCache` directives are emitted

#### Scenario: Stapling cache is emitted at server scope
- **WHEN** stapling is enabled for an SSL site
- **THEN** `SSLStaplingCache` appears after `</VirtualHost>`, not inside it

### Requirement: Let's Encrypt issuance is attempted before the vhost is written
The plugin SHALL request a Let's Encrypt certificate before rendering the vhost when
`ssl='y'`, `ssl_letsencrypt='y'`, `mirror_server_id=0`, and at least one of the
following holds: the previous `ssl` or `ssl_letsencrypt` was `n`, the domain changed, the
subdomain changed, or the request was flagged by a child-row cascade. On success it
SHALL clear `ssl_request`, `ssl_cert`, `ssl_key` and `ssl_action` in the database,
leaving the files on disk authoritative. On failure it SHALL set `ssl_letsencrypt='n'`
(and `ssl='n'` when the previous `ssl` was `n`) and persist that, so the next event does
not retry in a loop (port of `apache2_plugin.inc.php::update()` Let's Encrypt block).

#### Scenario: Enabling Let's Encrypt requests a certificate
- **WHEN** a site is updated from `ssl_letsencrypt='n'` to `'y'` with `ssl='y'`
- **THEN** a certificate request is issued before the vhost is rendered

#### Scenario: Failed issuance disables Let's Encrypt
- **WHEN** the certificate request fails on a site whose previous `ssl` was `n`
- **THEN** `ssl` and `ssl_letsencrypt` are both persisted as `n` and the site renders HTTP-only

#### Scenario: Adding an alias renews the parent certificate
- **WHEN** an alias row is added under a Let's Encrypt site
- **THEN** the parent's update is flagged for re-issuance and the new name is included in the request

### Requirement: ACME challenge is reachable
The installer SHALL provision `{apache_config_dir}/conf-available/999-acme.conf`
containing `Alias /.well-known/acme-challenge` pointing at the ACME webroot plus a
`<Directory>` grant, and SHALL enable it with `a2enconf`. The renderer SHALL emit the
`[END]` acme-challenge rewrite guard as the first rule of the rewrite block on Apache
2.4 and newer. Both SHALL be present for a site to be renewable (port of
`install/tpl/apache_acme.conf.master` and `vhost.conf.master` lines 513–516).

#### Scenario: ACME alias is enabled at install time
- **WHEN** `install --web-server apache2` completes
- **THEN** `conf-enabled/999-acme.conf` exists and `apache2ctl configtest` passes

#### Scenario: Redirects never shadow the challenge path
- **WHEN** a site with a catch-all redirect is rendered
- **THEN** the acme-challenge `RewriteRule ^ - [END]` precedes every redirect rule

### Requirement: Certificate removal on site delete
The plugin SHALL, when `le_delete_on_site_remove='y'` and the deleted row had `ssl='y'`
and `ssl_letsencrypt='y'` with a non-empty document root, extract the
certificate's serial number, locate the matching entry in the ACME client's certificate
list, and remove it, so renewal does not keep failing for a site that no longer exists
(port of `apache2_plugin.inc.php::delete()` Let's Encrypt block).

#### Scenario: Deleting a Let's Encrypt site removes its certificate
- **WHEN** a site with a Let's Encrypt certificate is deleted and `le_delete_on_site_remove='y'`
- **THEN** the matching certificate is removed from the ACME client store

#### Scenario: Deletion without the setting leaves the certificate
- **WHEN** `le_delete_on_site_remove` is not `y`
- **THEN** no ACME client command is executed during deletion

### Requirement: SSL material is rolled back with the vhost
The plugin SHALL, when the config check fails after a change in which the certificate
was modified, copy the failing key, crt, csr and bundle to `.err`, restore each from its
`~` backup, and restart Apache again. After a successful apply it SHALL delete the `~`
backups of all four files and the `~` backup of the vhost (port of
`apache2_plugin.inc.php::update()` rollback block).

#### Scenario: Bad certificate is rolled back
- **WHEN** a certificate save is followed by a failed Apache restart
- **THEN** the previous key, crt, csr and bundle are restored, the failing versions are kept as `.err`, and Apache is restarted

#### Scenario: Successful apply cleans up backups
- **WHEN** the vhost and certificate are applied and Apache stays up
- **THEN** no `~` backup remains for the vhost, key, crt, csr or bundle
