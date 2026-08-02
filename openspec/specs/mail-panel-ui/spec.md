# mail-panel-ui Specification

## Purpose
TBD - created by archiving change add-mail-module. Update Purpose after archive.
## Requirements

### Requirement: Mail module navigation and lists
The panel SHALL show a Mail module (visible per user permissions) with lists for: domains, mailboxes, aliases, forwards, catchalls, alias domains, transports, spamfilter policies, spamfilter users, spamfilter white/blacklists, and mail access lists. Each list SHALL support search on the primary name field and status where applicable. All strings SHALL go through the i18n layer (en first).

#### Scenario: Domain list shows only accessible domains
- **WHEN** a client opens the Mail domain list
- **THEN** only domains readable under the riud scope are listed with active status

### Requirement: Domain form with DKIM
The domain form SHALL mirror `mail_domain.tform.php`: server, domain, active, local_delivery, optional relay fields (admin), DKIM enable, selector, generate-key action, public key read-only, suggested DNS TXT, and indication whether DNS was auto-published. Client-side validation SHALL mirror API rules; API 422 errors SHALL display per field.

#### Scenario: Generate DKIM key from the form
- **WHEN** the user enables DKIM and clicks generate
- **THEN** private/public fields are filled (private not re-shown after save if policy hides it) and the suggested DNS TXT updates

#### Scenario: Managed zone shows auto-dns message
- **WHEN** the user saves DKIM on a domain that has a matching DNS zone
- **THEN** the form indicates DNS was updated automatically (parity with `dkim_auto_dns_txt`)

### Requirement: Mailbox form with tabs
The mailbox form SHALL provide tabs aligned with `mail_user.tform.php`: **Mailbox** (email, password, name, quota, cc, sender_cc, greylisting, access/postfix, disable imap/pop3/deliver/smtp/sieve/… flags), **Autoresponder** (enable, start/end, subject, text), **Filters** (move_junk, custom_mailfilter, purge_trash_days, purge_junk_days). Saving SHALL refresh pending datalog state indicators.

#### Scenario: Create mailbox via form
- **WHEN** the user saves a valid new mailbox for an existing domain
- **THEN** the mailbox appears in the list and detail shows pending until the daemon applies

#### Scenario: Autoresponder validation dates
- **WHEN** the user enables autoresponder with end before start
- **THEN** the form or API error is shown and the row is not saved

### Requirement: Forwarding and transport screens
The panel SHALL provide create/edit forms for aliases, forwards, catchalls, alias domains (each bound to the correct `mail_forwarding.type`) and for transports (`domain`, `transport`, `sort_order`, `active`).

#### Scenario: Create forward
- **WHEN** an authorized user saves a valid forward source→destination
- **THEN** it appears in the forwards list as active

### Requirement: Spamfilter and access screens
The panel SHALL provide admin/client-scoped screens for spamfilter policies, spamfilter users (email + policy + priority), white/black lists (rid + email), and global mail access lists (source, type, access, active).

#### Scenario: Policy assigned to user
- **WHEN** the user creates a spamfilter user linked to a policy
- **THEN** the row appears in the spamfilter users list with the policy name

### Requirement: E2E coverage of the Mail UI
agent-browser E2E tests SHALL cover: create mail domain, enable DKIM (generate key), create mailbox, create alias, create transport, create spamfilter policy + whitelist entry, and permission-filtered list visibility for a secondary client user. Screenshots go to `docs/prints/` (curated later to `docs/screenshots/`).

#### Scenario: E2E suite passes against a seeded panel
- **WHEN** the Mail E2E suite runs against a dev server with seeded data
- **THEN** all listed flows complete without errors
