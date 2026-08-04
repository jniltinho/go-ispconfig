## 1. Share the row shape

- [ ] 1.1 Move `row` and its `str`/`num` accessors into `internal/web` as an exported `Row`; alias it back in both plugins (`type row = web.Row`) so no call site changes.

## 2. Extract the issuer

- [ ] 2.1 Move `internal/nginx/le.go` into `internal/web/le_issue.go`, replacing the `plugin *Plugin` field with the four values it actually reads: `DB`, `Runner`, `Log`, `Webroot`, plus a `Module` string for the `"nginx: …"` / `"apache2: …"` error prefixes.
- [ ] 2.2 Keep `AcmeWebroot` re-exported from `internal/nginx` — it is referenced by the vhost template and by the installer.
- [ ] 2.3 Move `le_test.go` with it, unchanged in substance: the version gates, the ECDSA/RSA choice, `assembleDomains` (subdomains, aliases, www, dedup, 100 cap), the reachability probe and both clients' argv are the contract of the extraction.
- [ ] 2.4 Reduce `internal/nginx/le.go` to the wrapper: `newLEClient`/`requestCert` construct the shared issuer from the plugin. Assert the argv is byte-for-byte what it was before the move.

## 3. Wire Apache2

- [ ] 3.1 Add `maybeRequestLE` to the Apache plugin's site path, mirroring `internal/nginx/handlers.go:101` — same change detection (`ssl`, `ssl_letsencrypt`, `domain`, `subdomain`), same `webLEConfig` from the server config, same datalog warning on failure.
- [ ] 3.2 Set `sslChanged` so the vhost re-renders against the freshly written `-le.crt` / `-le.key` instead of the previous run's files.
- [ ] 3.3 Confirm the Apache vhost template serves `/.well-known/acme-challenge` from `AcmeWebroot` — the challenge is written there regardless of which client issues, and a missing alias fails validation before certbot is ever reached.
- [ ] 3.4 Test the Apache path end to end with a stub runner: a site with `ssl_letsencrypt = y` must produce an issue call, and one with it off must not.

## 4. Lab

- [ ] 4.1 Create the `apache-test` node (`vagrant up apache-test`) — it is `not created` today, which is why this gap went unnoticed.
- [ ] 4.2 Issue a real certificate for a site on that node with each client (acme.sh and certbot) and confirm the vhost serves it, not the self-signed pair.
- [ ] 4.3 Run the renewal job on the same node and confirm it now has something to renew.

## 5. Documentation

- [ ] 5.1 State in `docs/` that Let's Encrypt is available on both web servers, and that issuance is webroot-based on both — so the certbot web-server plugins are an operator convenience, not a dependency.
