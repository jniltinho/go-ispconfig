# Proposal: Let's Encrypt issuance for the Apache2 web module

## Why

An Apache node cannot obtain a certificate. Not "obtains a worse one" — it
never issues at all.

`internal/apache2/ssl.go` reads `<domain>-le.crt` / `-le.key` / `-le.bundle`
out of the SSL directory and renders them into the vhost, and
`internal/apache2/le_renew.go` registers the daily renewal job. Both assume
the files are already there. The code that *creates* them —
`leClient`, `requestCert` and everything they call — lives in
`internal/nginx/le.go` and hangs off `*nginx.Plugin`. Only one web plugin is
ever loaded (`cmd/daemon.go`), so on a node installed with
`--web-server apache` a site with `ssl_letsencrypt = y` gets:

1. `maybeRequestLE` — never called, there is no such method on the Apache plugin;
2. the vhost render — finds no `-le.crt`, falls back to the self-signed pair
   or to plain HTTP;
3. the renewal job — runs `acme.sh --cron` / `certbot renew` nightly over an
   empty certificate set and reports success.

The failure is silent in the worst way: the panel shows the checkbox ticked,
the daemon logs nothing, the renewal job is green, and the site serves the
wrong certificate. The operator has no signal short of opening the site.

## What changes

Move the issuance out of `internal/nginx` into `internal/web`, where the
*renewal* half already lives (`internal/web/le_renew.go`, shared by both
plugins since the Apache module landed), and call it from both.

Nothing about the logic is nginx-specific. Certificates are issued with
`--authenticator webroot` (`certbotArgs`) / `--webroot` (`acmeIssueArgs`)
against `/usr/local/ispconfig/interface/acme`, a path both web servers serve
under `/.well-known/acme-challenge`. The nginx coupling is incidental: the
`leClient` struct holds a `*nginx.Plugin` purely to reach four fields —
`db`, `runner`, `log` and `acmeWebroot()`.

The move is mechanical, and the two plugins already agree on the data shape:
`internal/nginx/data.go:12` and `internal/apache2/data.go:17` both declare
the identical `type row map[string]any` with the same `str`/`num` helpers.

## Scope

- `internal/web`: gains the issuer (~390 lines from `le.go`) plus the shared
  `Row` type and its accessors, parameterised on db/runner/log/webroot and on
  the module name used in error text.
- `internal/nginx`: `le.go` shrinks to the plugin-shaped wrapper; `le_test.go`
  (~229 lines) moves with the logic it covers.
- `internal/apache2`: gains `maybeRequestLE` on the site path, mirroring
  `internal/nginx/handlers.go:101`, and sets `sslChanged` so the vhost
  re-renders with the new files.

## What this is not

Not a change to *how* certificates are issued. Same clients (acme.sh
preferred, certbot fallback), same webroot authenticator, same version gates
for ECDSA, same 100-domain cap, same reachability probe. A node installed
with `--web-server nginx` must behave byte for byte as it does today — that
is the acceptance bar for the extraction, and what the moved tests assert.

The certbot **web-server plugins** are out of scope and deliberately so.
`python3-certbot-nginx` / `python3-certbot-apache` are installed by
`acmeStep` for the operator's own use (replacing the self-signed panel
certificate by hand); this port issues through webroot and needs neither.

## Validation

The lab has no Apache node — `apache-test` is `not created` in
`vagrant status`. Bringing one up is part of the work, not a precondition to
be assumed: an extraction that is only ever exercised on the nginx path
proves nothing about the half of the change that motivated it.
