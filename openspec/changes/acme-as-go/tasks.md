## 1. `internal/acme` — the client

- [ ] 1.1 `Client` over `golang.org/x/crypto/acme` (no new dependency): directory discovery, account registration, order → authorize → finalize. The production directory URL appears in exactly one place.
- [ ] 1.2 Account storage at `/var/lib/go-ispconfig/acme/account.{key,json}`, created 0600 on first use and reused after, registration serialised under a file lock (D6). Tests: a second `New` reuses the key; two concurrent `New` calls register once.
- [ ] 1.3 `Issue(ctx, domains) (*Result, error)` writing the acme.sh-shaped directory of D5 temp-then-rename, key 0600.
- [ ] 1.4 `Renew` and `Revoke` on the same client.
- [ ] 1.5 The 30-day / domain-set precondition of D7 under a per-domain lock, with a test for each half: fresh cert skips, added alias re-issues.
- [ ] 1.6 The backoff ledger of D8: a failed issuance is recorded per domain and the next attempt refused locally until it expires. Test the exponential and the cap.

## 2. http-01

- [ ] 2.1 `Solver` interface plus the webroot implementation — `Present` writes `acme.HTTP01ChallengePath` with `HTTP01ChallengeResponse` as the body, `CleanUp` removes it. No listener bound. Test against a temp webroot.
- [ ] 2.2 Confirm both vhost templates already route `/.well-known/acme-challenge` to that webroot, and add the assertion to the golden tests if not.

## 3. Integration with the two web plugins

- [ ] 3.1 `internal/nginx/le.go`: issuance delegates to `internal/acme`; the shell-out path stays behind `[server] acme_client` (D10) so an upgraded host does not change behaviour mid-flight.
- [ ] 3.2 `internal/apache2/acme.go`: issue on demand, then `apache2ctl graceful`. This closes the proposal's gap 1 — an Apache node can obtain its first certificate.
- [ ] 3.3 Point `internal/apache2/ssl.go` and the nginx vhost templates at the D5 datastore, leaving the `~/.acme.sh` and `/etc/letsencrypt/live` paths readable (D10).
- [ ] 3.4 `internal/web/le_renew.go`: native renewal branch when `acme_client=native`; the existing acme.sh/certbot branches untouched.

## 4. Config, install, docs

- [ ] 4.1 `[server] acme_client`, CA directory URL, contact e-mail and key type — rendered on the Server Config Web tab, defaults matching the legacy (http-01, Let's Encrypt production, RSA-2048). Form/getconf staleness test follows automatically.
- [ ] 4.2 Installer: `python3-certbot-nginx` / `python3-certbot-apache` as the optional external path, now genuinely optional because the native path is the default.
- [ ] 4.3 `docs/acme.md`: the approach vs the legacy shell-out, http-01 vs dns-01 and when each applies, how to run against staging, where the datastore and account key live, how to pin the external client.
- [ ] 4.4 Swagger for any new endpoint (AGENTS.md rule).

## 5. dns-01 (second wave — ships after 1–4)

- [ ] 5.1 dns-01 solver over `dns_rr`: insert the `_acme-challenge` TXT, journal it, poll until the authoritative nameserver answers, delete on cleanup.
- [ ] 5.2 Refuse a zone with no `dns_soa` row here, with an error naming it — the D4 failure mode must be a message, not a timeout.
- [ ] 5.3 Wildcard issuance, which is the capability dns-01 unlocks and the legacy has never had.

## 6. Validation

- [ ] 6.1 Unit tests against `tester.MockACMEServer().BuildHTTPS(t)` — lego ships
  its own stub directory (`platform/tester/api.go`: `GET /dir` + `HEAD /nonce`
  over `httptest.NewTLSServer`, extended per test with `.Route(...)`), which is
  how lego tests its own http-01 solver in `challenge/http01/http_challenge_test.go`.
  No network, no pebble, no CA. Covers solvers, storage, locking, backoff and
  the no-production-URL assertion of D9.
- [ ] 6.2 Staging integration test behind `GOISP_ACME_STAGING=1`, **skipped by
  default**. It needs a publicly reachable host — see 6.3.
- [ ] 6.3 The lab cannot validate issuance at all. `192.168.56.0/24` is NAT'd,
  and staging is not a workaround: `acme-staging-v02` validates http-01 by
  connecting *inbound* to port 80 of the name being certified, exactly like
  production. What the lab does prove, and what 6.3 is: the challenge file
  lands under the webroot with the right path and body, and both vhosts serve
  `/.well-known/acme-challenge/<token>` over plain HTTP — asserted with `curl`
  against a hand-placed file, no CA involved. Real issuance is a deploy-time
  check on a public VPS, not a lab task.
- [ ] 6.4 Cross-review with qwen3.8-max before the PR.
