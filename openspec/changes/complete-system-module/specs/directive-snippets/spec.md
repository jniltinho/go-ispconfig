# directive-snippets

## ADDED Requirements

### Requirement: Snippet catalogue
The panel SHALL provide an admin-only `System → Directive Snippets` list and form over `directive_snippets`, exposing name, type (`nginx`, `apache`, `php`, `proxy`), the snippet body, `customer_viewable`, `required_php_snippets` and `active`. Port of `interface/web/admin/directive_snippets_list.php` and `directive_snippets_edit.php`.

#### Scenario: A snippet is created
- **WHEN** an admin saves a named nginx snippet
- **THEN** the row exists, is journalled, and appears in the list

#### Scenario: Only admins may create one
- **WHEN** a client session posts to the snippet endpoint
- **THEN** the request is refused

### Requirement: A snippet reaches the generated vhost at a named insertion point
The nginx and apache2 vhost renderers SHALL expose named insertion points and SHALL emit the snippet referenced by `web_domain.directive_snippets_id` at the point matching its type. A snippet SHALL NOT be appended to arbitrary positions in the file.

#### Scenario: nginx snippet is emitted
- **WHEN** a site references an active nginx snippet and its vhost is re-rendered
- **THEN** the snippet body appears at the nginx insertion point of the generated vhost

#### Scenario: Removing the reference removes the text
- **WHEN** the site's snippet reference is cleared and the vhost is re-rendered
- **THEN** the snippet body is absent from the generated file

#### Scenario: An inactive snippet is not emitted
- **WHEN** a referenced snippet has `active = n` and the vhost is re-rendered
- **THEN** the snippet body is absent

### Requirement: Snippet type must match the target server
The system SHALL refuse to save or to attach a snippet whose type does not match the target server's `server_type`.

#### Scenario: Apache snippet on an nginx server is refused
- **WHEN** an apache-typed snippet is attached to a site on a server whose `server_type` is nginx
- **THEN** the save is refused with a field error

#### Scenario: Matching type is accepted
- **WHEN** an nginx-typed snippet is attached to a site on an nginx server
- **THEN** the save succeeds

### Requirement: A snippet that breaks the configuration is not applied
The renderer SHALL run its existing configuration test before applying, and SHALL leave the previous vhost in place when the test fails, recording the failure in the datalog error state.

#### Scenario: Broken snippet does not take the site down
- **WHEN** a snippet containing invalid syntax is attached and the vhost is re-rendered
- **THEN** the configuration test fails, the previously working vhost remains on disk, the service is not reloaded, and the error is recorded

#### Scenario: The error is visible to the operator
- **WHEN** such a failure has been recorded
- **THEN** the site form shows the datalog error state

### Requirement: Client visibility and limit
A snippet SHALL be offered to a non-admin only when `customer_viewable` is `y`, and the number a client may attach SHALL be bounded by the existing `limit_directive_snippets` client limit.

#### Scenario: Non-viewable snippet is hidden from a client
- **WHEN** a client opens the site form and a snippet has `customer_viewable = n`
- **THEN** that snippet is not offered

#### Scenario: Limit is enforced
- **WHEN** a client at its `limit_directive_snippets` attaches one more
- **THEN** the request is refused with the limit error

#### Scenario: Admin is not bounded by the client limit
- **WHEN** an admin attaches a snippet to a client's site
- **THEN** the client limit does not apply
