# API tokens

The whole go-ispconfig backend is a REST API — every panel screen is a client
of `/api`. An **API token** lets a script be a client too, without storing a
panel admin's password and without re-logging in when a session idles out.

A token is:

- **long-lived** — it expires only if you give it an expiry;
- **scoped** — it grants a subset of the API, and can never exceed what its
  owning user may do;
- **revocable** — one click, effective on the next request;
- **stored as a digest** — the secret is shown once at creation and never
  again, by anyone, anywhere.

Everything below was exercised against a real panel; the outputs are real
(with the secrets shortened).

## Minting a token

### From the panel

**System → Remote Users → Add new token.** Give it a label, pick the scopes,
optionally restrict it to a set of IPs and give it an expiry. The token is
displayed **once** — copy it then, because there is no screen anywhere that
can show it again.

### From the API

```bash
PANEL=https://panel.example.com:8080

# 1. Log in as an admin to get a session id.
SESSION=$(curl -sk -X POST $PANEL/api/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"…"}' | jq -r .session_id)

# 2. Mint the token.
curl -sk -X POST $PANEL/api/tokens \
  -H "Authorization: Bearer $SESSION" \
  -H 'Content-Type: application/json' \
  -d '{
        "label": "automation-demo",
        "scopes": ["sites:read", "dns:read"]
      }' | jq
```

```json
{
  "token": "goisp_1_iz7hpud80Yz1VrZ9gVuWsdl7QY-FLh7…",
  "id": 1,
  "label": "automation-demo",
  "owner": "admin",
  "owner_id": 1,
  "scopes": ["sites:read", "dns:read"],
  "allowed_ips": "",
  "enabled": true
}
```

The `token` field is the credential. **It appears in this response and nowhere
else, ever.**

Optional fields: `owner` (a `sys_user` login; defaults to the caller),
`allowed_ips` (CSV of IPs/CIDRs), `expires_at` (RFC3339).

### From the CLI

Useful for an unattended install, or when nobody can reach the panel:

```bash
go-ispconfig token create automation-demo \
  --owner admin \
  --scope sites:read --scope dns:read \
  --ips 10.0.0.0/8 \
  --expires 2027-01-01T00:00:00Z
```

```
Token 1 (automation-demo) created for admin
Scopes: sites:read, dns:read

goisp_1_iz7hpud80Yz1VrZ9gVuWsdl7QY-FLh7…

This is the only time the token is shown. Store it now.
```

`go-ispconfig token list` shows every token (never a secret) and
`go-ispconfig token revoke <id>` disables one.

## Using a token

Send it on the same header the panel uses. No CSRF token, no login call, no
session to keep alive:

```bash
TOKEN=goisp_1_iz7hpud80Yz1VrZ9gVuWsdl7QY-FLh7…

curl -sk $PANEL/api/sites/web-domains \
  -H "Authorization: Bearer $TOKEN" | jq '.items[] | {domain, active}'
```

```json
{"domain": "alpha1.goisp.test", "active": "y"}
{"domain": "beta2.goisp.test",  "active": "y"}
```

Anything the API can do, a suitably scoped token can do:

```bash
# Create a DNS zone
curl -sk -X POST $PANEL/api/dns/zones \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"origin":"example.com.","ns":"ns1.example.com.","mbox":"hostmaster.example.com.","serial":"2026080401"}'

# List mailboxes
curl -sk "$PANEL/api/mail/mailboxes?limit=50" -H "Authorization: Bearer $TOKEN"

# Delete a website
curl -sk -X DELETE $PANEL/api/sites/web-domains/7 -H "Authorization: Bearer $TOKEN"
```

The full endpoint list is in the Swagger UI at `/swagger/`.

## Scopes

A scope is `<resource>:<action>`.

| Resource | Covers |
|---|---|
| `sites` | websites, folders, cron jobs, FTP and shell users, databases |
| `mail` | mail domains, mailboxes, aliases, forwards, spamfilter, fetchmail |
| `dns` | zones, records, slave zones, templates |
| `clients` | clients, resellers, limit and message templates |
| `monitor` | monitor state, data, logs, job queue |
| `server` | server rows, server config, server IPs, firewall, fail2ban |
| `system` | CP users, token management |
| `*` | every resource |

| Action | Covers |
|---|---|
| `read` | `GET` |
| `write` | `POST`, `PUT`, `PATCH`, `DELETE` — and implies `read` |
| `*` | both |

So `sites:read` reads websites, `mail:write` fully manages mail, and `*:read`
is a read-only credential for the whole panel.

**Scopes only ever narrow.** A request authenticated by a token runs as the
token's owner, with that user's row permissions, admin/client level and every
security policy applied unchanged. A token owned by a client cannot reach
admin endpoints no matter what scopes it carries; a token owned by an admin
reaches only what its scopes allow.

Out-of-scope requests are distinguishable from every other failure:

```bash
curl -sk $PANEL/api/mail/domains -H "Authorization: Bearer $TOKEN"
```

```json
{"error":{"key":"error.missing_scope","fields":{"scope":["mail:read"]}}}
```

`403` with `error.missing_scope` means *the credential is fine, the grant is
not*. `401` means the credential itself was rejected — unknown, wrong,
revoked, expired, or used from a disallowed IP; the API deliberately never
says which.

## Short-lived JWTs

For a CI step or a sub-process that should hold a credential which expires on
its own, exchange the token for a JWT:

```bash
curl -sk -X POST $PANEL/api/tokens/exchange -H "Authorization: Bearer $TOKEN" | jq
```

```json
{
  "access_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOjEsInRpZCI6MSwic2NvcGUiOlsic2l0ZXM6cmVhZCIsImRuczpyZWFkIl0…",
  "token_type": "Bearer",
  "expires_in": 899,
  "scopes": ["sites:read", "dns:read"]
}
```

Use it exactly like the token:

```bash
JWT=$(curl -sk -X POST $PANEL/api/tokens/exchange -H "Authorization: Bearer $TOKEN" | jq -r .access_token)
curl -sk $PANEL/api/sites/web-domains -H "Authorization: Bearer $JWT"
```

The JWT carries the issuing token's owner and scopes and nothing else, so it
can never do more than the token did.

**One caveat worth understanding:** a JWT is verified by signature and expiry
alone, without a database read — that is what makes it stateless. So
**revoking a token does not immediately kill JWTs it already issued**: they
stop working when they expire, at most `[auth] jwt_ttl` later (default 15
minutes, hard-capped at 1 hour, and never beyond the token's own expiry). If
you need revocation to be instant, use the token directly — it is checked
against its row on every single request.

Configuration:

```toml
[auth]
# Generated by `go-ispconfig install`. Replacing it invalidates every
# outstanding JWT immediately — that is the emergency lever.
jwt_secret = "…"
jwt_ttl = "15m"
```

Both `go-ispconfig install` and `go-ispconfig init` generate a random key when
they write the file, and `install --update` reuses the one already there — so
an upgrade never invalidates outstanding JWTs.

An install created **before** this feature has no `jwt_secret`. Tokens keep
working; only `/api/tokens/exchange` returns `503` naming the missing setting.
Add the two lines above with a key of your own and restart
`go-ispconfig-serve`:

```bash
printf '\n[auth]\njwt_secret = "%s"\njwt_ttl = "15m"\n' "$(openssl rand -hex 32)" \
  | sudo tee -a /etc/go-ispconfig/config.toml
sudo systemctl restart go-ispconfig-serve
```

## Managing tokens

```bash
# List (never returns secrets)
curl -sk $PANEL/api/tokens -H "Authorization: Bearer $SESSION" | jq

# Revoke — effective on the very next request
curl -sk -X PUT $PANEL/api/tokens/1 \
  -H "Authorization: Bearer $SESSION" -H 'Content-Type: application/json' \
  -d '{"enabled": false}'

# Re-enable
curl -sk -X PUT $PANEL/api/tokens/1 \
  -H "Authorization: Bearer $SESSION" -H 'Content-Type: application/json' \
  -d '{"enabled": true}'

# Change scopes, IP allow-list or expiry (the secret can never be changed —
# mint a new token instead)
curl -sk -X PUT $PANEL/api/tokens/1 \
  -H "Authorization: Bearer $SESSION" -H 'Content-Type: application/json' \
  -d '{"scopes": ["sites:*", "dns:read"], "allowed_ips": "10.0.0.0/8"}'

# Delete irreversibly
curl -sk -X DELETE $PANEL/api/tokens/1 -H "Authorization: Bearer $SESSION"
```

The list shows `last_used_at`, which is how you find the credential nobody
remembers issuing:

```json
[
  {
    "id": 1,
    "label": "automation-demo",
    "owner": "admin",
    "scopes": ["sites:read", "dns:read"],
    "allowed_ips": "",
    "enabled": true,
    "last_used_at": "2026-08-04T10:32:28Z"
  }
]
```

A token that has never authenticated has no `last_used_at` at all.

## Security posture

| Control | Behaviour |
|---|---|
| Storage | only `sha256(secret)` is kept; the plaintext exists once, in the create response |
| Transport | `Authorization` header only — never accepted as a query parameter, so it cannot land in an access log or a `Referer` |
| Scope | mandatory and non-empty; intersected with the owner's own permissions |
| IP allow-list | optional CSV of IPs/CIDRs, matched against the real client address behind `[server] trusted_proxies` |
| Expiry | optional, enforced on every request |
| Revocation | immediate for tokens; bounded by `jwt_ttl` for already-issued JWTs |
| Owner | a disabled `sys_user` disables all of its tokens |
| Brute force | failed attempts are throttled per source IP (`429`), and a failure never reveals whether the token id exists |
| Kill switch | the `remote_api_allowed` security policy disables every token and JWT at once, leaving panel logins untouched |
| Who may mint | admins only, further gated by the `admin_allow_remote_users` policy (superadmin-only by default) |

Two practical rules:

1. **Give a token the narrowest scope that works.** A deploy script that only
   creates websites wants `sites:write`, not `*:*`.
2. **Prefer revoking over deleting** while a credential may still be in use —
   a revoked token gives the caller a clean `401` and leaves an audit trail;
   a deleted one is indistinguishable from a typo.

## Storage details

Tokens live in the ISPConfig3 `remote_user` table, which has shipped in the
schema since the port began and had no reader until now — so there is **no
schema change and no migration**. `remote_password` holds the digest,
`remote_access` the enabled flag, `remote_ips` the allow-list, and
`remote_functions` the scopes plus expiry and last-used. Issued JWT ids are
recorded in `remote_session` and swept when they expire.

See `openspec/changes/add-api-tokens/design.md` for why each of those choices
was made, including the trade-off in reusing `remote_functions`.
