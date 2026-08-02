# Design: Mail module (Postfix / Dovecot / Rspamd)

## Context

ISPConfig3's mail stack is four pieces glued by `sys_datalog`:

1. `interface/web/mail/` + `remote.d/mail.inc.php` — tform forms and remote API writing `mail_domain`, `mail_user`, `mail_forwarding`, `mail_transport`, `mail_access`, `spamfilter_policy`, `spamfilter_users`, `spamfilter_wblist` (and related tables) with `{old,new}` datalog diffs. Interface never shells out to Postfix/Dovecot; DKIM key generation and DNS TXT publication happen on the interface side when a domain is saved.
2. `server/mods-available/mail_module.inc.php` — registers table hooks for the MAIL group, translates datalog actions into named events, registers `postfix` / `rspamd` / `amavis` services (this port drops amavis).
3. `server/plugins-available/mail_plugin.inc.php` — maildir create/rename/delete, quota, domain-directory cascade, transport-triggered Postfix reload.
4. `mail_plugin_dkim.inc.php` + `rspamd_plugin.inc.php` + `maildeliver_plugin.inc.php` — DKIM key files and Rspamd signing maps; per-user Rspamd settings + wblist maps; Dovecot sieve scripts from mailbox autoresponder / move-junk / custom filter fields.

The foundation change already provides everything this module plugs into: datalog consumer with table-hook/event registries, `.master` renderer, getconf, delayed service restarts, riud GORM scopes, validation engine, REST core, panel skeleton. The DB tables exist (byte-identical ISPConfig3 schema); only GORM models, daemon plugins, REST entities and UI are missing.

Postfix/Dovecot virtual maps (`mysql-virtual_*.cf`) already point at the same tables — map queries are not rewritten by the plugins. What the daemon owns is filesystem state (maildirs, sieve, DKIM keys, Rspamd local configs) and service reloads.

## Goals / Non-Goals

**Goals:**
- Behavior-faithful port of `mail_module` + `mail_plugin` + `maildeliver_plugin` + `mail_plugin_dkim` + `rspamd_plugin` for the in-scope tables, with Rspamd as the only content filter (`content_filter=rspamd`).
- API/UI parity with the ISPConfig Mail module for domains, mailboxes, alias/forward/catchall/aliasdomain, transports, spamfilter policies/users/wblists and mail access lists.
- DKIM: key generation, private/public storage on `mail_domain`, on-disk key files, Rspamd signing maps, and DNS TXT publication via a `DNSPublisher` interface (never direct `dns_rr` SQL).
- Golden-file tests for rendered sieve and Rspamd wblist/user settings; integration tests for the datalog → event → filesystem pipeline.

**Non-Goals:**
- getmail / `mail_get`, Mailman / `mail_mailinglist`, Courier / maildrop, amavis / SpamAssassin (see proposal Non-goals).
- Mail backups (`mail_backup`), mailuser self-service login, advanced backup-MX beyond basic `mail_transport`.
- Schema changes of any kind; translations beyond English.
- Regenerating Postfix/Dovecot main configs on every event — SQL maps already read live from MariaDB; only delayed reload is required when transport maps or services need it.

## Decisions

### D1 — One package, module + four plugins
`internal/modules/mail` contains the `Module` (table hooks → events, port of `mail_module.inc.php`) and four plugins registered explicitly in daemon bootstrap:

| Plugin | PHP source | Responsibility |
|---|---|---|
| `mailPlugin` | `mail_plugin.inc.php` | maildir lifecycle, domain delete cascade, transport → postfix reload |
| `maildeliverPlugin` | `maildeliver_plugin.inc.php` | sieve before/after scripts from `mail_user` fields |
| `dkimPlugin` | `mail_plugin_dkim.inc.php` | DKIM key files + Rspamd domain/selector maps |
| `rspamdPlugin` | `rspamd_plugin.inc.php` | per-user settings, wblist/access maps, server-level Rspamd config |

Keeping the two-level dispatch (hook → named event → plugin) preserves the foundation's registry architecture and matches the nginx/dns modules.
*Alternative*: one mega-plugin — rejected: harder to test and diverges from PHP event subscriptions.

### D2 — Table hooks and events (in-scope subset)
The module announces and raises the same event names as PHP for the tables this change actually manages:

| Table | Events |
|---|---|
| `mail_domain` | `mail_domain_insert/update/delete` |
| `mail_user` | `mail_user_insert/update/delete` |
| `mail_forwarding` | `mail_forwarding_insert/update/delete` |
| `mail_transport` | `mail_transport_insert/update/delete` |
| `mail_access` | `mail_access_insert/update/delete` |
| `spamfilter_users` | `spamfilter_users_insert/update/delete` |
| `spamfilter_wblist` | `spamfilter_wblist_insert/update/delete` |

PHP also hooks `mail_get`, `mail_content_filter`, `mail_mailinglist` — those remain unregistered (non-goals). `spamfilter_policy` has no daemon hook in PHP either; policy edits affect Rspamd only when a dependent `spamfilter_users` / mailbox / forwarding row is re-processed (or via explicit resync). Event names stay identical so future plugins port 1:1.
*Alternative*: register all PHP hooks as no-ops — rejected, dead code.

### D3 — Services: postfix, dovecot, rspamd (no amavis)
The module registers three services with delayed restart/reload:

- `postfix` — unit `postfix` (reload default; restart when needed).
- `dovecot` — unit `dovecot` (reload after sieve/mailbox layout changes that need it). PHP's mail_module does not register dovecot; we add it because maildeliver/sieve and mailbox provisioning are Dovecot-centric on the target distros.
- `rspamd` — unit `rspamd` (reload after wblist/user/DKIM map changes).

Amavis is **not** registered. `content_filter` is fixed to `rspamd` for this port (amavis path in `mail_plugin_dkim` is not ported). Restart wins over reload in the same datalog run (foundation services registry).

### D4 — Maildir lifecycle (mailPlugin)
Port of `user_insert` / `user_update` / `user_delete` / `domain_delete` / `transport_update`:

1. **UID/GID resolution**: if `uid`/`gid` are `-1`, optionally map from matching `web_domain.system_user` when `mailbox_virtual_uidgid_maps=y` and web+mail share `server_id`; else use `mailuser_uid`/`mailuser_gid` from mail getconf. Persist resolved uid/gid back onto `mail_user` **without** a new datalog row (PHP parity: direct UPDATE).
2. **Create**: ensure domain base dir under `homedir_path` (0770, `mailuser_name`:`mailuser_group`); for `maildir_format=maildir` and `pop3_imap_daemon=dovecot` create `maildir` + `Maildir/{new,cur,tmp}` plus `.Sent`/`.Drafts`/`.Trash`/`.Junk` subfolders via `maildirmake` (or pure-Go equivalent with identical layout); for `mdbox` use `doveadm mailbox create/subscribe`. Chown recursive to resolved user/group.
3. **Quota**: for non-dovecot maildrop path set `maildirmake -q <quota>S`; for Dovecot, quota lives in SQL (`mail_user.quota`) — no maildirsize file required. `quota=0` means unlimited.
4. **Rename**: if `maildir` path changes, `mv` old → new (after safety checks); never change `maildir_format` on update (force old value like PHP).
5. **Delete mailbox**: path must be under `homedir_path`, length ≥ 10, no `//`/`..`/`*`. Soft-delete renames to `<path>-deleted-<YmdHis>` when `mailbox_soft_delete` is enabled; else `rm -rf`.
6. **Delete domain**: same guards on `homedir_path/<domain>` and `homedir_path/mailfilters/<domain>`.
7. **Transport events**: request delayed `postfix` reload (PHP runs `postfix reload` immediately; we use delayed reload for batching).
8. **Welcome mail**: optional, gated by global `enable_welcome_mail=y` and `mirror_server_id==0`, templates under embedded conf — port as a best-effort send via local MTA; failure is logged, never rolls back the mailbox.

All destructive paths go through the foundation command runner (logged, faked in tests). Corrupted-maildir quarantine (`homedir_path/corrupted/<mailuser_id>`) is ported.

### D5 — Sieve delivery rules (maildeliverPlugin)
On `mail_user_insert/update`, when any of `custom_mailfilter`, `move_junk`, autoresponder fields, `email`, `cc`, `forward_in_lda` change: render `sieve_filter.master` twice (`before` / `after`) into:

- `<maildir>/.ispconfig-before.sieve` (+ `.svbin` via `sievec`)
- `<maildir>/.ispconfig.sieve` (+ `.svbin`)

Vector includes: CC loop when `forward_in_lda=y`, `move_junk` (`y`/`a`/`n`), autoresponder subject/text/dates (space→`T` for sieve date), alias addresses collected from `mail_forwarding` type `alias` + `aliasdomain`. Ownership 0600 `mailuser_name`:`mailuser_group`. On delete, remove sieve artifacts. Golden files pin rendered sieve against PHP `tpl.inc.php` output for the same fixtures.

### D6 — DKIM (API-side keys + dkimPlugin filesystem)
Split across interface and daemon, matching PHP:

**API / domain save (interface side):**
- Generate RSA private key (`openssl genrsa` or crypto/rsa) at `dkim_strength` bits (default 2048 from mail getconf).
- Derive PEM public key; store both in `mail_domain.dkim_private` / `dkim_public`; selector in `dkim_selector` (default `default`, lowercase alnum, max 63).
- Publish DNS TXT via `DNSPublisher` (D7) when `active=y` and `dkim=y`: name `<selector>._domainkey.<domain>.`, data `v=DKIM1; t=s; p=<base64-without-pem-headers>`.
- On DKIM disable or domain deactivate/delete: unpublish the TXT through the same interface.

**Daemon dkimPlugin (on domain insert/update/delete):**
- Write `<dkim_path>/<domain>.private` and `.public` when active+dkim; remove when disabled/deleted.
- Rspamd path only: maintain `/etc/rspamd/local.d/dkim_domains.map` and `dkim_selectors.map` lines (`domain path`, `domain selector`); delayed `rspamd` reload.
- Amavis `60-dkim` path is **not** ported.

Key material in the DB is the source of truth; files are derived. Invalid private keys are rejected at API validation (`openssl rsa -check` parity).

### D7 — DNSPublisher interface (no direct dns_rr SQL)
PHP's `mail_domain_edit.php` writes `dns_rr` / bumps `dns_soa.serial` with raw SQL. This port defines:

```go
// DNSPublisher publishes or withdraws a TXT (or other) record into a managed zone.
// Implemented by the DNS module when present; a no-op (or err-not-available)
// implementation is used when DNS is disabled.
type DNSPublisher interface {
    UpsertTXT(ctx context.Context, owner Owner, name, data string, ttl uint32) error
    DeleteTXT(ctx context.Context, name, dataPrefix string) error // dataPrefix e.g. "v=DKIM1"
}
```

- Looks up active `dns_soa` by origin matching the mail domain (and parent zones as PHP does for subdomains).
- Upsert deletes prior DKIM TXT for old selector/domain, inserts new `dns_rr` with ownership from the SOA, bumps serial via existing `NextSerial`, writes datalog rows — all through the DNS module's repository, never ad-hoc SQL from mail.
- If no managed zone exists, the domain form still shows the suggested TXT for manual publication (PHP shows `dns_record` string); Upsert is a no-op success with a response flag `dns_published=false`.

Rationale: keeps mail free of Bind knowledge and matches the proposal's explicit integration contract with `add-dns-bind-module`.

### D8 — Rspamd integration (rspamdPlugin)
Port of `rspamd_plugin.inc.php` when `/etc/rspamd` exists:

1. **User settings files** (`user_settings_update` on spamfilter_users / mail_user / mail_forwarding events): one conf file per email or domain under the Rspamd local users dir (IDN-encoded name, `@` → `_`). Domain-level `@example.com` also re-renders child mailboxes/forwardings that lack their own `spamfilter_users` row. Settings vector includes policy thresholds from `spamfilter_policy` (`rspamd_spam_tag_level`, `rspamd_spam_kill_level`, `rspamd_spam_greylisting_level`, `rspamd_spam_tag_method`, greylist flags) plus per-mailbox `greylisting`. Delete removes the file.
2. **Wblist maps** (`spamfilter_wblist_*` and `mail_access_*`): render `rspamd_wblist.inc.conf.master` into `spamfilter_wblist_<id>.conf` or `global_wblist_<access_id>.conf` with priority offset (+40 spamfilter / +30 global). Inactive or invalid from/rcpt pairs remove the file.
3. **Server-level config** (`server_update` / `server_ip_*`): regenerate server Rspamd snippets from mail getconf + server IPs (whitelists, options) using embedded `rspamd_*.master` templates where the PHP plugin does; delayed `rspamd` reload.

Golden files cover wblist and a representative user-settings conf. No amavis/SpamAssassin config is written.

### D9 — Postfix/Dovecot SQL maps, not file maps
ISPConfig installs `mysql-virtual_domains.cf`, `mysql-virtual_mailboxes.cf`, `mysql-virtual_forwardings.cf`, `mysql-virtual_transports.cf`, etc., pointing at:

- `mail_domain` (`domain`, `active`, `server_id`)
- `mail_user` (`email`, `maildir`, `quota`, `access`/`postfix`, disable* flags, …)
- `mail_forwarding` (`source`, `destination`, `type`, `active`, `allow_send_as`)
- `mail_transport` (`domain`, `transport`, `active`)
- `mail_access` for policy/access maps

This module does **not** rewrite those `.cf` files on every event. The installer (or a one-time ensure step) deploys the stock templates with DB credentials. Daemon work is: keep filesystem state consistent + `postfix reload` / `dovecot reload` / `rspamd reload` when needed. Document which tables each map reads so operators understand live-DB semantics.

### D10 — GORM models on existing tables only
Models with explicit `gorm:"column:..."` tags for:

| Model | Table | PK |
|---|---|---|
| `MailDomain` | `mail_domain` | `domain_id` |
| `MailUser` | `mail_user` | `mailuser_id` |
| `MailForwarding` | `mail_forwarding` | `forwarding_id` |
| `MailTransport` | `mail_transport` | `transport_id` |
| `MailAccess` | `mail_access` | `access_id` |
| `SpamfilterPolicy` | `spamfilter_policy` | `id` |
| `SpamfilterUser` | `spamfilter_users` | `id` |
| `SpamfilterWblist` | `spamfilter_wblist` | `wblist_id` |

Optional for sieve alias expansion reads only (no dedicated CRUD in this change beyond what mailbox save needs): `MailUserFilter` → `mail_user_filter`. No migrations, no new columns. Password field on `mail_user` uses CRYPTMAIL hashing at the API boundary (never store plaintext; never return hash on list unless admin policy requires — default: omit/`***` on read like panel passwords).

### D11 — REST API shape
Port of `remote.d/mail.inc.php` onto the foundation entity framework (same pattern as `internal/api/dns.go` / `sites.go`) under `/api/mail/...`:

| Resource | Routes (REST) | Remote API parity |
|---|---|---|
| Domains | `/api/mail/domains` CRUD + set-status + generate-dkim | `mail_domain_*`, `mail_domain_set_status`, `mail_domain_get_by_domain` |
| Mailboxes | `/api/mail/mailboxes` CRUD + list-by-client | `mail_user_*` |
| Aliases | `/api/mail/aliases` (type=`alias`) | `mail_alias_*` |
| Forwards | `/api/mail/forwards` (type=`forward`) | `mail_forward_*` |
| Catchalls | `/api/mail/catchalls` (type=`catchall`) | `mail_catchall_*` |
| Alias domains | `/api/mail/alias-domains` (type=`aliasdomain`) | `mail_aliasdomain_*` |
| Transports | `/api/mail/transports` | `mail_transport_*` |
| Access lists | `/api/mail/access` | `mail_whitelist_*` / `mail_blacklist_*` (type recipient/sender/client) |
| Policies | `/api/mail/spamfilter/policies` | `mail_policy_*` |
| Spamfilter users | `/api/mail/spamfilter/users` | `mail_spamfilter_user_*` |
| Wblists | `/api/mail/spamfilter/wblists` | `mail_spamfilter_whitelist_*` / `_blacklist_*` (`wb` W/B) |

All mutations: riud scope + `{old,new}` datalog on the row's `server_id`. Validators port tform rules (domain ISDOMAIN+UNIQUE per server constraints, email ISEMAIL+UNIQUE, password strength, transport domain UNIQUE per server, etc.). Swaggo on every endpoint. Mailbox create verifies the domain part exists as a primary `mail_domain` and is not an aliasdomain (PHP `mail_user_add` check).

### D12 — UI shape
Vue Mail module (mirrors `interface/web/mail/` lists/forms, foundation DataTable + TabbedForm):

- **Domains** list + form (server, domain, active, local_delivery, relay_* admin fields, DKIM tab: enable, selector, generate key, public key read-only, suggested DNS TXT, auto-publish status).
- **Mailboxes** list + form tabs: Mailbox (email, password, name, quota, cc, sender_cc, greylisting, access/postfix, disable* flags), Autoresponder, Filters (move_junk, custom_mailfilter, purge days), optional Backup fields stored only.
- **Email aliases / Forwards / Catchalls / Alias domains** — four lists or one list filtered by `mail_forwarding.type`.
- **Transports** list/form.
- **Spamfilter**: policies, users (policy assignment), whitelist/blacklist; **Mail access** whitelist/blacklist (global).
- Navigation + en.json keys; agent-browser E2E for the main flows.

### D13 — Mail getconf section
Typed `MailConfig` in `internal/getconf` from `server.ini.master` `[mail]` keys used by the plugins: `homedir_path`, `mailuser_uid/gid/name/group`, `mailbox_virtual_uidgid_maps`, `mailbox_soft_delete`, `pop3_imap_daemon` (expect `dovecot`), `dkim_path`, `dkim_strength`, `content_filter` (expect `rspamd`), `relayhost*`, `sendmail_path`. Defaults for Debian/Ubuntu match the installer seed; module docs list them.

### D14 — Module enablement
Load only when `server.mail_server = 1` and the module is enabled in `config.toml` (same pattern as dns/web). Non-mail servers ignore mail datalog rows for local application (rows may still be written for other servers' `server_id`).

## Risks / Trade-offs

- [Maildir `rm -rf` as root] → same path guards as PHP (under `homedir_path`, min length, no metacharacters); soft-delete option; command runner logs every argv; unit tests with fake runner.
- [DKIM private keys in DB] → ISPConfig already stores them in `mail_domain.dkim_private`; API never logs key material; list endpoints omit private key; filesystem keys mode 0640 owned by rspamd user.
- [DNSPublisher absent when DNS module disabled] → domain save still succeeds; UI shows manual TXT; no error hard-fail unless caller requested `require_dns_publish`.
- [Postfix maps are live SQL — inactive rows must be filtered] → map queries (stock templates) already filter `active='y'`; our job is correct `active` flags and datalog delivery, not map regeneration.
- [Rspamd conf dir layout differs slightly across Debian 11–13] → paths from getconf + existence checks on `/etc/rspamd`; skip cleanly when Rspamd is not installed (PHP checks `is_dir('/etc/rspamd')`).
- [Welcome email duplicates on multi-server] → only send when `mirror_server_id==0` (PHP parity).
- [Password hashing algorithm drift] → use the same CRYPTMAIL scheme the foundation already uses for mail-compatible hashes (`$1$`/`$5$`/`$6$` as ISPConfig); migration keeps existing hashes.

## Migration Plan

- Ships as code only — no schema change. Existing ISPConfig3 `mail_*` / `spamfilter_*` rows work as-is.
- Fresh installs: installer seeds `[mail]` getconf, deploys SQL map `.cf` files, packages Postfix/Dovecot/Rspamd (owned by `add-installer-cli` / distro packages).
- After cutover: first datalog event (or resync touch) recreates maildirs/sieve/DKIM files from DB; SQL maps already point at the DB.
- Rollback: disable mail module in `config.toml`; filesystem state remains as last applied.

## Open Questions

- Should `mail_user_filter` rows get first-class CRUD in this change, or only `custom_mailfilter` free-text on the mailbox (PHP has both)? Leaning: store/API for `mail_user_filter` if time allows in API tasks; UI can start with custom_mailfilter + move_junk only.
- Soft-delete purge cron is a daemon scheduler job in PHP (`500-clean_mailboxes`) — include a minimal `mail_soft_delete_purge` daily job here or defer? Leaning: small job in this module (threshold from `mailbox_soft_delete` days).
- Relay domain / relay recipient tables exist and have remote API methods but are outside the proposal UI list — expose admin-only API later, not in this change.
