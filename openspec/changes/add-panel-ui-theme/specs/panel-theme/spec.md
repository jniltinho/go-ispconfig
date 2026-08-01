# panel-theme

## ADDED Requirements

### Requirement: Design token system with square corners
The panel SHALL define all theme colors, radii, and font families as Tailwind v4 `@theme` CSS variables with semantic names, used by every component (no hard-coded hex values in components). The light palette SHALL match the ISPConfig3 original: brand `#C70F19`, body background `#F2F5F7`, surface `#FFFFFF`, text `#3C444B`, link `#2371CA`, success `#3CB355`, border `#D3D7DA`, info background `#DFEAF6`, danger background `#F7DFDF`, table header `#3E474E`. Every radius token SHALL be `0` so no rendered element has rounded corners.

#### Scenario: Tokens drive utilities
- **WHEN** a component needs the brand color or a surface background
- **THEN** it uses a token-derived utility class (e.g. `bg-brand`, `bg-surface`) and changing the token value in the `@theme` block restyles the component without editing it

#### Scenario: Square corners everywhere
- **WHEN** any page of the panel is rendered (buttons, cards, tables, tabs, inputs, modals, alerts)
- **THEN** every element has `border-radius: 0` (computed style), with no exceptions

### Requirement: Topbar preserving ISPConfig3 identity
The topbar SHALL contain, left to right: the logo, one button per enabled module rendered as a Lucide icon above a bold title (mapping: dashboard→LayoutDashboard, sites→Globe, dns→Network, mail→Mail, client→Users, monitor→Activity, system→Settings, tools→Wrench, help→CircleHelp), a global search field with a search icon, a dark-mode toggle, and a red (`brand`) logout button labeled with the logged-in username. The active module SHALL be visually distinct and module buttons SHALL show a brand-colored hover state.

#### Scenario: Module buttons render icon over title
- **WHEN** an admin is logged in
- **THEN** each enabled module appears in the topbar as an icon above its bold title, and the active module is visually highlighted

#### Scenario: Red logout button
- **WHEN** the topbar renders for user `admin`
- **THEN** a solid `#C70F19` logout button reading "Logout admin" appears at the right and logs the user out when clicked

### Requirement: Fluid responsive layout with collapsible sidebar
The panel layout SHALL be fluid (no fixed 950px width): content spans the available width with consistent gutters. Below the large breakpoint the per-module sidebar SHALL collapse into a toggleable off-canvas drawer, and data tables SHALL scroll horizontally inside their own container instead of reflowing into stacked cards; the page body SHALL never scroll horizontally.

#### Scenario: Sidebar collapses on narrow viewports
- **WHEN** the viewport is narrower than the large breakpoint
- **THEN** the sidebar is hidden behind a toggle button and opens as an overlay drawer

#### Scenario: Table overflow scrolls in place
- **WHEN** a data table is wider than a narrow viewport
- **THEN** the table scrolls horizontally within its container and the page itself has no horizontal scrollbar

### Requirement: Data table visual identity
Data tables SHALL render: a flat dark header (`#3E474E` background, white text, no gradient); a second header row with per-column inline filter inputs and a filter action button; zebra striping (odd rows `#F2F5F7`); row hover highlight (`#DFEAF6`); and row actions right-aligned as icon buttons (edit, external link, stats, delete), where delete uses a danger style and requires confirmation. While loading, the table SHALL show skeleton rows; with zero rows it SHALL show an empty state with an icon and hint text instead of a bare empty body.

#### Scenario: Signature header with inline filters
- **WHEN** a list view renders
- **THEN** the table shows a dark header row of column titles and a second row of per-column filter inputs with a filter button

#### Scenario: Loading skeleton
- **WHEN** a list view is fetching data
- **THEN** placeholder skeleton rows are shown until data arrives

#### Scenario: Empty state
- **WHEN** a list query returns zero rows
- **THEN** the table body shows an empty state (icon + explanatory text) rather than an empty area

### Requirement: Tabbed form visual identity
Tabbed forms SHALL render as a surface card with flat square tabs across the top (active tab visually connected to the content, 1px separators), horizontal form rows with left-aligned labels that end with a `:` suffix, fieldset legends as sub-headings, and Save/Cancel buttons styled as solid success and default variants respectively.

#### Scenario: Flat tabs and label colons
- **WHEN** a tabbed form renders
- **THEN** tabs are square with the active tab highlighted, and every field label displays a trailing `:`

### Requirement: Solid button system
Buttons SHALL use solid flat fills with three variants — default (surface + border), success (`#3CB355`), danger (`#C70F19`) — with no gradients and no 2px darker bottom border. All interactive elements SHALL have hover states, visible keyboard focus outlines, and text contrast meeting WCAG AA (≥ 4.5:1); transitions SHALL be 150ms or shorter.

#### Scenario: Accessible focus state
- **WHEN** a button or link receives keyboard focus
- **THEN** a visible focus outline is rendered and its contrast against the background meets WCAG AA

### Requirement: Alerts in original style
Info alerts SHALL use the `#DFEAF6` background with matching border and text tokens; error alerts SHALL use `#F7DFDF` with a leading "Error" label and a list of messages, mirroring the original `.alert-notification` / `.alert-danger` composition.

#### Scenario: Error alert composition
- **WHEN** a form submit returns validation errors
- **THEN** a danger alert with `#F7DFDF` background shows an "Error" label followed by the list of error messages

### Requirement: Login screen and dashboard dashlets
The login screen SHALL be a centered surface card on the `#F2F5F7` background containing the logo, username and password fields, a "stay logged in" checkbox, a password-lost link, and a solid submit button. The dashboard SHALL render modules as dashlet cards (`#E1E4E9` background in light mode) with a large module icon, title, and a full-width action button.

#### Scenario: Centered login card
- **WHEN** an unauthenticated user opens the panel
- **THEN** a centered square-cornered card with logo, credential fields, stay-logged-in checkbox and password-lost link is shown

#### Scenario: Dashboard dashlets
- **WHEN** the dashboard loads
- **THEN** each available module appears as a dashlet card with icon, title and a full-width button linking into the module

### Requirement: Dark mode toggle
The panel SHALL provide a dark color scheme implemented by re-assigning the semantic token variables under a `.dark` root class. A topbar toggle SHALL switch schemes, persist the choice in `localStorage`, and on first visit the scheme SHALL follow `prefers-color-scheme`. All dark-scheme text/background token pairs SHALL meet WCAG AA contrast.

#### Scenario: Toggle persists across reloads
- **WHEN** the user enables dark mode and reloads the page
- **THEN** the panel renders in dark mode without a flash of the light scheme preference being lost

#### Scenario: System preference honored initially
- **WHEN** a user with OS dark mode set visits with no stored preference
- **THEN** the panel renders the dark scheme

### Requirement: Locally vendored typography and zero external requests
The panel SHALL use the Inter typeface served from woff2 files committed under `web/static/fonts` and declared via local `@font-face` with a system-stack fallback; base font size SHALL be 14px. No page of the themed panel SHALL issue a network request to any non-origin host (fonts, icons, CSS, JS all bundled or same-origin).

#### Scenario: No external requests
- **WHEN** any themed page is loaded with the network log recording
- **THEN** every request targets the panel's own origin

### Requirement: Visual validation via screenshots
Theme delivery SHALL include agent-browser screenshots of the key screens (login, dashboard, list view, tabbed form, dark mode, and a narrow-viewport view) written to `docs/prints/` (not committed) for human review; screenshots approved by the reviewer SHALL be copied to `docs/screenshots/` and committed.

#### Scenario: Approved screenshots committed
- **WHEN** the reviewer approves the rendered screens from `docs/prints/`
- **THEN** the approved images exist in `docs/screenshots/` in the repository and `docs/prints/` remains git-ignored

### Requirement: E2E coverage of themed flows
agent-browser E2E tests SHALL exercise login, module navigation, a list view (including filter row and pagination) and a tabbed form against a built binary with the theme applied, asserting the theme's observable traits (square corners on a sampled element, dark thead color, dark-mode toggle effect) and asserting that no request left the origin host during the run.

#### Scenario: Themed E2E run passes
- **WHEN** the E2E suite runs against a fresh themed build
- **THEN** login, navigation, list and form flows pass, sampled elements report `border-radius: 0`, and the network log contains only same-origin requests
