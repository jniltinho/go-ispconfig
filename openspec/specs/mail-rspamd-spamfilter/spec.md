# mail-rspamd-spamfilter Specification

## Purpose
TBD - created by archiving change add-mail-module. Update Purpose after archive.
## Requirements

### Requirement: Per-user Rspamd settings files
On `spamfilter_users_*`, `mail_user_*` and `mail_forwarding_*` events, when `/etc/rspamd` exists, the rspamd plugin SHALL write or delete a per-identity settings file under the local users config directory (IDN-encoded filename, `@` replaced by `_`). Domain-level identities (`@example.com` or bare domain) SHALL also re-render child mailboxes and forwardings that lack their own `spamfilter_users` row. Global `@` / `*@` identities SHALL be ignored (PHP parity). Settings SHALL incorporate linked `spamfilter_policy` Rspamd fields (`rspamd_greylisting`, `rspamd_spam_greylisting_level`, `rspamd_spam_tag_level`, `rspamd_spam_tag_method`, `rspamd_spam_kill_level`) and per-row greylisting where applicable (port of `rspamd_plugin::user_settings_update`).

#### Scenario: Spamfilter user insert writes settings conf
- **WHEN** `spamfilter_users_insert` fires for email `user@example.com` with a policy that sets kill level 15
- **THEN** a settings file for that identity exists containing the policy-derived thresholds

#### Scenario: Delete removes settings file
- **WHEN** `spamfilter_users_delete` fires for an identity that had a settings file
- **THEN** the file is removed and a delayed `rspamd` reload is queued

#### Scenario: Rspamd absent is a no-op
- **WHEN** any rspamd-handled event fires and `/etc/rspamd` is not a directory
- **THEN** the plugin returns without error and writes no files

### Requirement: White/blacklist maps from spamfilter_wblist and mail_access
On `spamfilter_wblist_*` and `mail_access_*` events the plugin SHALL render `rspamd_wblist.inc.conf.master` into `spamfilter_wblist_<wblist_id>.conf` or `global_wblist_<access_id>.conf` when the row is active and from/rcpt (and optional ip/hostname for access type `client`) validate; priority SHALL be row priority plus 40 (spamfilter) or 30 (global access). Inactive or invalid rows SHALL remove the file if present. After changes a delayed `rspamd` reload SHALL be requested.

#### Scenario: Whitelist entry rendered
- **WHEN** `spamfilter_wblist_insert` fires with `wb=W`, active, valid from and rid→recipient email
- **THEN** the conf file exists with whitelist action and elevated priority

#### Scenario: Global mail_access sender block
- **WHEN** `mail_access_update` sets type `sender`, access not `OK`, active `y`
- **THEN** `global_wblist_<access_id>.conf` is written with blacklist semantics for the encoded source

#### Scenario: Inactive wblist removes file
- **WHEN** a wblist row is updated to `active=n`
- **THEN** its conf file is deleted if it existed

### Requirement: Spamfilter policy and user management (API-side)
`spamfilter_policy` CRUD SHALL persist all policy columns used by Rspamd (at minimum the `rspamd_*` fields plus identity name) with riud and without requiring a daemon table hook (PHP has none). `spamfilter_users` CRUD SHALL require unique `email`, valid `policy_id` reference when non-zero, `priority`, `server_id`, and write datalog rows so the rspamd plugin regenerates settings. `spamfilter_wblist` CRUD SHALL validate `wb` ∈ {`W`,`B`}, `rid` referencing a spamfilter user, `email`, `priority`, `active`.

#### Scenario: Policy update alone does not write rspamd files until dependents refresh
- **WHEN** only a `spamfilter_policy` row is updated via API
- **THEN** a policy table write succeeds (no mail-module event for that table) and existing settings files remain until a dependent user/mailbox event re-renders them

#### Scenario: Assigning a user to a policy regenerates settings
- **WHEN** a `spamfilter_users` row is updated to a new `policy_id`
- **THEN** a datalog update is written and the subsequent daemon run rewrites that user's Rspamd settings from the new policy

### Requirement: mail_access management
`mail_access` rows SHALL support `type` ∈ {`recipient`,`sender`,`client`}, unique `(server_id, source, type)`, `access` string (e.g. `OK` / `REJECT`), `active`, with riud and datalog so rspamd global wblist maps stay in sync.

#### Scenario: Client IP access list entry
- **WHEN** an access row is created with type `client` and a valid IP source
- **THEN** the row is stored and the daemon writes a global wblist conf using the IP field

### Requirement: Server-level Rspamd config refresh
On server or server_ip events (when emitted), the rspamd plugin SHALL regenerate server-level Rspamd snippets from mail getconf and server IP data using the embedded `rspamd_*.master` templates that the PHP plugin maintains, then request delayed `rspamd` reload.

#### Scenario: Server IP change refreshes rspamd server config
- **WHEN** a `server_ip_update` event is processed on a mail server with Rspamd installed
- **THEN** server-level Rspamd config files are rewritten and reload is queued

### Requirement: Golden-file fidelity for Rspamd snippets
Wblist conf rendering and a representative user-settings conf SHALL be covered by golden-file tests produced from the PHP `rspamd_wblist.inc.conf.master` / plugin logic for the same fixtures.

#### Scenario: Golden wblist conf matches
- **WHEN** the fixture wblist vector is rendered by the Go plugin
- **THEN** the output is byte-identical to the committed PHP-produced golden file
