# client-messaging

## ADDED Requirements

### Requirement: Message template CRUD
The system SHALL manage `client_message_template` rows with `template_type` ∈ {`welcome`, `gdpr`, `other`}, `template_name`, `subject` and `message` (all required non-empty for save), under riud permissions and datalog writes. Templates are scoped by ownership so resellers manage their own welcome templates.

#### Scenario: Create welcome template
- **WHEN** a reseller creates a template with `template_type = welcome`, a name, subject and body containing `{username}` and `{password}`
- **THEN** the row is stored and listed only within that reseller's scope

#### Scenario: Empty subject rejected
- **WHEN** a message template is saved with an empty subject
- **THEN** validation fails and no row is written

### Requirement: Placeholder substitution
Sending or previewing a message SHALL replace placeholders in subject/body with values from the target `client` row and related fields, porting the PHP welcome/message substitution set (at minimum `{username}`, `{password}` only when plaintext is available in-request, `{company_name}`, `{contact_name}`, `{contact_firstname}`, `{email}`, `{customer_no}`, and other non-limit client columns exposed by `client_message.php`). Password placeholders MUST NOT be filled from stored password hashes.

#### Scenario: Welcome placeholders rendered
- **WHEN** a welcome template body contains `Hello {contact_name}, user {username}` and is rendered for a client
- **THEN** the output contains the client's contact name and username

#### Scenario: Stored hash not injected as password
- **WHEN** a template containing `{password}` is rendered outside of a create flow that still holds the plaintext
- **THEN** `{password}` is left empty or replaced with a neutral marker, never the hash

### Requirement: Optional SMTP delivery
Actual email delivery SHALL use an optional generic SMTP relay configured in `config.toml`. When the relay is not configured or is disabled, template CRUD and preview remain available, but send operations SHALL fail with a clear i18n error key indicating delivery is disabled. No dependency on the future mail module is required.

#### Scenario: Send disabled without SMTP
- **WHEN** no SMTP relay is configured and a send-message request is made
- **THEN** the API returns an error that delivery is disabled and no message is handed to a transport

#### Scenario: Send succeeds with SMTP configured
- **WHEN** SMTP is configured and a send-message request targets a client with a valid email
- **THEN** one message is submitted to the relay with the rendered subject/body and configured sender

### Requirement: Welcome email on client create
When a client is created with a non-empty email and a `welcome` template exists for the creator's group, the system SHALL render and attempt to send that template (including plaintext password only from the create request). Failure to send MUST NOT roll back the client create; it SHALL be reported as a warning in the API response or log.

#### Scenario: Welcome sent on create
- **WHEN** SMTP is configured, a welcome template exists, and a client with email is created
- **THEN** the client is created and a welcome message is submitted to the relay

#### Scenario: Missing welcome template skips send
- **WHEN** no welcome template exists for the creator's group
- **THEN** client create still succeeds and no send is attempted

### Requirement: Panel send-message recipients
The send-message operation SHALL support: a single `client_id` recipient; all clients (admin); or all direct children of the current reseller (`parent_client_id = current`). Only non-canceled clients with non-empty email are messaged. Sender defaults to the acting user's client email when available.

#### Scenario: Reseller broadcasts to children only
- **WHEN** a reseller sends a message with recipient "all"
- **THEN** only clients with `parent_client_id` equal to that reseller and non-empty email receive the message
