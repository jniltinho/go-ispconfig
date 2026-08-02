# Mail module

Port of the ISPConfig3 mail stack: Postfix + Dovecot + Rspamd driven
from the database through `sys_datalog`. REST API under `/api/mail/...`,
panel UI under the **Email** top-nav module, daemon module + four
plugins on servers with `mail_server = 1`.

Scope is Rspamd only (amavis/SpamAssassin are superseded) and Dovecot
for IMAP/POP3/LMTP. Out of scope (proposal non-goals): getmail,
Mailman mailing lists, Courier/maildrop, mail backups, mailuser
self-service login.

## Getconf keys ([mail])

Typed in `internal/getconf` (`MailConfig`), defaults in
`DefaultMailConfig` (Debian/Ubuntu; the fresh-install seed writes the
same section):

| Key | Default | Used for |
|-----|---------|----------|
| `homedir_path` | `/var/vmail` | maildir root; every delete/purge guard is relative to it |
| `maildir_path` | `/var/vmail/[domain]/[localpart]` | derived `mail_user.maildir` |
| `maildir_format` | `maildir` | `maildir` (Dovecot layout) or `mdbox` (doveadm) |
| `pop3_imap_daemon` | `dovecot` | Dovecot `Maildir/` subdir + SQL-authoritative quota |
| `content_filter` | `rspamd` | gates the DKIM + rspamd plugins |
| `dkim_path` | `/var/lib/rspamd/dkim` | on-disk DKIM key files |
| `dkim_strength` | `2048` | generated RSA key size |
| `mailuser_name`/`group`/`uid`/`gid` | `vmail`/`vmail`/`5000`/`5000` | maildir ownership |
| `mailbox_virtual_uidgid_maps` | `n` | map uid to the matching `web_domain.system_user` when web+mail share the server |
| `mailbox_soft_delete` | `0` | `y` or a day count enables soft delete + purge retention |
| `rspamd_redis_servers`/`_passwd`/`_bayes_*` | `127.0.0.1` | server-level rspamd snippets |
| `sendmail_path` | `/usr/sbin/sendmail` | welcome-mail transport |

## SQL maps vs daemon responsibility

Postfix and Dovecot read the live database through the stock
`mysql-virtual_*.cf` maps (domains, mailboxes, forwardings, transports),
deployed once by the installer. This module does **not** rewrite those
`.cf` files. The daemon owns filesystem state and service reloads:

- **mailPlugin** — maildir lifecycle (create/repair/rename/delete),
  quota on non-Dovecot, domain-tree delete, transport → delayed
  `postfix reload`, optional welcome mail.
- **maildeliverPlugin** — Dovecot sieve scripts from the mailbox
  autoresponder/junk/custom-filter/cc fields.
- **dkimPlugin** — DKIM key files + Rspamd signing maps.
- **rspamdPlugin** — per-identity settings, white/blacklist maps,
  server-level snippets.

The mail module announces `mail_domain_*`, `mail_user_*`,
`mail_forwarding_*`, `mail_transport_*`, `mail_access_*`,
`spamfilter_users_*`, `spamfilter_wblist_*` (plus `server`/`server_ip`
so the rspamd plugin can refresh server config). `spamfilter_policy`,
`mail_get`, `mail_content_filter` and `mail_mailinglist` are not hooked.

## Maildir layout

For `maildir_format=maildir` + Dovecot: `<maildir>/Maildir/{cur,new,tmp}`
plus `.Sent`/`.Drafts`/`.Trash`/`.Junk` (each a maildir), a
`subscriptions` file, all `0700` owned by the resolved uid/gid; the
domain base dir is `0770 mailuser_name:group`. An existing path that is
not a valid maildir is quarantined to
`homedir_path/corrupted/<mailuser_id>` before a clean one is built.
`mdbox` uses `doveadm mailbox create/subscribe`. Format never changes on
update (the old value always wins).

## DKIM and the DNSPublisher

Keys are generated with pure `crypto/rsa` (PKCS#1 private, PKIX public
PEM) at `dkim_strength`, validated on supply, stored in
`mail_domain.dkim_private`/`dkim_public`. The **API never writes
`dns_rr` directly** — DKIM TXT records go through the `DNSPublisher`
interface (`internal/api`), which finds the covering active `dns_soa`
(walking parent labels), upserts the record with SOA-derived ownership,
bumps the serial once via `NextSerial` and journals everything, all on
the mail-domain save's own transaction. When no managed zone matches,
the save still succeeds and the response carries `dns_published=false`
plus the quoted `suggested_record` for manual publication. Disable,
selector change, rename and delete withdraw the old TXT. The daemon
`dkimPlugin` writes `<dkim_path>/<domain>.private`/`.public` (`0640`,
rspamd-owned) and the `dkim_domains.map`/`dkim_selectors.map` lines,
then reloads rspamd. `dkim_private` is redacted from every API response
(the datalog row still carries it so the daemon can write the key file).

## Rspamd files

Under `/etc/rspamd` (no-op when absent):

- `local.d/users/<idn-encoded, @→_>.conf` — per-identity settings from
  the linked `spamfilter_policy` (tag/kill/greylist levels, spam/virus
  lover). Domain identities fan out to child mailboxes/forwardings that
  lack their own `spamfilter_users` row.
- `local.d/users/spamfilter_wblist_<id>.conf` /
  `global_wblist_<access_id>.conf` — white/blacklist maps with priority
  offset +40 (spamfilter) / +30 (global); client access rows carry
  ip/hostname.
- `local.d/{dkim_signing,options.inc,redis.conf,classifier-bayes}.conf`
  — server-level snippets from getconf + server IPs; password-bearing
  files are `chgrp _rspamd`/`chmod 640`.

Rendered sieve and rspamd wblist output are covered by golden files
produced from ISPConfig's own `tpl` engine
(`internal/mail/golden/generate*.php`, run under `php:8.2-cli`).

## Limits

`limit_maildomain`, `limit_mailbox`, `limit_mailalias`,
`limit_mailaliasdomain`, `limit_mailforward` and `limit_mailcatchall`
are enforced through the client limit hook (`-1` unlimited, `0`
disabled, `>0` veto at count) — a create over quota returns 403 with
`error.limit_*`. Admin bypasses.

## Cascade delete and soft delete

`mail_user`/`mail_domain` deletes journal the row; the daemon removes
(or, when `mailbox_soft_delete` is on, renames to
`<path>-deleted-<YmdHis>`) the maildir/domain tree under the
homedir/length/metacharacter guards. The daily `mail_soft_delete_purge`
job removes soft-deleted trees older than the retention window.

## Migration / self-healing notes

- Ships as code only — no schema change. Existing ISPConfig3 `mail_*` /
  `spamfilter_*` rows work unchanged; `$1$`/`$5$`/`$6$` mail password
  hashes keep verifying (passwords are stored CRYPTMAIL).
- After cutover the first datalog event (or a resync touch) recreates
  maildirs, sieve, DKIM keys and rspamd configs from the DB; the SQL
  maps already point at the database.
- `content_filter` must be `rspamd`; the amavis code path is not ported.
- Enable/disable per node with `server.mail_server` and
  `[daemon] disable_mail_module` in `config.toml`.

## Validated against the lab

The module was compared against a live legacy ISPConfig 3.2 mail stack
(lab VMs, Postfix 3.8.6 / Dovecot 2.3.21 / Rspamd, nginx and apache2
variants) using the standing fixture dataset:

- **Sieve**: the Go render of `user1` (plain, alias-expanded vacation
  identity list) and `user2` (autoresponder with date window and empty
  subject) is **byte-identical** to the `.ispconfig-before.sieve` /
  `.ispconfig.sieve` files PHP wrote on both VMs, for the exact
  `mail_user` rows from the legacy database.
- **Maildir layout**: `<maildir>/Maildir/{cur,new,tmp}` with
  `.Sent/.Drafts/.Trash/.Junk` and a `subscriptions` file listing
  `Sent, Drafts, Trash, Junk` in that order, plus the vestigial
  `sieve/` directory — matches this port's provisioning exactly.
- **Rspamd server snippets**: the lab's `dkim_signing.conf` and
  `options.inc` (empty `local_addrs` loop — the lab has no `server_ip`
  rows) match the embedded template outputs; `dkim_domains.map` /
  `dkim_selectors.map` are empty on the lab (no DKIM fixture domain)
  and absent-until-needed here.
- No `local.d/users/` directory exists on the lab (no
  `spamfilter_users` fixtures) — consistent with the no-policy no-file
  behavior of this port.

No divergences were found beyond the deliberate ones listed below.

## Deliberate divergences from PHP

- DKIM TXT is withdrawn on disable/rename/delete (PHP left orphans);
  suggested TXT is quoted so it parses in Bind; TTL fixed at 3600.
- `mail_access` client IP/hostname entries render a conf (PHP's
  `!from && !rcpt` branch deleted it — a PHP bug).
- rspamd redis password reads the real `rspamd_redis_passwd` key (PHP
  read `*_password` and always got empty).
- DKIM key files are `0640` rspamd-owned (design D6 tightens PHP).
- Welcome mail is best-effort via `sendmail`; sent only on the master
  server (the daemon only runs there).
