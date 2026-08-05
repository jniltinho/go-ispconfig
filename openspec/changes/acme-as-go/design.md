# Design: acme-as-go

`proposal.md` weighs the options; D1 records which was chosen. This file covers
how it is built.

## D1. lego, decided

Wrap `github.com/go-acme/lego/v4`. The alternative was
`golang.org/x/crypto/acme`, already a direct dependency and costing no new
modules; the owner chose lego, and this records the decision rather than
re-arguing it.

Measured with `go list -deps` on a program importing only `lego`,
`certificate`, `challenge/http01` and `registration`, the core links **3 new
modules** — `go-jose/v4`, `cenkalti/backoff/v5`, `miekg/dns` — plus lego
itself; `x/crypto`, `x/net`, `x/text` and `x/sys` were already direct
dependencies. The 231 modules `go list -m all` reports is the *requirement*
graph: lego ships its 100+ DNS providers in one module, so every cloud SDK is
required-but-not-linked.

What lego buys beyond the protocol is the provider catalogue, which D4 said we
would not need because this panel *is* the DNS server. That held for
panel-hosted zones and stopped holding for external ones: `internal/acme/dns.go`
now adapts Route 53, Cloudflare and DigitalOcean straight from lego, which is
the case a hand-rolled client would have had to grow.

Neither library needs anything on the box — the property that lets D11 delete
the installer's ACME step outright.

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

Certificates go where certbot puts them, because that is what an adopted
install already has on disk and what third-party tooling, monitoring and every
Let's Encrypt tutorial expects:

```
/etc/letsencrypt/
├── live/<main-domain>/           symlinks into ../../archive/<main-domain>/
│   ├── cert.pem  chain.pem  fullchain.pem  privkey.pem
├── archive/<main-domain>/        every generation kept, N-suffixed
│   ├── cert1.pem  chain1.pem  fullchain1.pem  privkey1.pem
│   └── cert2.pem  …
└── renewal/<main-domain>.conf    issuance metadata, INI, certbot-shaped
```

`live/` is a symlink farm into `archive/`, exactly as certbot does it: a renewal
writes generation N+1 and repoints the four symlinks, so a reader that opens
`live/…/fullchain.pem` never sees a half-written file and a bad renewal can be
rolled back by repointing. Directories 0755, `archive/*/privkey*.pem` 0600,
written temp-then-rename.

`renewal/<domain>.conf` is not decoration — it is the **discovery contract**.
ISPConfig does not glob `live/`; `get_certificate_list()`
(`letsencrypt.inc.php:614`) walks the renewal directory, parses each `.conf` as
`key = value` until the `[[webroot_map]]` marker, and takes the `cert`,
`privkey`, `chain` and `fullchain` paths from it, discarding any candidate
whose four files are not all readable. So the interop requirement is precise
and testable: write those four keys with absolute paths, terminate before
`[[webroot_map]]`, and the legacy panel's own discovery finds our certificates
unmodified. A layout that merely looks like certbot's but omits the renewal
file is invisible to it.

The lineage name matters for the same reason. ISPConfig issues ECDSA
certificates as `--cert-name <domain>_ecc` (`installer_base.lib.php:3454`), so
an adopted host may already have both `<domain>` and `<domain>_ecc` lineages.
Ours must pick the same name for the same key type or it will create a second,
competing lineage next to the one already being renewed.

Account keys are the one deliberate divergence:
`/etc/letsencrypt/accounts/<server-id>/account.{key,json}`. Certbot has no
multi-account concept and keys the directory by CA URL; this panel is
per-server, and the account key is what carries the rate-limit history — losing
a certificate costs a re-issue, losing the account key costs that history.

**The vhosts do not change, and that is the point.** Both templates render
`<docroot>/ssl/<domain>-le.crt|key|bundle`
(`internal/nginx/le.go:242`, `internal/apache2/vhost.go:182`) — the ISPConfig
layout. Those stay, as **symlinks into `live/`**, which is precisely what the
legacy does on its certbot path: `letsencrypt.inc.php:496` calls `link_file()`
for privkey, chain and fullchain after certbot writes them. Repointing the
templates at `/etc/letsencrypt/live/` instead would rewrite every vhost on
upgrade and break the acme.sh path, which installs *copies* at the site ssl
path rather than symlinks (`letsencrypt.inc.php:478`). Writing certbot's layout
and linking from the site ssl dir is what makes an adopted install zero-touch:
the symlinks it already has keep resolving.

The challenge webroot also stays at `/usr/local/ispconfig/interface/acme` —
both vhost templates hardcode it (`nginx_vhost.conf.master:92`), and it is what
the legacy passes as `--webroot-path`. Moving it to `/var/www/letsencrypt`
would mean re-rendering every vhost to chase a path that is a per-site
convention, not a certbot default.

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

## D11. The installer stops shipping an ACME client

An in-process client makes the external one dead weight, and this holds for
either library — it is a consequence of "the daemon speaks ACME", not of which
Go package it speaks it with.

`acmeStep` goes away, and with it: the `--acme` / `--acme-client` answers, the
`curl` pull that only exists to run acme.sh's installer, the `apt install
certbot`, and the sudoers/service surface an external client would otherwise
need. What is left is a Go dependency, resolved at build time, versioned in
`go.mod` and auditable with `govulncheck` — none of which is true of 10k lines
of POSIX shell fetched over `curl | sh`.

Measured, so the trade is not argued from adjectives (`go list -deps` on a
program importing only `lego`, `certificate`, `challenge/http01` and
`registration`):

| | External binary | New Go modules linked | Audit surface |
|---|---|---|---|
| `acme.sh` | yes, fetched by `curl \| sh` | 0 | ~10k lines of shell, unversioned |
| `certbot` | yes, apt + Python runtime | 0 | ~50k lines Python + plugin venv |
| `lego` core | **no** | 3 new (`go-jose/v4`, `cenkalti/backoff/v5`, `miekg/dns`) + lego itself; `x/crypto`, `x/net`, `x/text`, `x/sys` already direct deps | one module, pinned, `govulncheck`-visible |
| `x/crypto/acme` | **no** | 0 — already a direct dependency | ditto, Go team maintained |

The 231-module figure `go list -m all` prints for lego is the *requirement*
graph: lego ships its 100+ DNS providers in one module, so every cloud SDK is
required-but-not-linked. Importing only the core links 33 non-stdlib packages.
Neither library needs anything on the box.

Detection of an existing client stays as the D10 fallback, but nothing installs
one any more. An operator who wants acme.sh keeps it — it is theirs.

## D10. Nothing already working breaks

Certificates issued by `acme.sh` (`~/.acme.sh`) or `certbot`
(`/etc/letsencrypt/live`) keep working — the vhost templates read those paths
and this change does not move them. A `[server] acme_client` setting selects
`native` (new default), `acme.sh` or `certbot`; the existing detection stays as
the fallback, so an operator with a working custom hook has somewhere to stand.
