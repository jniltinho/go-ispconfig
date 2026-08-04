## 1. `internal/acme` — the client

- [x] 1.1 Add `github.com/go-acme/lego/v4` and wrap it in `internal/acme/client.go` (directory discovery, account registration, obtain). Production CA URL in exactly one place.
- [x] 1.2 Account storage at `/etc/letsencrypt/accounts/<server-id>/` (`account.key` + `account.json`), 0600, file-locked registration. Tests: key reuse, concurrent register once.
- [x] 1.3 `Issue(domains)` writes certbot layout under `/etc/letsencrypt/{live,archive,renewal}` temp-then-rename, key 0600.
- [x] 1.4 `RenewDue` walks `renewal/*.conf` and re-issues inside the 30-day window.
- [x] 1.5 30-day / domain-set precondition under per-lineage lock. Tests: fresh cert skips, added alias re-issues.
- [x] 1.6 Backoff ledger per lineage (15m → 24h cap). Tests: refuse locally, double, cap, clear on success.

## 2. http-01

- [x] 2.1 `webrootSolver` writes `/.well-known/acme-challenge/<token>` under `/usr/local/ispconfig/interface/acme`. No listener. Tests against temp webroot.
- [x] 2.2 Both vhost templates already route the challenge path (golden tests unchanged).

## 3. Integration with the two web plugins

- [x] 3.1 `internal/nginx/le.go`: issuance delegates to `acme.Manager` (no acme.sh/certbot shell-out).
- [x] 3.2 `internal/apache2/acme.go`: issue on demand before vhost render; `apache2ctl graceful` via services registry.
- [x] 3.3 Vhosts keep reading `<docroot>/ssl/<domain>-le.{crt,key}` symlinks into `/etc/letsencrypt/live/`.
- [x] 3.4 `internal/web/le_renew.go`: native `RenewDue` branch; daily job reloads nginx or Apache.

## 4. Config, install, docs

- [x] 4.1 No external ACME client install step — native is the only path (`acmeStep` removed).
- [x] 4.2 Installer no longer offers `acme.sh` / `certbot` packages.
- [ ] 4.3 `docs/acme.md` (deferred — design already in openspec).
- [x] 4.4 No new API endpoints in this slice.

## 5. dns-01 (second wave)

- [x] 5.1 Interface + Route53 / Cloudflare / DigitalOcean adapters via lego providers (`internal/acme/dns.go`).
- [ ] 5.2 Panel-hosted dns-01 over `dns_rr` (not in this PR).
- [ ] 5.3 Wildcard issuance (needs 5.2).

## 6. Validation

- [x] 6.1 Unit tests: solvers, storage, locking, backoff, manager, state — no production CA URL in tests.
- [ ] 6.2 Staging integration behind `GOISP_ACME_STAGING=1` (skipped by default).
- [x] 6.3 Lab validates challenge file placement only (documented in tasks 6.3).
- [ ] 6.4 Cross-review with qwen3.8-max before merge.

## 7. Datastore

- [x] 7.1 `/var/lib/go-ispconfig/acme/state.json` — domain → `{provider, last_renewal, last_error}`.

## 8. Datalog

- [x] 8.1 `web_domain` ssl_letsencrypt changes trigger issuance via existing nginx/apache2 `maybeRequestLE` handlers (no separate datalog handler needed).
