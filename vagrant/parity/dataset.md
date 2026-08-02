# Parity dataset

Canonical records created on **both** panels (legacy PHP ISPConfig at
`https://192.168.56.20:8080`, go-ispconfig at `https://192.168.56.10:8080`)
by the agent-browser flows. Parity scope: **clients + sites + DNS** —
email accounts are created on the legacy side only (future mail-module
baseline / migration-source data) and are never compared.

## Clients (legacy only until add-client-module ships)

go-ispconfig has no client module yet, so clients exist only on the legacy
panel as baseline/migration-source data; the `client` table diff is an
allowlisted intended difference (see `intended-differences.txt`). On
go-ispconfig the sites/zones are created as admin.

| # | Contact name      | Username  | Email                  |
|---|-------------------|-----------|------------------------|
| 1 | Parity Client One | pclient1  | pclient1@goisp.test    |
| 2 | Parity Client Two | pclient2  | pclient2@goisp.test    |

Password (legacy panel, test rig only): `ParityPw2026!`

## Websites (both panels)

| Domain             | Type  | Notes                       |
|--------------------|-------|-----------------------------|
| parity1.goisp.test | vhost | defaults, auto-subdomain www|
| parity2.goisp.test | vhost | defaults                    |

## DNS zones (owner: pclient1)

Created via the DNS wizard (template "Default") on both panels:

| Zone                | IP            | NS1/NS2                          | Email                |
|---------------------|---------------|----------------------------------|----------------------|
| parity1.goisp.test. | 192.168.56.20 / .10 (own server IP) | ns1/ns2.goisp.test | hostmaster@goisp.test |

## Email (legacy only, excluded from parity)

- The legacy VM now runs the full mail stack (postfix/dovecot/rspamd +
  Roundcube, openspec add-legacy-test-lab); its mail fixtures live in
  `vagrant/lab/dataset.md` and stay out of parity scope until the
  go-ispconfig mail module ships.
- The legacy VM also carries the standing-lab entities (lab clients,
  sites, zones — `vagrant/lab/`); the parity suite scopes its queries to
  the parity records (`pclient%` / `parity%`) and ignores them.
- Baseline DB dump of the legacy record set (client, web_domain, dns_soa,
  dns_rr, mail_*): `vagrant/parity/baseline-legacy.sql.gz` (gitignored,
  regenerate with mysqldump on the legacy guest).

## Screenshots

Both panels, after the flows: list views of clients, sites and zones →
`docs/prints/legacy-*.png` and `docs/prints/goisp-*.png` (never committed).
