# Tasks: add-panel-ui-theme

## 1. Foundation: tokens, fonts, icons

- [x] 1.1 Create the Tailwind v4 `@theme` block with all semantic light-scheme tokens (brand, bg, surface, text, link, success, border, info, danger, thead, dashlet) and all radius tokens set to 0; commit
- [x] 1.2 Download Inter woff2 (Latin, 400/600/700) into `web/static/fonts`, add local `@font-face` with system fallback and 14px base size; verify build produces no external URL; commit
- [x] 1.3 Add `lucide-vue-next` and create the central icon mapping module (modules: dashboard, sites, dns, mail, client, monitor, system, tools, help; utility: search, filter, edit, delete, external-link, stats, login-as, calendar); commit
- [x] 1.4 Add `.dark` variable overrides derived from the original dark scheme reference, checking each text/background pair for WCAG AA (4.5:1); commit

## 2. Shell: topbar, sidebar, layout

- [ ] 2.1 Style the topbar: logo, icon-over-title module buttons with active state and brand hover, global search field with search icon; commit
- [ ] 2.2 Add the red solid logout button ("Logout <username>") and the dark-mode toggle (localStorage persistence + `prefers-color-scheme` initial state, no flash on reload); commit
- [ ] 2.3 Convert the layout to fluid width with consistent gutters (remove any fixed content width) and style the per-module sidebar; commit
- [ ] 2.4 Make the sidebar collapse into an off-canvas drawer below the large breakpoint with a toggle button (state in Pinia); commit

## 3. Components: buttons, tables, forms, alerts

- [ ] 3.1 Implement the solid button variants (default/success/danger), ≤150ms transitions, hover states, and visible AA-contrast focus outlines applied globally; commit
- [ ] 3.2 Restyle DataTable: flat `#3E474E` thead with white text, second header row with per-column inline filter inputs + filter button, zebra odd rows `#F2F5F7`, hover `#DFEAF6`, right-aligned icon action buttons with delete confirmation; commit
- [ ] 3.3 Add DataTable loading skeleton rows and the zero-results empty state (icon + hint text); wrap tables in `overflow-x-auto` so narrow viewports scroll the table, never the page; commit
- [ ] 3.4 Restyle TabbedForm: surface card, flat square tabs with 1px separators and connected active tab, horizontal rows with `:`-suffixed labels, fieldset legends as sub-headings, Save (success) / Cancel (default) buttons; commit
- [ ] 3.5 Implement info (`#DFEAF6`) and danger (`#F7DFDF` with "Error" label + message list) alert components; commit

## 4. Screens: login and dashboard

- [ ] 4.1 Restyle the login screen: centered square card on `#F2F5F7` with logo, credential fields, "stay logged in" checkbox, password-lost link, solid submit button; commit
- [ ] 4.2 Restyle the dashboard as dashlet cards (`#E1E4E9` light background, large module icon, title, full-width button); commit

## 5. Visual validation

- [ ] 5.1 Ensure `docs/prints/` is git-ignored; use agent-browser to capture screenshots of login, dashboard, list view, tabbed form, dark mode, and a narrow-viewport view into `docs/prints/` for human review
- [ ] 5.2 Iterate on reviewer feedback until approved; copy approved screenshots to `docs/screenshots/` and commit

## 6. E2E and verification

- [ ] 6.1 Write agent-browser E2E for the themed flows: login, module navigation, list view (filter row + pagination), tabbed form save/cancel; commit
- [ ] 6.2 Add E2E assertions for theme traits: sampled elements report `border-radius: 0`, thead computed background is `#3E474E`, dark-mode toggle switches the scheme and persists across reload; commit
- [ ] 6.3 Add the E2E network check asserting every request during the suite targets the panel origin (no external hosts); run the full suite against a fresh built binary and commit
