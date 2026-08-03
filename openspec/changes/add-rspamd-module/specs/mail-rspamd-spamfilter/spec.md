# mail-rspamd-spamfilter

## MODIFIED Requirements

### Requirement: Server-level Rspamd config refresh
On server or server_ip events, the rspamd plugin SHALL regenerate server-level Rspamd snippets from mail getconf and server IP data using the embedded `rspamd_*.master` templates that the PHP plugin maintains, then request delayed `rspamd` reload. In addition to those templated snippets, the same handler SHALL deploy the baseline Rspamd configuration that ISPConfig deploys from its installer (`install/lib/installer_base.lib.php::configure_rspamd()`) — including the `local.d/users.conf` include that loads the per-identity settings files, the static `local.d` and `override.d` snippets, the `maps.d` whitelist files and the controller worker configuration — as specified by the `rspamd-config` capability. Without that baseline the per-identity settings files and wblist confs this capability writes are loaded by nothing; deploying it is therefore part of a correct server-level refresh, not an optional extra.

#### Scenario: Server IP change refreshes rspamd server config
- **WHEN** a `server_ip_update` event is processed on a mail server with Rspamd installed
- **THEN** server-level Rspamd config files are rewritten and reload is queued

#### Scenario: Refresh also ensures the settings include exists
- **WHEN** a server event is processed on a mail server whose `local.d/users.conf` is missing
- **THEN** it is written from the template so the per-identity settings files under the local users config directory take effect

### Requirement: Spamfilter policy and user management (API-side)
`spamfilter_policy` CRUD SHALL persist all policy columns used by Rspamd (at minimum the `rspamd_*` fields plus identity name) with riud and without requiring a daemon table hook (PHP has none); it SHALL be available to non-administrators subject to the `limit_spamfilter_policy` client and reseller limits rather than being restricted to administrators (parity with `spamfilter_policy_edit.php`). `spamfilter_users` CRUD SHALL require unique `email`, valid `policy_id` reference when non-zero, `priority`, `server_id`, enforce the `limit_spamfilter_user` client and reseller limits on create, and write datalog rows so the rspamd plugin regenerates settings. `spamfilter_wblist` CRUD SHALL validate `wb` ∈ {`W`,`B`}, `rid` referencing a spamfilter user readable by the caller, `email`, `priority`, `active`, enforce the `limit_spamfilter_wblist` client and reseller limits on create, and SHALL derive `server_id` from the referenced `spamfilter_users` row rather than trusting a caller-supplied value (parity with `spamfilter_whitelist_edit.php::onSubmit`).

#### Scenario: Policy update alone does not write rspamd files until dependents refresh
- **WHEN** only a `spamfilter_policy` row is updated via API
- **THEN** a policy table write succeeds (no mail-module event for that table) and existing settings files remain until a dependent user/mailbox event re-renders them or a resync action runs

#### Scenario: Assigning a user to a policy regenerates settings
- **WHEN** a `spamfilter_users` row is updated to a new `policy_id`
- **THEN** a datalog update is written and the subsequent daemon run rewrites that user's Rspamd settings from the new policy

#### Scenario: Over-limit create is refused
- **WHEN** a client whose `limit_spamfilter_wblist` is already reached creates another wblist entry
- **THEN** the request is refused with the limit error and no row or datalog entry is written

#### Scenario: Server is derived from the referenced spamfilter user
- **WHEN** a wblist row is created with a `server_id` different from that of the `spamfilter_users` row named by `rid`
- **THEN** the stored row carries the spamfilter user's `server_id`

### Requirement: mail_access management
`mail_access` rows SHALL support `type` ∈ {`recipient`,`sender`,`client`}, unique `(server_id, source, type)`, `access` string (e.g. `OK` / `REJECT`), `active`, with riud and datalog so rspamd global wblist maps stay in sync. Writes SHALL additionally enforce the authorization the PHP forms enforce (`mail_whitelist_edit.php` / `mail_blacklist_edit.php`): a leading `@` is stripped from `source`; non-administrators may use only `recipient` and `sender` types, and their `source` MUST be a valid email address whose domain resolves to a `mail_domain` readable under their riud scope; creates are subject to the `limit_mail_wblist` client and reseller limits. Administrators keep the unrestricted surface.

#### Scenario: Client IP access list entry
- **WHEN** an administrator creates an access row with type `client` and a valid IP source
- **THEN** the row is stored and the daemon writes a global wblist conf using the IP field

#### Scenario: Non-admin cannot create a client-type entry
- **WHEN** a client submits an access row with type `client`
- **THEN** the request is refused with a validation error and no row is written

#### Scenario: Non-admin cannot list a foreign domain
- **WHEN** a client submits an access row whose `source` belongs to a mail domain they cannot read
- **THEN** the request is refused and no row is written

#### Scenario: Leading at-sign normalised
- **WHEN** an access row is submitted with `source` `@example.com`
- **THEN** the stored source is `example.com`
