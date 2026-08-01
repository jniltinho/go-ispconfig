# Proposal: add-mail-module

> Roadmap phase 2 — proposal only. Design/specs/tasks will be written when this module is scheduled.

## Why

The foundation (`port-ispconfig3-to-go`) ships the datalog engine, `.master` renderer, riud permissions, REST API core and panel skeleton, but no mail functionality. This change ports the ISPConfig3 mail stack so go-ispconfig can manage a Postfix + Dovecot + Rspamd mail server end to end: mail domains, mailboxes, aliases/forwardings, transports, spam filtering and DKIM, plus the panel UI and API that drive them.

Reference PHP sources being ported (read-only under `base/ispconfig3_install/`):
- `server/mods-available/mail_module.inc.php` — table hooks for `mail_domain`, `mail_user`, `mail_forwarding`, `mail_transport`, `mail_access`, `mail_get`, `mail_content_filter`, `mail_mailinglist`, `spamfilter_users`, `spamfilter_wblist` → named events
- `server/plugins-available/mail_plugin.inc.php` — maildir creation/removal, mailbox quota, mail_domain/mail_transport handling
- `server/plugins-available/mail_plugin_dkim.inc.php` — DKIM key generation and publication per mail domain
- `server/plugins-available/rspamd_plugin.inc.php` — Rspamd user settings, wblist/mail_access maps, server-level config
- `server/plugins-available/maildeliver_plugin.inc.php` — Dovecot sieve delivery rules per mailbox
- `interface/web/mail/` — forms/lists for the Mail panel module
- `interface/lib/classes/remote.d/mail.inc.php` — remote API surface (`mail_domain_*`, `mail_user_*`, `mail_alias_*`, `mail_forward_*`, `mail_catchall_*`, `mail_transport_*`, `mail_spamfilter_*`, policy/wblist functions)

## What Changes

- **mail module (daemon side)**: Go `Module` registering the table hooks above and raising the corresponding named events (`mail_domain_*`, `mail_user_*`, `mail_forwarding_*`, `mail_transport_*`, `spamfilter_users_*`, `spamfilter_wblist_*`, `mail_access_*`, …); registers the `postfix`, `dovecot` and `rspamd` services with delayed restart/reload.
- **mail plugin**: maildir lifecycle (create/rename/delete), mailbox quota enforcement, domain delete cascade, transport map maintenance — port of `mail_plugin.inc.php`.
- **maildeliver plugin**: Dovecot sieve filter generation from `mail_user` autoresponder/move-to-folder settings — port of `maildeliver_plugin.inc.php`.
- **DKIM**: per-domain key generation, DNS TXT record publication and Rspamd/signing config — port of `mail_plugin_dkim.inc.php`. TXT publication goes through a defined `DNSPublisher` interface exposed by the DNS module/foundation — the mail plugin never writes `dns_rr` rows via direct SQL.
- **Rspamd integration**: spamfilter policies and per-user settings files, white/blacklist maps from `spamfilter_wblist` and `mail_access`, server-level Rspamd config — port of `rspamd_plugin.inc.php` (Rspamd only; amavis is superseded).
- **Postfix/Dovecot config**: virtual maps (domains, mailboxes, aliases, transports) driven from the DB via the mechanisms the plugins above maintain, using the existing `.master` templates where applicable.
- **REST API**: port of `remote.d/mail.inc.php` — mail domain, mailbox, alias/forward/catchall, transport, spamfilter policy/wblist CRUD — with swaggo annotations, riud scopes and datalog writes.
- **UI (Vue 3)**: Mail panel module — domain list/form, mailbox list/form (quota, autoresponder, filters), aliases/forwardings, transports, spamfilter policies and white/blacklists.
- **Testing**: golden-file tests for generated maps/configs, integration tests for the datalog→event→file pipeline.

## Capabilities

### New Capabilities

- `mail-module-events`: daemon mail module — table hooks for the MAIL table group, named event dispatch, postfix/dovecot/rspamd service registration.
- `mail-mailbox-management`: maildir lifecycle, quota, aliases/forwardings, transports and sieve delivery rules (mail_plugin + maildeliver_plugin ports).
- `mail-dkim`: DKIM key generation, signing config and DNS record publication per mail domain.
- `mail-rspamd-spamfilter`: spamfilter policies, per-user settings and white/blacklists rendered to Rspamd configuration.
- `mail-rest-api`: REST endpoints porting `remote.d/mail.inc.php` with swagger docs.
- `mail-panel-ui`: Vue Mail module — domains, mailboxes, aliases, transports, spamfilter.

### Modified Capabilities

(none — foundation capabilities are consumed, not changed)

## Impact

- **Depends on `port-ispconfig3-to-go`** (datalog registries, `.master` renderer, rest-api-core, auth-permissions, panel-skeleton). No dependency on the web/DNS modules, though DKIM DNS records integrate with `add-dns-bind-module` when present — via the `DNSPublisher` interface, never by writing `dns_rr` directly.
- New Go packages: `internal/modules/mail` (module + plugins), REST handlers, Vue `mail` module.
- DB: no schema changes — uses existing MAIL group tables (`mail_domain`, `mail_user`, `mail_forwarding`, `mail_transport`, `mail_access`, `spamfilter_policy`, `spamfilter_users`, `spamfilter_wblist`, …).
- External services on the mail server: Postfix, Dovecot, Rspamd (systemd units), plus `openssl`/`rspamadm` for DKIM keys.

## Non-goals

- getmail / `mail_get` fetching (`getmail_plugin`), Mailman mailing lists (`mailman_plugin`), Courier and maildrop (`maildrop_plugin`) — legacy.
- amavis/SpamAssassin — superseded by Rspamd.
- Advanced backup-MX / relay-domain scenarios beyond basic `mail_transport` support.
- Mail backups (`mail_backup`) and mailuser self-service login — later changes.
- Translations beyond English.
