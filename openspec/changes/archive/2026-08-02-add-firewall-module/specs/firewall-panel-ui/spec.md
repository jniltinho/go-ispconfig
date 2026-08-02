# firewall-panel-ui

## ADDED Requirements

### Requirement: System navigation entry for Firewall
The panel System module SHALL gain an admin-only **Firewall** sidebar section routing to `/system/firewall` (port of `admin` module nav item "Firewall" → `firewall_list.php`). Non-admin users SHALL not see the section.

#### Scenario: Admin sees Firewall under System
- **WHEN** an admin opens the panel
- **THEN** System → Firewall is listed and navigates to the firewall list

#### Scenario: Client does not see Firewall
- **WHEN** a client session loads the shell
- **THEN** the Firewall section is absent from the sidebar

### Requirement: Firewall list view
The list SHALL use `DataTable` with columns Active, Server, Open TCP ports, Open UDP ports (port of `firewall.list.php` / `en_firewall_list.lng`), support search/filter on those fields, and provide add / edit / delete actions. Delete SHALL confirm before calling the API. All strings SHALL go through i18n (`en.json`).

#### Scenario: Empty state
- **WHEN** no firewall rows exist
- **THEN** the table shows the empty state and an "Add Firewall record" action

#### Scenario: List reflects API data
- **WHEN** the API returns one row for server "ns1" with `tcp_port` containing `22,80` and `active=y`
- **THEN** the row is shown with active indicator, server name/id, and the port lists

### Requirement: Firewall edit form
The form SHALL use `TabbedForm` (single tab) driven by entity metadata: Server select (create only / immutable display on edit), Open TCP ports, Open UDP ports, Active checkbox — labels and help text ported from `en_firewall.lng` ("Separated by comma"). Client-side validation SHALL mirror the port regex; API field errors SHALL display inline. Help text SHALL note that the panel listen port and SSH port remain reachable even if omitted from the TCP list (lock-out guard).

#### Scenario: Create with defaults
- **WHEN** an admin opens the add form
- **THEN** TCP/UDP defaults match the tform defaults and Active is checked

#### Scenario: Validation error shown inline
- **WHEN** the admin submits `tcp_port` with illegal characters
- **THEN** the form shows the field error and does not navigate away

#### Scenario: Successful save returns to list
- **WHEN** the admin saves a valid new record
- **THEN** the API create succeeds and the list shows the new row

### Requirement: i18n coverage
All user-visible Firewall UI strings (nav, list headers, form labels, help, buttons, errors, empty state) SHALL be present in `frontend/src/locales/en.json`. No hard-coded English in the Vue templates beyond i18n keys.

#### Scenario: Locale keys resolve
- **WHEN** the Firewall list and form are rendered with the `en` locale
- **THEN** every label shows English text and no raw key paths are visible

### Requirement: E2E coverage of the Firewall UI
agent-browser E2E tests SHALL cover: admin login → open System → Firewall → create a record → edit ports → toggle active → delete, plus a non-admin session that cannot open the section. Screenshots for human review go to `docs/prints/` (not committed).

#### Scenario: E2E suite passes against a seeded panel
- **WHEN** the Firewall E2E suite runs against a built binary with a seeded admin and server row
- **THEN** all listed flows complete without errors
