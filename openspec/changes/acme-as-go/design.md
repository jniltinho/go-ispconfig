# Design: acme-as-go

The client choice is argued in `proposal.md` (Options A–D, recommendation:
Option D, `golang.org/x/crypto/acme`). This file covers how it is built.

## D1. Why not lego, in one paragraph

Recorded here because the instruction to implement this said "wrap lego", and
this design does not. Both are mature libraries and either would work — the
directive's purpose was "do not hand-roll ACME", which Option D satisfies, since
`x/crypto/acme` is the Go team's RFC 8555 implementation. Measured, lego's core
(without the provider catalogue) costs 4 new modules — `lego`, `go-jose/v4`,
`cenkalti/backoff/v5`, `miekg/dns` — while `golang.org/x/crypto` is **already a
direct dependency** (`go.mod:15`). Lego's differentiator is its 100+ DNS
provider catalogue, and this panel *is* the DNS server, so the catalogue is
weight we would carry and not use. If ARI (renewal-info) or a third-party CA
with awkward quirks later justifies it, swapping the implementation behind the
`internal/acme` seam is a contained change — which is why the seam exists.

## D2. Package shape

`internal/acme` exposes what hosting needs and hides the protocol:

```
Client.Issue(ctx, domains []string) (*Result, error)
Client.Renew(ctx, domains []string) (*Result, error)
Client.Revoke(ctx, domain string, reason int) error
```

`Solver` is the one interface (`Present(domain, token, keyAuth) error`,
`CleanUp(...) error`), with an http-01 implementation now and a dns-01 one
behind the same seam.

Everything above the protocol — which domains go on the certificate, where the
PEMs land, which service reloads — already exists in `internal/nginx/le.go` and
moves rather than gets rewritten.

## D3. http-01 keeps the legacy webroot semantics

One shared webroot for every domain, as the legacy does, because per-site
webroots put the challenge path inside a docroot the operator may have just
changed. `AcmeWebroot` already names it (`internal/nginx/le.go:21`) and both
vhost templates already route `/.well-known/acme-challenge` there. Only the
process writing the token changes.

No listener is bound — nginx or Apache owns :80. `acme.HTTP01ChallengePath` and
`HTTP01ChallengeResponse` give the filename and the body; the solver is a write
and a delete.

## D4. dns-01 uses our own DNS module, not a provider catalogue

Where go-ispconfig differs from every other ACME consumer: **this panel is the
DNS server.** `internal/dns` writes bind zonefiles and `internal/powerdns` the
PowerDNS backend, both driven off `dns_rr` rows through the `sys_datalog` cycle.
So the dns-01 solver is a `dns_rr` insert of the `_acme-challenge` TXT
(`acme.DNS01ChallengeRecord` gives the value), a wait for the daemon to apply
it and for the authoritative nameserver to answer, and a delete on cleanup.

An operator whose DNS is hosted elsewhere stays on http-01. Adding Route53 or
Cloudflare credentials to the panel to solve a challenge for a zone the panel
does not host is a feature looking for a user, and it is the one that would drag
cloud SDKs in.

Per the open question in `proposal.md`, **http-01 ships first** (it is the
legacy behaviour and covers every current user); dns-01 follows as its own task
group, because it is what makes wildcards possible and deserves its own review.

`ponytail: dns-01 serves only zones this panel hosts; add a provider adapter if
an external-DNS user actually appears.`

## D5. Storage, and the two things that must not be lost

Certificates: `/var/lib/go-ispconfig/acme/certs/<main-domain>/` holding
`cert.pem`, `chain.pem`, `fullchain.pem`, `key.pem` — the acme.sh layout, so
`internal/apache2/ssl.go` and the nginx templates need a new path, not a new
shape. Written temp-then-rename, key 0600.

Account: `/var/lib/go-ispconfig/acme/account.key` plus `account.json` (the
registration URI and contact). Separate from the certificates on purpose —
losing a certificate costs a re-issue, losing the account key costs the
rate-limit history attached to it.

**Not in the database.** A private key in `sys_datalog`'s replication path would
be handed to every node in a multi-server install.

## D6. Concurrency

Two writers can race here and both must be handled, because the datalog cycle
applies sites in parallel:

- **Account creation.** Two first-time issuances would each register an account
  and the second would overwrite the first's key, orphaning its rate-limit
  history. Registration happens once under a file lock on the account
  directory, with a re-read after acquiring it.
- **Same-domain issuance.** Two applies of the same site must not both call the
  CA. A per-main-domain lock around the check-then-issue of D7 makes the
  precondition a decision rather than a guess.

`ponytail: file locks, single-host. A multi-server install issues per node,
which is already how the certificates are stored — revisit if issuance ever
moves to the controller.`

## D7. Idempotence is a precondition, not a cache

`Issue` returns early when a stored certificate covers **the same domain set**
with more than 30 days left — same threshold as the legacy cron. On the domain
set, not the main domain: adding an alias must re-issue, and a check keyed on
the main domain alone would not notice.

`ponytail: 30-day threshold hardcoded, matches legacy; make it a server.config
key if someone needs a different window.`

## D8. Rate limits are a design constraint, not an error path

Let's Encrypt's limits are account-wide and punitive (5 failed validations per
hour per account/hostname, 50 certificates per domain per week). A retry loop
over many sites can lock an operator out, and they feel it days later as "the
panel stopped issuing certificates".

So: a failed issuance is recorded per domain with a timestamp, and the next
attempt for that domain is refused locally until a backoff has passed —
exponential from 15 minutes, capped at a day. The CA is never the thing that
tells us to slow down. Failures surface as a real error on the site rather than
a log line, which is what makes the backoff acceptable.

## D9. Testing never touches production Let's Encrypt

- Unit tests against a stub ACME directory (`httptest`) covering the solvers,
  the storage, the locking and the backoff.
- An opt-in integration test against the **staging** directory
  (`https://acme-staging-v02.api.letsencrypt.org/directory`), skipped unless
  `GOISP_ACME_STAGING=1`, so CI never issues anything.

The production directory URL appears in exactly one place, and a test asserts no
test file mentions it.

## D10. Nothing already working breaks

Certificates issued by `acme.sh` (`~/.acme.sh`) or `certbot`
(`/etc/letsencrypt/live`) keep working — the vhost templates read those paths
and this change does not move them. A `[server] acme_client` setting selects
`native` (new default), `acme.sh` or `certbot`; the existing detection stays as
the fallback, so an operator with a working custom hook has somewhere to stand.
