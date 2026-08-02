# Tasks: add-mail-module

## 1. Models and getconf

- [x] 1.1 Add GORM models for `mail_domain`, `mail_user`, `mail_forwarding`, `mail_transport`, `mail_access` with explicit `gorm:"column:..."` tags matching `internal/database/ispconfig3.sql` (incl. sys_* riud fields); unit-test round-trip against MariaDB. Commit.
- [x] 1.2 Add GORM models for `spamfilter_policy`, `spamfilter_users`, `spamfilter_wblist` with explicit column tags; unit-test round-trip. Commit.
- [x] 1.3 Add typed `MailConfig` getconf section (`homedir_path`, `mailuser_uid/gid/name/group`, `mailbox_virtual_uidgid_maps`, `mailbox_soft_delete`, `pop3_imap_daemon`, `dkim_path`, `dkim_strength`, `content_filter`, `relayhost*`, `sendmail_path`) with Debian/Ubuntu defaults and parse tests. Commit.

## 2. Mail module (daemon events + services)

- [x] 2.1 Implement `internal/modules/mail` Module: announce in-scope events, register table hooks for `mail_domain`, `mail_user`, `mail_forwarding`, `mail_transport`, `mail_access`, `spamfilter_users`, `spamfilter_wblist`, map datalog `i/u/d` → events; gate on `server.mail_server=1` + config.toml; unit tests with fake registries. Commit.
- [x] 2.2 Register services `postfix`, `dovecot`, `rspamd` (reload/restart, delayed dedup); do not register amavis; test unit resolution and restart-wins-reload. Commit.

## 3. mail plugin — maildir lifecycle

- [x] 3.1 Implement uid/gid resolution (`mailbox_virtual_uidgid_maps` → web_domain system_user, else mailuser_uid/gid) and no-datalog DB write-back; unit tests. Commit.
- [x] 3.2 Implement maildir create path: domain base dir 0770, Dovecot maildir layout + standard folders (or mdbox via doveadm), chown; safety helpers; tests with fake runner + temp dirs. Commit.
- [x] 3.3 Implement user_update: refuse maildir_format change, move on path rename, quota apply/unlimited, corrupted-maildir quarantine under `homedir_path/corrupted/<id>`; tests. Commit.
- [x] 3.4 Implement user_delete and domain_delete with path guards and soft-delete rename (`-deleted-<YmdHis>`); refuse unsafe paths; tests. Commit.
- [x] 3.5 Implement transport_* → delayed postfix reload; optional welcome-mail send gated by global config + mirror_server_id==0; tests. Commit.

## 4. maildeliver plugin — sieve

- [x] 4.1 Embed `sieve_filter.master`; implement pure sieve render vector (autoresponder, move_junk, custom_mailfilter, cc/forward_in_lda, alias address collection from `mail_forwarding`); unit tests. Commit.
- [x] 4.2 Produce golden files from PHP maildeliver/tpl for fixtures; byte-identical Go golden tests. Commit.
- [x] 4.3 Implement write/sievec/chown of `.ispconfig-before.sieve` / `.ispconfig.sieve` (+ `.svbin`) and delete cleanup; skip when maildeliver-relevant fields unchanged; tests with stubbed sievec. Commit.

## 5. DKIM plugin + DNSPublisher

- [x] 5.1 Define `DNSPublisher` interface (UpsertTXT/DeleteTXT) with no-op implementation; DNS-module adapter that upserts `dns_rr` TXT, deletes prior DKIM records, bumps SOA serial via existing NextSerial + datalog through DNS repositories; unit tests. Commit.
- [x] 5.2 Implement API-side DKIM key generate/derive/validate (strength from getconf, selector rules); store on `mail_domain`; never log private keys. Commit.
- [x] 5.3 Implement dkimPlugin filesystem: ensure dkim_path, write/remove `.private`/`.public`, maintain Rspamd `dkim_domains.map` / `dkim_selectors.map`, delayed rspamd reload; domain rename/disable/delete transitions; tests. Commit.
- [x] 5.4 Wire domain save to DNSPublisher when active+dkim; return suggested TXT + `dns_published` flag when no zone; tests for publish/unpublish/no-zone. Commit.

## 6. rspamd plugin

- [x] 6.1 Implement user_settings_update for spamfilter_users / mail_user / mail_forwarding (IDN filenames, domain child fan-out, policy rspamd_* fields, greylist); delete path; no-op without `/etc/rspamd`; tests. Commit.
- [x] 6.2 Embed `rspamd_wblist.inc.conf.master`; implement spamfilter_wblist + mail_access map rendering (priority offsets 40/30, global vs per-user, ip/hostname for client type); golden tests; delayed rspamd reload. Commit.
- [x] 6.3 Implement server/server_ip-driven server-level Rspamd snippet regeneration from embedded masters used by PHP; tests with temp config dir. Commit.

## 7. REST API

- [x] 7.1 Domain entity + routes `/api/mail/domains` (CRUD, get-by-domain, set-status, generate-dkim) with tform validators, riud, datalog, Decorate pending/error; swaggo; handler tests. Commit.
- [x] 7.2 Mailbox entity + routes `/api/mail/mailboxes` (CRUD, list-by-client): domain-exists check, CRYPTMAIL password hashing, password omitted on list; swaggo; tests incl. 403 cross-client. Commit.
- [x] 7.3 Forwarding routes for aliases, forwards, catchalls, alias-domains (type forced server-side); transport routes with unique (server_id, domain); swaggo; tests. Commit.
- [x] 7.4 Access + spamfilter policy/users/wblist routes; swaggo; tests. Commit.
- [x] 7.5 Regenerate swagger (`make swagger` / `swag init`), verify Swagger UI lists all mail endpoints, CI staleness check green. Commit.

## 8. Panel UI (Vue)

- [x] 8.1 Mail module navigation in `modules.ts` + router + en.json keys; domain list (DataTable) and domain form with DKIM generate/suggested TXT. Commit.
- [x] 8.2 Mailbox list + tabbed form (Mailbox / Autoresponder / Filters); password field create/update UX. Commit.
- [x] 8.3 Alias, forward, catchall, alias-domain lists/forms and transport list/form. Commit.
- [x] 8.4 Spamfilter policy/users/wblist screens and mail access list/form. Commit.
- [ ] 8.5 agent-browser E2E: domain+DKIM, mailbox, alias, transport, spamfilter policy+whitelist; screenshots to docs/prints. Commit.

## 9. Integration, soft-delete job, docs

- [ ] 9.1 End-to-end integration test against MariaDB: API domain+mailbox+alias create → datalog → daemon run → maildir + sieve + (optional) DKIM files exist; transport update queues postfix reload. Commit.
- [ ] 9.2 Optional daily scheduler job `mail_soft_delete_purge` removing soft-deleted trees older than `mailbox_soft_delete` days under `homedir_path` with the same path guards; tests. Commit.
- [ ] 9.3 Module docs in `docs/mail-module.md`: getconf keys, SQL map responsibility, maildir layout, DKIM+DNSPublisher, Rspamd files, migration/self-healing notes, non-goals. Commit.
