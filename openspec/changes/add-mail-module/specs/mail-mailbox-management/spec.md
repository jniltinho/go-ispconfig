# mail-mailbox-management

## ADDED Requirements

### Requirement: Mailbox create provisions maildir on disk
On `mail_user_insert` the mail plugin SHALL resolve `uid`/`gid` (from `web_domain.system_user` when `mailbox_virtual_uidgid_maps=y` and same `server_id`, else `mailuser_uid`/`mailuser_gid`), persist them onto `mail_user` without a new datalog row when they changed from `-1`, ensure the domain base directory under `homedir_path` exists (0770, `mailuser_name`:`mailuser_group`), and create the mailbox layout for `maildir_format` (`maildir` with Dovecot `Maildir/` subdirs and Sent/Drafts/Trash/Junk, or `mdbox` via `doveadm`), owned by the resolved user/group (port of `mail_plugin::user_insert`).

#### Scenario: New maildir mailbox is created under homedir_path
- **WHEN** `mail_user_insert` fires for `user@example.com` with `maildir=/var/vmail/example.com/user` and `maildir_format=maildir`
- **THEN** the path and standard subfolders exist, owned by the resolved uid/gid, and the domain base `/var/vmail/example.com` exists 0770 as vmail

#### Scenario: Default uid/gid applied and written back
- **WHEN** insert payload has `uid=-1` and `gid=-1` and virtual maps are off
- **THEN** the plugin sets uid/gid from mail getconf and updates `mail_user` without writing a new `sys_datalog` row for that update

### Requirement: Mailbox update moves path and refreshes quota
On `mail_user_update` the plugin SHALL refuse to change `maildir_format` (keep old value), move the maildir when the path changes (after safety checks), recreate missing structure, and apply quota semantics: for non-Dovecot maildrop set `maildirmake -q <quota>S` when `quota>0` or remove `maildirsize` when unlimited; for Dovecot, SQL quota is authoritative (port of `user_update`).

#### Scenario: Email rename moves maildir
- **WHEN** `mail_user_update` changes `maildir` from `/var/vmail/example.com/old` to `/var/vmail/example.com/new` and the old path exists
- **THEN** the directory is moved to the new path and ownership is corrected

#### Scenario: Maildir format cannot change on update
- **WHEN** an update payload sets `maildir_format` different from the old row
- **THEN** the plugin keeps the old format for filesystem operations

### Requirement: Mailbox and domain delete with path guards
On `mail_user_delete` / `mail_domain_delete` the plugin SHALL delete (or soft-rename when `mailbox_soft_delete` is enabled) only paths that are under `homedir_path`, longer than `homedir_path`, length ≥ 10, and free of `//`, `..`, `*`, `&`. Unsafe paths SHALL be refused and logged as errors without deletion (PHP parity).

#### Scenario: Soft-delete renames mailbox
- **WHEN** `mail_user_delete` fires and `mailbox_soft_delete` is enabled for a valid maildir path
- **THEN** the path is renamed to `<maildir>-deleted-<YmdHis>` and not hard-removed

#### Scenario: Path outside homedir is refused
- **WHEN** `mail_user_delete` fires with `old.maildir=/etc/passwd-looking-path`
- **THEN** no filesystem delete runs and an error is logged

#### Scenario: Domain delete removes domain tree
- **WHEN** `mail_domain_delete` fires for domain `example.com`
- **THEN** `homedir_path/example.com` and `homedir_path/mailfilters/example.com` are removed or soft-renamed under the same guards

### Requirement: Invalid maildir quarantine
When a path exists but is not a valid maildir (missing `new`/`cur` for maildir format), the plugin SHALL move it to `homedir_path/corrupted/<mailuser_id>` before recreating a clean structure (PHP parity).

#### Scenario: Corrupted directory is quarantined
- **WHEN** `mail_user_update` finds `maildir` exists without `new`/`cur`
- **THEN** the tree is moved under `corrupted/<mailuser_id>` and a fresh maildir is created

### Requirement: Transport events reload Postfix
On `mail_transport_insert|update|delete` the mail plugin SHALL request a delayed `postfix` reload so transport map changes are picked up (SQL maps read live; reload refreshes Postfix caches).

#### Scenario: Transport change queues postfix reload
- **WHEN** `mail_transport_update` fires
- **THEN** a delayed `reload` is queued for the `postfix` service

### Requirement: Sieve scripts from mailbox delivery settings
On `mail_user_insert|update` when `custom_mailfilter`, `move_junk`, autoresponder fields, `email`, `cc` or `forward_in_lda` change, the maildeliver plugin SHALL render `sieve_filter.master` for `before` and `after` into `<maildir>/.ispconfig-before.sieve` and `<maildir>/.ispconfig.sieve`, compile with `sievec` to `.svbin`, set mode 0600 and ownership to `mailuser_name`:`mailuser_group`, and include alias addresses from `mail_forwarding` rows of type `alias` and `aliasdomain` (port of `maildeliver_plugin`). On `mail_user_delete` those sieve artifacts SHALL be removed.

#### Scenario: Autoresponder enables sieve vacation
- **WHEN** a mailbox is updated with `autoresponder=y` and non-empty subject/text
- **THEN** both sieve files contain the autoresponder content and compiled `.svbin` files exist

#### Scenario: Alias addresses included in vacation
- **WHEN** sieve is rendered for `user@example.com` and an alias `alias@example.com` → `user@example.com` exists
- **THEN** the sieve `:addresses` list includes both addresses

#### Scenario: Unchanged filter fields skip rewrite
- **WHEN** a mailbox update changes only `quota` and no maildeliver-relevant field
- **THEN** sieve files are not rewritten

### Requirement: Domain and mailbox field validation (API-side)
Domain create/update SHALL enforce `mail_domain.tform.php` rules: `server_id` a mail-capable server (`mail_server=1`); `domain` NOTEMPTY, ISDOMAIN, lowercased, IDN→ASCII, not conflicting with a `mail_transport.domain` on the same server; `dkim`/`active`/`local_delivery` Y/N (`y`/`n`); `dkim_selector` lowercase alnum ≤63 when DKIM enabled; `dkim_private` valid RSA when set. Mailbox create/update SHALL enforce `mail_user.tform.php`: `email` ISEMAIL+UNIQUE, lowercased IDN; domain part must exist as primary `mail_domain` and not only as aliasdomain; `password` strength (CRYPTMAIL on save); `quota` integer ≥0; Y/N flags for access/postfix/disable* / greylisting / autoresponder / forward_in_lda; `move_junk` in `y|a|n`.

#### Scenario: Mailbox on missing domain rejected
- **WHEN** a mailbox is created for `a@no-such-domain.test` with no `mail_domain` row
- **THEN** the API returns a validation error and no datalog row is written

#### Scenario: Duplicate email rejected
- **WHEN** a mailbox is created with an email that already exists
- **THEN** the API returns a uniqueness validation error

### Requirement: Forwarding and transport validation
`mail_forwarding` rows SHALL validate `type` ∈ {`alias`,`aliasdomain`,`forward`,`catchall`}, `source`/`destination` per type (email or `@domain` forms), `active`/`allow_send_as`/`greylisting` Y/N, unique sensible sources per server as in the tform rules. `mail_transport` SHALL require unique `(server_id, domain)`, non-empty `transport`, `sort_order` integer, `active` Y/N.

#### Scenario: Catchall source is domain catchall form
- **WHEN** a catchall is created with source `@example.com` and a valid destination
- **THEN** the row is stored with `type=catchall` and a datalog insert is written

### Requirement: riud permissions and datalog on all mail mutations
All domain/mailbox/forwarding/transport/access/spamfilter mutations SHALL go through the foundation permission scope (`sys_userid`/`sys_groupid`/`sys_perm_*`) and SHALL write `{old,new}` JSON datalog rows targeted at the record's `server_id`.

#### Scenario: Client cannot modify another client's mailbox
- **WHEN** client A updates a `mail_user` owned by client B's group with empty `sys_perm_other`
- **THEN** the API returns 403 and no datalog row is written

### Requirement: Golden-file fidelity for sieve rendering
Sieve output SHALL be covered by golden-file tests whose expected outputs were produced by the original PHP `tpl.inc.php`/`maildeliver_plugin` logic for the same fixtures (autoresponder on/off, move_junk modes, cc+forward_in_lda, alias expansion).

#### Scenario: Golden sieve matches PHP
- **WHEN** the fixture mailbox is rendered by the Go maildeliver plugin
- **THEN** the before/after sieve text is byte-identical to the committed PHP-produced golden files
