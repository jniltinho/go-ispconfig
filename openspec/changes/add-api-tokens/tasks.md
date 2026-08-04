## 1. Credential core (`internal/apitoken`)

- [ ] 1.1 Add `internal/apitoken/token.go`: mint (`goisp_<id>_<secret>`, 32 random bytes base64url), parse a presented token into id + secret, SHA-256 digest, constant-time compare. Unit tests for round trip, malformed input and the identical-rejection of unknown id vs wrong secret.
- [ ] 1.2 Add `internal/apitoken/meta.go`: the `remote_functions` metadata codec (`scopes=…;expires=…;last_used=…`) with the tolerant bare-CSV fallback (design D2). Table-driven tests incl. a legacy PHP-written value and an unparseable value.
- [ ] 1.3 Add `internal/apitoken/store.go`: load a token by id with its owner `sys_user`, verify enabled/expiry/IP allow-list, and the rate-limited `last_used` write (at most one per minute per token).
- [ ] 1.4 Add the IP allow-list matcher (IPs + CIDRs) resolving the caller address through the existing trusted-proxy chain; tests incl. a forwarded address behind a trusted proxy.

## 2. Scopes (`internal/apitoken/scope.go`)

- [ ] 2.1 Implement the scope grammar `<resource>:<action>` with `write` implying `read` and `*` wildcards; validation that rejects unknown resources/actions and empty scope lists.
- [ ] 2.2 Declare the resource of each route group at registration (`registerEntities`, `registerSystemRoutes`, `registerMonitorRoutes`, `registerServerConfigRoutes`, `registerFail2banRoutes`, `registerMetaRoutes`) and derive the action from the HTTP method.
- [ ] 2.3 Add the route-coverage test: enumerate the built Echo router and fail when any route resolves to no scope.
- [ ] 2.4 Add the out-of-scope error key and wire it into `ErrorHandler` so it is distinguishable from unauthenticated and permission-denied.

## 3. Authentication middleware

- [ ] 3.1 Extend `auth.Middleware` to resolve a bearer value as session id, then as API token; on token success build the same `SessionData` from the owning `sys_user` (id, typ, groups, default group, modules) so every downstream check is unchanged.
- [ ] 3.2 Refuse a token whose owner is inactive; refuse every token when the `remote_api_allowed` policy is not `yes`.
- [ ] 3.3 Mark the resolved credential type on the request context and exempt token/JWT requests from the CSRF requirement, keying off the resolved type and not off a caller-supplied header (design D6). Tests for the cookie-plus-Authorization case.
- [ ] 3.4 Enforce the token scope check for token-authenticated requests only; session requests skip it entirely.
- [ ] 3.5 Add the per-IP failed-token throttle with a 429 response, isolated from the login path.

## 4. Token CRUD API (`internal/api/tokens.go`)

- [ ] 4.1 Add the admin-only token entity over `remote_user` (label, owner, scopes, IP allow-list, expiry, enabled), gated by `admin_allow_remote_users`, with `Decorate` stripping `remote_password`.
- [ ] 4.2 Create returns the plaintext exactly once; validate scopes and owner; store the digest. Integration test asserting the secret never appears in list/get.
- [ ] 4.3 Revoke / re-enable / delete, each taking effect on the next request. Integration test that a revoked token is refused immediately.
- [ ] 4.4 Swaggo annotations for every endpoint, plus the `BearerAuth` description covering both credential forms and the per-endpoint scope; `make swagger` and commit the regenerated docs.

## 5. JWT exchange

- [ ] 5.1 Add `[auth] jwt_secret` / `[auth] jwt_ttl` to the config struct and `config.toml.example`; generate the secret in `go-ispconfig install` with mode 0600.
- [ ] 5.2 Implement `POST /api/tokens/exchange`: HS256, claims `sub`/`tid`/`scope`/`jti`/`exp`, TTL clamped to one hour and to the issuing token's own expiry; record the `jti` in `remote_session`.
- [ ] 5.3 Verify JWTs in the middleware without a database read; tests for tampered claims, expired token, and scope enforcement identical to the parent token.
- [ ] 5.4 Refuse exchange when authenticated by a session, when the token is revoked/expired, or when `jwt_secret` is absent (actionable error, token auth unaffected).
- [ ] 5.5 Extend the daemon's scheduled session sweep to prune expired `remote_session` rows.

## 6. CLI

- [ ] 6.1 Add `cmd/token.go` with `create`, `list` and `revoke` subcommands reading `config.toml`; `create` prints the plaintext once, `list` never prints secrets.
- [ ] 6.2 Cobra tests for flag validation and the refusal to create a token with no scopes.

## 7. Panel UI (System → Remote Users)

- [ ] 7.1 Add the Vue list view (label, owner, scopes, IPs, expiry, last used, enabled) with revoke/re-enable/delete row actions; `never used` rendered explicitly.
- [ ] 7.2 Add the create form with scope selection (human descriptions per scope), optional IP allow-list and expiry.
- [ ] 7.3 Add the one-time secret panel with a copy affordance and the "will not be shown again" warning, plus the JWT expiry-bound note next to the exchange documentation.
- [ ] 7.4 Wire the route + sidebar entry (`sidebar.system.remote_users`) admin-only, and add the i18n keys.
- [ ] 7.5 Vitest coverage for the one-time display (secret gone after navigation) and for the required-scope validation.

## 8. Documentation and validation

- [ ] 8.1 Write `docs/api-tokens.md`: minting a token, scopes table, curl examples against real endpoints, JWT exchange, revocation, and the security posture (digest storage, one-time display, IP allow-list, expiry).
- [ ] 8.2 Update `docs/README.md` and `docs/ROADMAP.md`; update the System-module parity table in `docs/server-config-module.md` now that Remote Users exists.
- [ ] 8.3 Add `e2e/panel-tokens.sh` (agent-browser): create a token in the UI, use it with curl against `/api/sites/web-domains`, revoke it, confirm the next call is 401.
- [ ] 8.4 Validate on the lab VM: mint via CLI and via UI, exercise scope denial, IP allow-list denial, expiry, JWT exchange and revocation; capture screenshots into `docs/prints/`.
