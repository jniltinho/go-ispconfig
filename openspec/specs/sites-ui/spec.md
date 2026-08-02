# sites-ui Specification

## Purpose
TBD - created by archiving change add-web-nginx-module. Update Purpose after archive.
## Requirements
### Requirement: Sites module navigation and domain list
The Vue panel SHALL gain a Sites module (topbar entry + sidebar sections) whose default view lists web domains using the foundation DataTable: columns active, server, domain, type; server-side pagination, sorting and filtering through the sites API; row click opens the edit form; an Add button opens the create form. All strings SHALL go through the i18n layer (English catalog).

#### Scenario: Domain list loads
- **WHEN** a logged-in user opens the Sites module
- **THEN** the domain DataTable renders rows from `/api/sites/web-domains` respecting the user's riud scope

### Requirement: Metadata-driven tabbed domain form
The domain create/edit view SHALL render its tabs (Domain, Redirect, SSL, Statistics, Backup, Options) and fields from the form metadata endpoint — field order, input types (text, select, checkbox, password, textarea), options and defaults come from the server; client-side validation mirrors the validator hints and server 422 responses are mapped onto the fields. Saving submits to the sites API and returns to the list on success.

#### Scenario: Tabs render from metadata
- **WHEN** the edit form loads for a vhost domain
- **THEN** the six tabs appear with their fields as described by the metadata, without hardcoded per-tab Vue components

#### Scenario: Server validation errors shown inline
- **WHEN** a save returns 422 with field errors
- **THEN** each error appears at its field and the form stays on the offending tab

### Requirement: SSL tab actions
The SSL tab SHALL support the ISPConfig actions: request self-signed certificate (`ssl_action=create`), save pasted certificate (`save`), delete certificate (`del`), and a Let's Encrypt toggle; cert/key/request textareas display the stored values.

#### Scenario: Enable Let's Encrypt from the form
- **WHEN** the user checks the Let's Encrypt option and saves
- **THEN** the API receives `ssl=y, ssl_letsencrypt=y` and the pending state indicator shows until the daemon applies the change

### Requirement: Pending/error state indicator
The domain list and form SHALL show the record's datalog state: pending (change queued, daemon not yet processed) and error (daemon recorded a failure) with the error message accessible.

#### Scenario: Broken custom directives feedback
- **WHEN** the daemon rejected a blacklisted nginx directive for a domain
- **THEN** the domain shows an error indicator and the user can read the recorded message

