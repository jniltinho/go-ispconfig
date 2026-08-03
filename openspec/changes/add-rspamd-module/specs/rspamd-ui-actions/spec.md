# rspamd-ui-actions

## ADDED Requirements

### Requirement: Separate whitelist and blacklist screens per scope
The panel SHALL provide four spam-list screens mirroring `interface/web/mail/lib/module.conf.php`: spamfilter whitelist and spamfilter blacklist over `spamfilter_wblist` (list filtered on `wb`, form with `wb` fixed to `W` or `B` and not user-editable), and access whitelist and access blacklist over `mail_access` (list and form fixed on the `OK` / `REJECT` access value, matching the PHP form defaults). Each list SHALL support search on the address field and show active status. Navigation entries SHALL be hidden when the caller's matching limit is zero, as `module.conf.php` does.

#### Scenario: Whitelist form cannot create a blacklist row
- **WHEN** a user saves an entry from the spamfilter whitelist form
- **THEN** the stored row has `wb` = `W` regardless of any `wb` value in the submitted body

#### Scenario: Blacklist list shows only blacklist rows
- **WHEN** a user opens the spamfilter blacklist list
- **THEN** only rows with `wb` = `B` readable under the riud scope are shown

#### Scenario: Navigation hidden at zero limit
- **WHEN** a client whose `limit_spamfilter_wblist` is `0` opens the Mail module
- **THEN** the spamfilter whitelist and blacklist entries are not offered

### Requirement: Relationship fields rendered as scoped dropdowns
Forms SHALL render `rid`, `policy_id` and `server_id` as dropdowns populated from the records the caller may read — spamfilter users by email for `rid`, spamfilter policies by name for `policy_id`, mail servers by name for `server_id` — instead of raw numeric inputs (parity with the tform SQL datasources in `form/spamfilter_whitelist.tform.php`, `form/spamfilter_users.tform.php` and `form/mail_blacklist.tform.php`). The spamfilter wblist form SHALL NOT show a server field at all, because the server is derived from the referenced spamfilter user. Priority SHALL be offered as the PHP 1–10 scale.

#### Scenario: Wblist form offers the caller's spamfilter users
- **WHEN** a client opens the spamfilter whitelist form
- **THEN** the recipient dropdown lists only the spamfilter user emails readable under their riud scope, and no server field is shown

#### Scenario: Access form limits the type options for non-admins
- **WHEN** a non-admin opens an access list form
- **THEN** the type dropdown offers only `recipient` and `sender`

### Requirement: Spam score threshold form
The spamfilter policy form SHALL expose the Rspamd thresholds `rspamd_spam_tag_level`, `rspamd_spam_kill_level`, `rspamd_spam_greylisting_level` and the tag method `rspamd_spam_tag_method` (parity with `templates/spamfilter_rspamd_edit.htm` and the `rspamd` tab of `form/spamfilter_policy.tform.php`), validated as numbers within a sane score range and rejecting a tag level above the kill level. Empty values SHALL be allowed and SHALL be shown with the daemon's own fallbacks as placeholders, so the form states what will actually be rendered when a field is left unset. The policy screen SHALL be available to non-admin callers under their policy limit rather than restricted to administrators.

#### Scenario: Tag level above kill level rejected
- **WHEN** a policy is saved with a tag level greater than its kill level
- **THEN** a validation error is returned and the row is not written

#### Scenario: Empty threshold shows the effective default
- **WHEN** a policy has no tag level set
- **THEN** the form shows the daemon fallback as a placeholder and saving leaves the column empty

#### Scenario: Client manages its own policy
- **WHEN** a client with a non-zero policy limit opens the policies screen
- **THEN** the screen is available and lists the policies readable under their riud scope

### Requirement: Greylisting toggles with inheritance stated
The panel SHALL expose greylisting as the daemon resolves it: the policy-level `rspamd_greylisting` flag with its level, and the per-mailbox and per-forwarding `greylisting` flag. The forms SHALL state the resolution rule the plugin implements — an explicitly greylisted mailbox or forwarding wins, otherwise the policy value applies — so the two toggles are not read as contradicting each other.

#### Scenario: Mailbox greylisting overrides an off policy
- **WHEN** a mailbox has greylisting enabled and its effective policy has greylisting disabled
- **THEN** the mailbox form indicates greylisting is in effect for that mailbox

#### Scenario: Policy greylisting inherited
- **WHEN** a mailbox does not have greylisting explicitly enabled and its effective policy enables it
- **THEN** the mailbox form indicates the setting is inherited from the policy

### Requirement: Learning endpoint and panel action
The API SHALL expose an endpoint that enqueues a Bayes learning action for a mailbox folder, taking the target server, the mailbox address, the kind (`spam` or `ham`) and an optional folder. The caller SHALL be required to have read access to the referenced mailbox; requests for a mailbox outside the caller's scope SHALL be refused. The endpoint SHALL return the identifier of the queued action so the panel can display its outcome, and SHALL NOT perform the training synchronously. The panel SHALL offer this as an action on the mailbox and spamfilter user screens.

#### Scenario: Train Junk from the panel
- **WHEN** an authorized user triggers "learn Junk as spam" for one of their mailboxes
- **THEN** a learning action is queued for that mailbox and the panel shows its pending state, then its final state

#### Scenario: Foreign mailbox refused
- **WHEN** a client requests learning for a mailbox they cannot read
- **THEN** the request is refused and no action row is written

#### Scenario: Request returns immediately
- **WHEN** a learning request is accepted for a large folder
- **THEN** the response returns without waiting for the training to run

### Requirement: Resync endpoint and panel action
The API SHALL expose an administrator-only endpoint that enqueues a spamfilter resync for a server, and the policy screen SHALL offer it as an action, so a policy edit — which writes no per-row event — can be pushed to Rspamd without touching every dependent row.

#### Scenario: Resync after a policy edit
- **WHEN** an administrator saves a policy and triggers resync for the mail server
- **THEN** a resync action is queued for that server and its state is shown in the panel

#### Scenario: Non-admin cannot resync
- **WHEN** a client calls the resync endpoint
- **THEN** the request is refused and no action row is written

### Requirement: E2E coverage of the spam filtering UI
End-to-end tests SHALL cover: creating a spamfilter whitelist entry and a blacklist entry from their separate screens, creating an access whitelist entry as a client and being refused a `client`-type entry, saving a policy with thresholds and triggering resync as administrator, and triggering a learn action from a mailbox.

#### Scenario: Suite passes against a seeded panel
- **WHEN** the spam filtering E2E suite runs against a dev server with seeded mail data
- **THEN** all listed flows complete without errors
