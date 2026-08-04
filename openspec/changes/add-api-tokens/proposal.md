# Proposal: add-api-tokens

## Why

The whole go-ispconfig backend already **is** a REST API — every panel screen is a client of `/api` — but the only way to authenticate against it is to log in as a human: `POST /api/login` with a `sys_user` password, then carry the returned session id (bearer) or the `goisp_session` cookie plus `X-CSRF-Token`. That session is idle-expiring (1 hour, or 30 days for "stay logged in"), it is scoped to a full panel user, and it cannot be revoked without touching `sys_session` by hand.

For automation — a provisioning script, a CI job, Terraform, a billing system creating clients, a backup runner listing sites — this means storing a panel admin's password in a config file and re-logging in whenever the session expires. That is the credential the panel is protected with, granted in full, to every script that needs to read one list.

ISPConfig3 solved this with a second, parallel front door: `interface/web/remote/json.php` plus the `remote_user` / `remote_session` tables, a separate account type with a per-function allow-list and an IP allow-list, managed under System → Remote Users (`interface/web/admin/remote_user_list.php`, `remote_user_edit.php`, `form/remote_user.tform.php`). go-ispconfig already carries **both tables in the schema and both GORM models** (`internal/model/remote.go`) — they are migrated on every install and never used. The Server Config parity sweep listed Remote Users as the largest remaining System gap.

Rather than reproducing `json.php`'s RPC envelope (one POST with a `{function, params}` body, a login call to get a session id, a logout call to drop it), this change gives the existing resource-oriented REST API a machine credential: a **long-lived API token** an admin creates in the panel, presented as `Authorization: Bearer <token>` on the very same endpoints the SPA uses, and optionally exchanged for a short-lived **JWT** for callers that want a stateless, self-verifying credential. The function-level grant list of `remote_user.remote_functions` becomes a scope list over the API's own resources, so the legacy grant model survives the port with the same shape and the same table.

## What Changes

- **API tokens as a first-class credential**: a token is created by an admin, shown **once** at creation, stored only as a SHA-256 digest, and presented as `Authorization: Bearer goisp_<id>_<secret>`. It carries an owner (`sys_user`), a scope list, an optional IP allow-list, an optional expiry, and a `last_used_at` stamp. Revocation is a single row update — no password change, no session hunting.
- **Scopes instead of all-or-nothing**: a token grants a subset of the API surface, expressed as `<resource>:<action>` (`sites:read`, `mail:write`, `dns:*`, `*:read`). Scopes intersect with the owner's existing riud permissions and `sys_user.typ` — a token can never widen what its owner may do, only narrow it.
- **Optional JWT exchange**: `POST /api/tokens/exchange` trades a token for a short-lived (default 15 min) HS256 JWT carrying the same subject and scopes, for callers that prefer a stateless credential or need to hand a bounded-lifetime credential to a sub-process. The signing key is generated at install time and stored in `config.toml`; JWTs are verified without a database round trip and cannot outlive their parent token's revocation window (see design).
- **System → Remote Users** (the legacy menu entry, now backed by tokens): admin-only list and form to create, scope, IP-restrict, expire and revoke tokens, plus a `last used` column so a stale credential is visible. Port of `remote_user_list.php` / `remote_user_edit.php` with the token model replacing the password one.
- **`remote_user` / `remote_session` are reused, not duplicated**: `remote_user` stores the token owner, its digest, scopes (in `remote_functions`), IP allow-list (`remote_ips`) and enabled flag (`remote_access`); `remote_session` stores issued JWT ids for revocation. **No schema change** — the columns already exist and already match. See design D1.
- **CLI**: `go-ispconfig token create|list|revoke` so an unattended install can mint the first automation credential without a browser, and so an operator locked out of the panel can revoke one.
- **`security` policy flags**: token management is gated by the existing `admin_allow_remote_users` flag (superadmin-only by default) and the existing `remote_api_allowed` flag turns the whole token front door off.
- **Swagger**: the existing `BearerAuth` security definition is documented as accepting either a session id or an API token, and the scope of each endpoint is annotated so the generated docs state what a token needs.

## Capabilities

### New Capabilities

- `api-tokens`: the credential itself — creation, one-time display, digest storage, expiry, IP allow-list, revocation, `last_used_at`, and the CLI that manages tokens without a browser.
- `api-token-scopes`: the authorization model — scope grammar, the resource/action map over the existing API surface, intersection with the owner's riud permissions and `typ`, and the 403 behaviour when a scope is missing.
- `api-token-jwt`: the optional stateless credential — exchange endpoint, claim set, signing key lifecycle, expiry bounds, and revocation semantics against `remote_session`.
- `remote-users-ui`: System → Remote Users — the admin list and form, one-time secret display, revoke action, and the security-policy gating.

### Modified Capabilities

- `auth-permissions`: the authentication middleware gains a second credential type on the same `Authorization: Bearer` header (API token / JWT alongside the session id), and mutating requests authenticated by token are exempt from the CSRF header requirement (there is no browser and no ambient cookie to forge).
- `rest-api-core`: every endpoint gains a declared scope, and the API returns a distinct error key when a request is authenticated but out of scope, so a caller can tell "wrong credential" from "insufficient grant".

## Impact

- **Depends on** `port-ispconfig3-to-go` (auth middleware, `sys_session` store, entity framework, security policies) and on the just-landed `cp-users` change (the owner of a token is a `sys_user` row managed there).
- New Go package `internal/apitoken` (mint, hash, verify, scope match), `internal/api/tokens.go` (CRUD + exchange), `cmd/token.go`, Vue `System → Remote Users` view.
- **DB**: none. `remote_user` and `remote_session` already exist in `internal/database/ispconfig3.sql` and in `internal/model/remote.go`; the column meanings are documented in design D1.
- **Config**: new `[auth] jwt_secret` (generated at install, `0600`) and `[auth] jwt_ttl`. An install that never mints a token never needs either.
- Security-sensitive by construction: a long-lived credential that bypasses the interactive login. Mitigations — digest-only storage, one-time display, mandatory scopes, optional IP allow-list and expiry, revocation, `last_used_at`, and rate-limited failed-token attempts — are specified in `api-tokens` and design D5/D6.

## Non-goals

- **Porting `remote/json.php`'s RPC envelope.** No `{function, params}` POST endpoint, no `login`/`logout` RPC pair, no per-function names like `sites_web_domain_get`. Automation talks to the same resource-oriented REST API the panel uses. A compatibility shim for existing ISPConfig remote-API clients is a separate change if it is ever wanted.
- OAuth2, OIDC, or any third-party identity provider; refresh tokens; token exchange between tokens.
- Per-token rate limiting or quota accounting beyond the failed-attempt throttle.
- Asymmetric JWT signing (RS256/ES256) and key rotation tooling — HS256 with a single install-scoped secret, rotatable by editing `config.toml` and restarting, which invalidates outstanding JWTs by design.
- mTLS or client-certificate authentication.
- Tokens owned by a client (non-admin `sys_user`) creating tokens for themselves — v1 is admin-minted only; the owner may be any `sys_user`, but only an admin may mint.
- Webhooks or any outbound callback surface.
