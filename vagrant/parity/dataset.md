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

- Mail domain `parity1.goisp.test`, mailbox `info@parity1.goisp.test`.
- Skipped entirely when the legacy VM was provisioned with `--no-mail`
  (no mail stack): record it in the parity report as "not applicable".

## Screenshots

Both panels, after the flows: list views of clients, sites and zones →
`docs/prints/legacy-*.png` and `docs/prints/goisp-*.png` (never committed).
