# panel-skeleton Specification

## Purpose
TBD - created by archiving change port-ispconfig3-to-go. Update Purpose after archive.
## Requirements
### Requirement: Embedded Vue 3 SPA shell
The panel SHALL be a Vue 3 + Vite + Tailwind v4 + Pinia SPA, built into `web/dist` and embedded in the binary. Layout SHALL follow the ISPConfig3 structure: top bar with module tabs (Sites, DNS, System, …), per-module sidebar navigation, content area with list views and tabbed forms. Corners SHALL be square (Tailwind radius 0) and all fonts vendored locally.

#### Scenario: Navigation structure
- **WHEN** an admin logs in
- **THEN** the top bar shows the enabled modules and selecting one loads its sidebar sections

#### Scenario: No external asset requests
- **WHEN** the panel is loaded with the browser network log open
- **THEN** no request goes to any third-party host (fonts, CSS, JS all same-origin)

### Requirement: List view and form primitives
The SPA SHALL provide reusable primitives: paginated/searchable data table (columns, row actions, active toggle) and tabbed form (rendered from the API form metadata, client-side validation hints, Save/Cancel), matching ISPConfig's listview/tform interaction patterns.

#### Scenario: Server-side pagination
- **WHEN** a list has more rows than the page size
- **THEN** the table paginates via API query parameters and shows total count

### Requirement: i18n-ready English UI
All UI strings SHALL come from JSON locale files through an i18n layer, with `en` as the only shipped locale initially; adding a language SHALL require only a new JSON file.

#### Scenario: Missing key fallback
- **WHEN** a locale lacks a key
- **THEN** the English string is shown (never a raw key) and a dev-mode warning is logged

### Requirement: E2E coverage with agent-browser
Panel flows (login, module navigation, list + form CRUD) SHALL be covered by agent-browser E2E tests run against a built binary.

#### Scenario: Login E2E
- **WHEN** the E2E suite runs against a fresh instance
- **THEN** it logs in with seeded credentials and asserts the dashboard renders

