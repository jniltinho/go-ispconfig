# mail-rest-api Specification

## Purpose
TBD - created by archiving change add-mail-module. Update Purpose after archive.
## Requirements

### Requirement: Mail domain endpoints
The REST API SHALL expose domain operations porting `remote.d/mail.inc.php`: create, get by id, get by domain name, list, update, delete, set status (active/inactive), and DKIM generate — under `/api/mail/domains`, session/token authenticated, permission-scoped, returning JSON, writing `{old,new}` datalog rows on mutations.

#### Scenario: Create domain
- **WHEN** `POST /api/mail/domains` is called with valid domain fields by an authorized user
- **THEN** the domain is created with the caller's ownership fields, a datalog row is written and the new `domain_id` returned

#### Scenario: Lookup by domain name
- **WHEN** the get-by-domain endpoint is called with an existing accessible domain
- **THEN** the domain record is returned; 404 when no accessible domain matches

#### Scenario: Set status inactive
- **WHEN** set-status sets a domain to inactive
- **THEN** `mail_domain.active` becomes `n` and a datalog update is written for the domain's `server_id`

### Requirement: Mailbox endpoints
The REST API SHALL expose mailbox CRUD and list-by-client under `/api/mail/mailboxes` (port of `mail_user_add/get/get_all_by_client/update/delete`). Create SHALL verify the email domain exists as a primary `mail_domain` (not only aliasdomain). Passwords SHALL be hashed with CRYPTMAIL before storage; responses SHALL NOT return the password hash in list views.

#### Scenario: Create mailbox
- **WHEN** `POST /api/mail/mailboxes` is called with valid email, password and quota for an existing mail domain
- **THEN** the mailbox is created, password is stored hashed, datalog insert is written and the new `mailuser_id` returned

#### Scenario: Create mailbox for unknown domain fails
- **WHEN** create is called for an email whose domain has no primary `mail_domain`
- **THEN** the API returns an error (`mail_domain_does_not_exist` parity) and no row is written

### Requirement: Forwarding endpoints (alias, forward, catchall, aliasdomain)
The REST API SHALL expose typed CRUD for `mail_forwarding` discriminators — `/api/mail/aliases`, `/api/mail/forwards`, `/api/mail/catchalls`, `/api/mail/alias-domains` — porting `mail_alias_*`, `mail_forward_*`, `mail_catchall_*`, `mail_aliasdomain_*`. Each surface forces its `type` value server-side.

#### Scenario: Create alias
- **WHEN** `POST /api/mail/aliases` is called with source and destination emails
- **THEN** a `mail_forwarding` row with `type=alias` is created with a datalog insert

#### Scenario: List catchalls is type-filtered
- **WHEN** the catchalls list endpoint is called
- **THEN** only rows with `type=catchall` visible under riud are returned

### Requirement: Transport endpoints
The REST API SHALL expose `mail_transport` CRUD under `/api/mail/transports` (port of `mail_transport_*`), enforcing unique `(server_id, domain)`.

#### Scenario: Duplicate transport domain rejected
- **WHEN** a transport is created with a domain that already has a transport on the same server
- **THEN** the API returns a uniqueness validation error

### Requirement: Access and spamfilter endpoints
The REST API SHALL expose:
- `/api/mail/access` for `mail_access` CRUD (whitelist/blacklist semantics of remote `mail_whitelist_*` / `mail_blacklist_*`)
- `/api/mail/spamfilter/policies` for `spamfilter_policy` CRUD (`mail_policy_*`)
- `/api/mail/spamfilter/users` for `spamfilter_users` CRUD (`mail_spamfilter_user_*`)
- `/api/mail/spamfilter/wblists` for `spamfilter_wblist` CRUD with `wb` filter or dedicated white/black subpaths (`mail_spamfilter_whitelist_*` / `_blacklist_*`)

#### Scenario: Create spamfilter whitelist entry
- **WHEN** a wblist entry is posted with `wb=W`, valid rid and email
- **THEN** the row is created and a datalog insert is written

### Requirement: Swagger documentation for all mail endpoints
Every mail endpoint SHALL carry swaggo annotations (summary, params, request/response models, security, error codes) and appear in the embedded Swagger UI; CI SHALL fail when generated swagger output is stale.

#### Scenario: Mail endpoints visible in Swagger UI
- **WHEN** `/swagger/` is opened
- **THEN** the mail domain, mailbox, forwarding, transport and spamfilter endpoints are listed with typed request/response schemas

### Requirement: Datalog pending and error state on reads
List/get responses for mail domains and mailboxes SHALL include foundation datalog state decoration (`_datalog_state` pending/error and `_datalog_error` when quarantined), matching the sites/dns pattern.

#### Scenario: Pending domain after create
- **WHEN** a domain is created and the daemon has not yet processed its datalog row
- **THEN** GET/list for that domain shows `_datalog_state=pending`
