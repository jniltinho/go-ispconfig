## 1. `internal/acme` — the client

- [x] 1.1 Wrap `github.com/go-acme/lego/v4` in `internal/acme` (directory discovery, account registration, obtain). The production CA URL appears in exactly one place, asserted by a test.
- [x] 1.2 Account storage at `/etc/letsencrypt/accounts/<server-id>/account.{key,json}`, 0600 on first use and reused after, registration serialised. Tests: a second load reuses the key.
- [x] 1.3 `internal/acme/storage.go` implementing the certbot layout of D5: write generation N+1 into `archive/<lineage>/`, repoint the four `live/<lineage>/` symlinks in one step, then write `renewal/<lineage>.conf` with the `cert`/`privkey`/`chain`/`fullchain` absolute paths the legacy's `get_certificate_list()` parses. Lineage follows `<domain>` / `<domain>_ecc` per key type. Temp-then-rename, `privkey*.pem` 0600. Tests: generation 1 then 2 with the symlinks following; a reader holding `live/…/fullchain.pem` open across a renewal never sees a truncated file; the renewal conf parses under the legacy's own key/value rules and stops at `[[webroot_map]]`.
- [x] 1.4 `internal/acme/install.go` links the site paths the vhosts already render — `<docroot>/ssl/<domain>-le.{key,crt,bundle}` → `live/<lineage>/{privkey,fullchain,chain}.pem` — replacing an existing symlink but **never** a regular file, which is what an acme.sh install has there.
- [x] 1.5 `RenewDue` walks `renewal/*.conf` and re-issues inside the 30-day window.
- [x] 1.6 The 30-day / domain-set precondition of D7 under a per-lineage lock. Tests: fresh cert skips, added alias re-issues.
- [x] 1.7 The backoff ledger of D8, per lineage: 15 minutes doubling to a 24h cap, cleared on success. Tests: refused locally, doubles, caps, clears.

## 2. http-01

- [x] 2.1 `webrootSolver` writes `/.well-known/acme-challenge/<token>` under `/usr/local/ispconfig/interface/acme`. No listener. Tests against temp webroot.
- [x] 2.2 Both vhost templates already route the challenge path (golden tests unchanged).

## 3. Integration with the two web plugins

- [x] 3.1 `internal/nginx/le.go`: issuance delegates to `internal/acme` (no acme.sh/certbot shell-out).
- [x] 3.2 `internal/apache2/acme.go`: issue on demand before the vhost renders, then `apache2ctl graceful`. Closes the gap the proposal opened with — an Apache node can obtain its first certificate.
- [x] 3.3 **No vhost or template change** (D5): both keep rendering `<docroot>/ssl/<domain>-le.*`, which task 1.4 links into `/etc/letsencrypt/live/`. A diff in the golden vhost tests would mean an adopted install gets its vhosts rewritten on upgrade, which is the outcome this design exists to avoid.
- [x] 3.4 `internal/web/le_renew.go`: native `RenewDue` branch; the daily job reloads nginx or Apache.

## 4. Config, install, docs

- [x] 4.1 No external ACME client install step — native is the only path.
- [x] 4.2 `acmeStep` and its `--acme` / `--acme-client` answers deleted (D11): the native client makes the external one dead weight, so nothing on the box is installed for ACME any more.
- [ ] 4.3 `docs/acme.md`: the approach vs the legacy shell-out, http-01 vs dns-01, how to run against staging, where the datastore and account key live.
- [x] 4.4 No new API endpoints in this slice, so no swagger change (AGENTS.md rule).

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
