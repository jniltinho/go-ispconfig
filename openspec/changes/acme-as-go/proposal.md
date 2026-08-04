# Proposal: acme-as-go

## Why

Certificate **issuance** in this port is nginx-only and shells out. Everything
else about Let's Encrypt has already been made shared — `internal/web/le_renew.go`
holds the renewal job, `DetectACME` finds either client, and both `internal/nginx`
and `internal/apache2` register it — but the code that actually asks a CA for a
certificate lives in `internal/nginx/le.go` and nowhere else.

Two consequences:

**1. An Apache-only server cannot obtain a certificate from this panel.**
`internal/apache2/ssl.go` reads `<domain>-le.crt` / `-le.key` off disk and
`le_renew.go` renews what is already there, but nothing issues the first one.
Ticking "Let's Encrypt" on a site of an Apache node silently produces a vhost
pointing at files that do not exist.

**2. Issuance depends on a binary we do not ship.** `internal/nginx/le.go` is a
390-line wrapper that finds `acme.sh` or `certbot`, builds an argv and parses
what comes back. If neither is installed — and `install-acme` is **off by
default**, so on a default install neither is — the feature is inert. The lab
VM at `192.168.56.10` provisioned today reports exactly that:
`[install-acme] skipped: acme client install not requested`.

Doing it in Go removes the argv/stdout contract, makes issuance available to
both web plugins through one path, and puts the result where the existing
renewal job and both SSL readers already look.

## What the legacy actually does

Checked in `base/ispconfig3_install/`, because the shape of the port should be
argued against the real thing rather than against a memory of it:

- **There is no `server/scripts/acme.sh`.** The tree ships no ACME client. It
  *detects* one: `letsencrypt.inc.php:36` runs
  `which acme.sh /usr/local/ispconfig/server/scripts/acme.sh /root/.acme.sh/acme.sh`,
  and falls back to a certbot probe at line 122.
- **Issuance is one `--issue` call**, `letsencrypt.inc.php:88`:
  `acme.sh --issue <domains> -w /usr/local/ispconfig/interface/acme --always-force-new-domain-key`,
  followed by `--install-cert … --reloadcmd <service reload>` at line 91.
  The certbot branch (line 191) is the same idea:
  `certbot certonly -n --text --agree-tos --authenticator webroot --webroot-map <json>`.
- **http-01 only, one shared webroot** for every domain:
  `/usr/local/ispconfig/interface/acme`. No dns-01 anywhere.
- **The post-issue work is a shell hook**, `letsencrypt_renew_hook.sh`: rebuild
  `ispserver.pem` from key+crt, `chmod 600`, then restart pure-ftpd, monit,
  postfix, dovecot, mysql, nginx/apache — gated on `Le_Domain == $(hostname -f)`
  for acme.sh or `RENEWED_DOMAINS` for certbot.

So the legacy's contract is narrow: **http-01, one webroot, reload on success.**
That is the behaviour to match; the rest is ours to design.

Two things follow from this, and they shape the decision below. First, **the
legacy is not an ACME implementation** — porting acme.sh would not bring this
port closer to ISPConfig, it would take it somewhere neither codebase has been.
Second, **the legacy never does dns-01**, which matters because dns-01 provider
support is the entire reason the obvious library exists.

## The decision: which ACME client

### Option A — keep shelling out, extend it to Apache

The status-quo fix: move the `leClient` issuance path out of `internal/nginx`
into `internal/web`, next to the renewal that is already shared, and add
`python3-certbot-nginx` / `python3-certbot-apache` to the installer. Both costs
above stay — the install is still not self-contained and failures are still an
exit status. Cheap, and the honest fallback if the work below is not wanted.

### Option B — port acme.sh to Go

**Not recommended.** ~10k lines of shell semantics (CA quirks, provider hooks,
its own state-directory format) that we would then own forever, to reach a place
the legacy panel never reached. See the previous section: the PHP shells out
too.

### Option C — `golang.org/x/crypto/acme`

The close call, and it has to be argued rather than dismissed: **it is already a
direct dependency of this module** (`go.mod:15`, `golang.org/x/crypto v0.54.0`).
Rung 5 of the ladder — an installed dependency that solves the problem. It is a
complete RFC 8555 client maintained by the Go team, with EAB support, and it
covers both challenge types (`HTTP01ChallengeResponse`, `DNS01ChallengeRecord`,
`AuthorizeOrder`, `WaitAuthorization`, `CreateOrderCert`, `FetchCert`).

Rejected on one argument: it is a *protocol* client, not a *certificate* client.
The order/authz/challenge/finalize/poll loop, the retry policy and the CSR
assembly are ours to write — call it 400 lines whose failure mode is a Let's
Encrypt rate limit. Those limits are punitive and account-wide (5 failed
validations per hour), and the operator feels them days later as "the panel
stopped issuing certificates", with nothing in the logs pointing at our retry
bug. This is the case where the shortest diff and the correct one diverge.

Explicitly **not** `acme/autocert` either: it terminates TLS inside the serving
process with its own cache. We need PEMs on disk for nginx and Apache to read.

### Option D — `github.com/go-acme/lego/v4` ← recommended

MIT, the client behind Traefik. (One correction to the premise this proposal
started from: lego is maintained by the go-acme org — the "Microsoft, Google"
attribution is not right, and the decision should not rest on it.) It owns the
loop Option C would make us write, plus ARI, EAB and per-CA quirks.

The usual objection is weight — the DNS provider catalogue pulls the AWS, Azure
and Google SDKs. **That objection was measured and does not hold**, because Go
links only what is imported. Importing just `lego`, `certificate`,
`challenge/http01` and `challenge/dns01` adds **four modules**: lego itself,
`go-jose/v4` (JWS), `cenkalti/backoff/v5` (retry), `miekg/dns` (dns-01
pre-check). The provider catalogue is never imported — and per the legacy
analysis above we do not want it: **this panel is its own DNS server**, so
dns-01 here is a `dns_rr` insert through the existing datalog cycle, not a
third-party API credential.

See `design.md` D1 for the full decision record and D3 for the dns-01 shape.

## What changes

A new `internal/acme` owning issuance over lego, with `internal/nginx` and
`internal/apache2` as its two consumers through the existing `internal/web`
seam. Renewal keeps the shared job and gains a native branch, so a host with
neither `acme.sh` nor `certbot` still renews.

## Migration and the escape hatch

Certificates already issued by `acme.sh` (`~/.acme.sh`) or `certbot`
(`/etc/letsencrypt/live`) **keep working untouched** — the vhost templates read
those paths and this change does not move them. The native client takes over for
new issuance and for renewals of what it issued.

The external-client path is **kept, not deleted**: a `[server] acme_client`
setting selects `native` (new default), `acme.sh` or `certbot`, and the existing
detection stays as the fallback. An operator with a working acme.sh setup and a
custom hook has somewhere to stand.

## Impact

- New: `internal/acme` (client, http-01 solver, storage), `internal/apache2/acme.go`.
- Changed: `internal/nginx/le.go` delegates issuance; `internal/web/le_renew.go`
  gains the native branch; `internal/installer/acmestep.go` no longer has to
  install anything for the default path.
- New dependency: `github.com/go-acme/lego/v4` (MIT) plus three small transitive
  modules; the DNS provider catalogue is never imported (design D1).
- Storage: certificates under `/var/lib/go-ispconfig/letsencrypt/<domain>/` in
  the acme.sh layout both SSL readers already understand; the account key at
  `/etc/go-ispconfig/acme/account.key`. **Not** in the database — a private key
  in `sys_datalog`'s replication path is not something to hand to every node.
- The panel gains a real error to display when issuance fails.
- Out of scope: the panel's own certificate stays self-signed, as today.
  Issuing for the panel FQDN is a follow-up.

## Open questions for review

1. **dns-01 scope.** Serving it from our own DNS is the interesting capability
   (it is what gets wildcards working) and the legacy has nothing like it. It
   only applies when the zone is hosted here, and it needs propagation waiting.
   Ship http-01 first and dns-01 second, or together?
2. **CA choice.** Let's Encrypt only, or expose the directory URL so ZeroSSL /
   Buypass work? One config field, but EAB credentials are extra surface.
3. **The panel's own certificate** stays self-signed here (out of scope). The
   legacy rebuilds `ispserver.pem` and restarts five services on renewal; worth
   doing, but it is a second change.

## Status

**Design proposal — awaiting approval before implementation.** No code written.
`tasks.md` is a plan, not work in progress. If the answer is Option A instead,
this reduces to two tasks plus the certbot plugin packages.
