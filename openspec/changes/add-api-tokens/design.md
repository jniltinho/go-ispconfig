# Design: add-api-tokens

## Context

`internal/auth` authenticates a request one of two ways today, both resolving to the *same* thing — a `sys_session` row:

- **Browser**: the `goisp_session` HTTP-only cookie plus `X-CSRF-Token` on every mutating request.
- **Non-browser**: `Authorization: Bearer <session_id>`, where the session id is exactly what `POST /api/login` returned.

`auth.Middleware` looks the id up in `sys_session`, decodes `SessionData` (user id, typ, groups, default group, modules, CSRF token) and attaches it to the request context. Everything downstream — `repository.WithPerm`, `requireAdmin`, entity hooks, security policies — reads that context. A session idles out after 1 hour (30 days when `permanent`).

The schema already carries the ISPConfig3 remote-API tables, migrated on every install and never read:

| Column | ISPConfig3 meaning | Reused here as |
|---|---|---|
| `remote_user.remote_userid` | PK | token id (the public half of the credential) |
| `remote_user.sys_userid` / `sys_groupid` | owner | owner `sys_user` whose permissions the token inherits |
| `remote_user.remote_username` | login name | human label of the token ("terraform-prod") |
| `remote_user.remote_password` | md5/crypt of the API password | **SHA-256 digest of the token secret** |
| `remote_user.remote_access` | `y`/`n` | enabled / revoked |
| `remote_user.remote_ips` | IP allow-list (CSV) | unchanged |
| `remote_user.remote_functions` | allowed function names (CSV) | **scope list** (CSV) |
| `remote_session.remote_session` | RPC session id | issued JWT id (`jti`) |
| `remote_session.remote_userid` | owner | issuing token id |
| `remote_session.tstamp` | expiry | JWT expiry |
| `remote_session.remote_ip` | caller IP | caller IP at exchange |

Two columns have no natural home: expiry and `last_used_at`. See D2.

Constraint that shapes everything below: **a token must never be able to do more than its owner.** The panel's authorization model is riud permission bits per row plus `sys_user.typ`; a token is an attenuation of an existing identity, never a new one.

## Goals / Non-Goals

**Goals:**

- One credential type that works on every existing `/api` endpoint with no endpoint-specific code.
- Attenuation: scopes can only narrow the owner's rights.
- Revocation that takes effect immediately for tokens, and within a bounded window for JWTs.
- No schema change — the tables are already there.
- The failure modes an operator cares about are visible in the panel: last used, expired, revoked, IP-rejected.

**Non-Goals:**

- Replacing session auth for the SPA.
- Any RPC-shaped compatibility surface for existing ISPConfig remote-API clients.
- Multi-tenant token issuance (clients minting their own tokens).

## Decisions

### D1 — Reuse `remote_user`/`remote_session` instead of a new table

**Decision**: store tokens in `remote_user`, JWT ids in `remote_session`, with the column mapping above.

**Why**: the tables exist in `internal/database/ispconfig3.sql`, are modeled in `internal/model/remote.go`, and are created by `go-ispconfig migrate` on every install today. Their semantics are a near-exact match — an owner, a secret, an enable flag, an IP allow-list and a grant list. Adding `api_token`/`api_token_jti` tables would mean a schema divergence from ISPConfig3, which the project's core constraint (identical schema, design D9) forbids for no benefit.

**Alternative considered**: new tables `api_token` + `api_token_jti`. Rejected — schema parity is a hard project constraint, and a migration that adds tables makes an adopted ISPConfig database no longer round-trippable back to the PHP panel.

**Consequence**: two attributes have nowhere to live in the legacy columns — expiry and `last_used_at`. See D2.

### D2 — Expiry and `last_used_at` ride in `remote_functions` as a leading metadata field

**Decision**: `remote_functions` stores a small, ordered, `;`-separated document rather than a bare CSV:

```
scopes=sites:read,mail:*;expires=2027-01-01T00:00:00Z;last_used=2026-08-04T09:41:12Z
```

The parser tolerates a bare CSV (`sites:read,mail:*`) and reads it as scopes with no expiry — so a `remote_user` row written by the PHP panel still parses, and a row written here degrades to "a list of function names" if the PHP panel ever reads it.

**Why**: the alternative is `ALTER TABLE`, which D1 rules out. `remote_functions` is `text` and, in the PHP panel, an opaque CSV the interface writes and reads as a whole — nothing joins on it or indexes it.

**Trade-off, stated plainly**: this is a structured value inside a text column. It is not queryable — "list tokens expiring this week" means reading every row. With a realistic token count (tens, not millions) that is fine; if it ever is not, the fix is a real table and a schema divergence decision, not a smarter encoding.

`last_used` is written **at most once per minute per token** (compare-and-skip on the parsed value) so a busy automation client does not turn every request into a write.

### D3 — Token format: `goisp_<id>_<secret>`, digest-only storage

**Decision**: the credential is `goisp_` + the decimal `remote_userid` + `_` + 32 bytes of `crypto/rand` in base64url (no padding). Only `sha256(secret)` is stored, hex, in `remote_password`.

**Why the id is in the token**: verification is a single primary-key lookup plus one constant-time digest compare. Without it, every request would scan every token row and compare digests — O(n) crypto per request, and a timing surface.

**Why SHA-256 and not bcrypt**: the secret is 256 bits of uniform randomness, not a human password. There is no dictionary to attack, so a slow KDF buys nothing and would add ~50 ms of CPU to *every* API call. `sys_user` passwords stay bcrypt (`internal/auth/password.go`) — different threat model, different function.

**Why the `goisp_` prefix**: it makes a leaked token greppable in logs and scannable by secret-detection tooling.

The plaintext is returned **once**, in the create response. It is never stored, never logged, and never returned by list or get — `Decorate` strips `remote_password` the same way `redactCPUserSecrets` strips hashes.

### D4 — Scope grammar `<resource>:<action>`, intersected with the owner

**Decision**: a scope is `resource:action` where action is `read` | `write` | `*` and resource is one of the API's top-level resource groups (`sites`, `mail`, `dns`, `clients`, `system`, `monitor`, `server`) or `*`. `read` covers GET; `write` covers POST/PUT/DELETE and implies `read`.

Authorization is a **two-stage AND**:

1. the token's scope list must cover (resource, action) of the route;
2. the owner's own identity must permit the operation — the request runs with the owner's `SessionData`, so `repository.WithPerm`, `requireAdmin`, `AdminOnly` entities and every security policy apply unchanged.

**Why not per-function grants like ISPConfig3**: `remote_functions` in the PHP panel lists ~200 function names (`sites_web_domain_get`, `mail_domain_add`, …) that an admin ticks one by one. Our API is resource-oriented; the same expressiveness comes from ~16 scope strings, and a new endpoint inherits the right scope from its route group instead of needing a new grant name that every existing token silently lacks.

**Why intersection and not replacement**: a token that could name `system:*` while owned by a client would be a privilege-escalation primitive. Making the owner's permissions the ceiling means the worst a compromised token can do is exactly what a compromised password for that user could do — minus whatever the scopes exclude.

Route → scope is declared **once per route group** at registration (`registerEntities`, `registerSystemRoutes`, …), not per handler, so an endpoint cannot be added without a scope by forgetting an annotation: the group carries it.

### D5 — JWT is an optional, short-lived projection of a token

**Decision**: `POST /api/tokens/exchange` (authenticated by a token) returns an HS256 JWT with `sub` = owner `sys_user` id, `tid` = token id, `scope` = the token's scopes, `jti` = random id, `exp` = now + `auth.jwt_ttl` (default 15 min, hard cap 1 h). The `jti` is inserted into `remote_session`.

Verification does **not** hit the database in the common path: signature + `exp` + `tid` presence is enough, because the TTL bounds the damage. A revoked token's outstanding JWTs remain valid for at most `jwt_ttl`. Operators who need instant revocation get it by revoking the token and setting `auth.jwt_ttl` low, or by using the token directly (which *is* checked against the row on every request).

**Why offer JWT at all**, when the token already works everywhere: callers that hand a credential to a short-lived sub-process (a CI step, a rendered job) want something that expires on its own, and callers behind a gateway that verifies JWTs want to avoid a panel round trip. Both are real automation shapes; neither is served by a long-lived opaque token.

**Why HS256 and a single install-scoped secret**: one panel, one issuer, one verifier. RS256 buys third-party verification we have no consumer for, and key rotation tooling we would then have to build and test. Rotation today is: edit `config.toml`, restart — every outstanding JWT becomes invalid, which is the correct emergency behaviour.

`remote_session` rows are pruned by the daemon's existing scheduler alongside the `sys_session` sweep.

### D6 — Tokens skip CSRF, and only tokens do

**Decision**: a request authenticated by token or JWT is exempt from the `X-CSRF-Token` requirement; a request authenticated by cookie is not, unchanged.

**Why this is safe**: CSRF exists because a browser attaches cookies to cross-site requests automatically. An `Authorization` header is never ambient — an attacker's page cannot make the victim's browser send it. Requiring a CSRF token from a credential that cannot be CSRF'd would only force every automation client to make a pointless extra call.

**Why it is not a hole**: the exemption keys off *how the request authenticated*, not off a header the caller controls. A request carrying a session cookie is a cookie request even if it also carries an `Authorization` header; the middleware resolves the cookie path first and keeps the CSRF requirement.

### D7 — IP allow-list is evaluated against the same trusted-proxy chain as the request logger

**Decision**: `remote_ips` accepts CSV of IPs and CIDRs; the caller IP is resolved by the existing `Deps.trustedProxies` logic (`internal/api/api.go`) rather than raw `RemoteAddr`, so a panel behind nginx sees the real client.

An empty list means "any IP" — the field is an optional hardening, not a mandatory one, matching ISPConfig3.

## Risks / Trade-offs

- **A long-lived credential that bypasses interactive login** → digest-only storage, one-time display, mandatory non-empty scope list, optional expiry and IP allow-list, immediate revocation, `last_used_at` surfaced in the UI, and the whole surface killable via the existing `remote_api_allowed` policy flag.
- **Structured data in a text column (D2)** → tolerant parser that accepts the legacy bare-CSV form; documented as a deliberate trade against schema divergence. If token counts ever make it painful, the migration path is a real table plus an explicit divergence decision.
- **Revoked JWTs stay valid until `exp`** → bounded by a default 15-minute TTL and a 1-hour hard cap; documented in the UI next to the exchange setting. Operators needing instant revocation use the token directly.
- **Scope drift as the API grows** → scopes are declared per route *group*, and a test asserts every registered route resolves to a scope, so a new endpoint cannot ship unscoped.
- **Token in a URL** → never accepted as a query parameter, only as the `Authorization` header, so it cannot land in an access log or a `Referer`.
- **Brute force against `remote_userid`** → the id is public by design; the secret is 256 bits. Failed verifications are counted per source IP and throttled, and a failure never distinguishes "no such token" from "wrong secret".

## Migration Plan

1. Ship the credential and the middleware behind the existing `remote_api_allowed` policy (default `yes`, but no token exists on a fresh install, so the surface is inert until an admin mints one).
2. `go-ispconfig install` generates `[auth] jwt_secret` at `0600` alongside the DB credentials; an upgrade without one refuses only the `exchange` endpoint, with an actionable error, and leaves tokens working.
3. No data migration: `remote_user` is empty on every existing install (nothing writes it today).
4. **Rollback**: revoking every token (`UPDATE remote_user SET remote_access='n'`) disables the surface without a downgrade; removing the middleware restores exactly today's behaviour, because session auth is untouched.

## Open Questions

- Should a non-admin `sys_user` eventually be allowed to mint tokens for itself (scoped to its own permissions)? The proposal says admin-only for v1; the data model already supports it, so this is a policy decision, not a design one.
- Should `last_used_at` record the source IP of the last use as well? Useful for spotting a leaked token, but it is one more field in the D2 blob.
- Is a per-token rate limit worth building before there is evidence of need, or does the existing failed-attempt throttle plus scopes cover the realistic abuse cases?
