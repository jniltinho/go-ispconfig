# client-ui

## ADDED Requirements

### Requirement: Client module navigation and lists
The panel SHALL show a Client module (visible when the session user's `modules` include `client`, always for admin) with sections: Clients list, Resellers list, Limit templates, Message templates, and Send message. Lists SHALL use the shared DataTable pattern (columns: company, contact, username, customer_no, status flags; search on username/company/contact/customer_no). All strings SHALL go through the i18n layer (`en.json` first).

#### Scenario: Client list shows only accessible clients
- **WHEN** a reseller opens the Clients list
- **THEN** only clients readable under the riud/reseller scope are listed

#### Scenario: Module hidden without permission
- **WHEN** a user without the `client` module (and non-admin) loads the panel
- **THEN** the Client module is absent from navigation

### Requirement: Client form with tabs
The client form SHALL mirror `client.tform.php` tabs: **Info** (username, password write-only, language, theme, locked, canceled, can_use_api, parent_client_id for admin, customer_no, notes), **Address** (company, contact names, gender, street, zip, city, state, country select from API, phone/mobile/fax, email, internet, bank fields), **Limits** (template master/additional multi-select, default servers, all relevant `limit_*` fields including web/dns/mail/ftp/shell/db/cron quotas and feature flags), **IP address** (`limit_web_ip`). Client-side validation mirrors API rules; API field errors display inline. Password is never shown on edit (blank = unchanged).

#### Scenario: Country select populated
- **WHEN** the user opens the Address tab
- **THEN** the country field offers printable names from the countries endpoint

#### Scenario: Limits tab shows template controls
- **WHEN** the user opens the Limits tab
- **THEN** master template select and additional templates controls are rendered

#### Scenario: Password blank on edit keeps previous
- **WHEN** the user saves an existing client leaving password empty
- **THEN** the update succeeds without changing the login password

### Requirement: Reseller form
The reseller form SHALL mirror `reseller.tform.php` with the same tab structure as clients, with `limit_client` editable (default allowing sub-clients). Creating a reseller uses the reseller API surface.

#### Scenario: Create reseller from UI
- **WHEN** an admin completes the reseller form with valid data and `limit_client = -1`
- **THEN** the reseller appears in the Resellers list and can log in with the given username

### Requirement: Limit template and message template screens
The panel SHALL provide limit-template list/form (`template_type`, name, limits tab) and message-template list/form (`template_type`, name, subject, body with placeholder help). Admins and resellers manage templates within their scope.

#### Scenario: Edit limit template
- **WHEN** a user opens a limit template and sets `limit_web_domain = 10`
- **THEN** saving persists the value and the list shows the updated template

### Requirement: Send message form
The Send message view SHALL port `client_message.php`: sender (default reseller/admin email), recipient selector (one client / all in scope), subject, body, optional load-from-template. Success and delivery-disabled states are shown via i18n toasts/alerts.

#### Scenario: Send to one client
- **WHEN** the user selects one client, enters subject/body and submits with SMTP configured
- **THEN** a success message is shown and the API send endpoint was called

#### Scenario: Delivery disabled feedback
- **WHEN** SMTP is not configured and the user submits send
- **THEN** an i18n error explaining delivery is disabled is shown

### Requirement: Delete confirmation with resource counts
Deleting a client SHALL show a confirmation that lists counts of owned resources (sites, DNS zones, …) and offers simple delete vs delete-everything, matching the intent of `client_del.php`.

#### Scenario: Confirmation shows owned site count
- **WHEN** the user deletes a client that owns two web domains
- **THEN** the confirmation UI indicates 2 websites before the user confirms

### Requirement: E2E coverage of the Client UI
agent-browser E2E tests SHALL cover: admin creates a reseller and a client under that reseller; login as reseller sees only own clients; edit limits and assign a template; create a welcome message template; attempt send-message; delete flow confirmation. Screenshots go to `docs/prints/`.

#### Scenario: E2E suite passes against a seeded panel
- **WHEN** the Client E2E suite runs against a dev server with seeded data
- **THEN** all listed flows complete without errors
