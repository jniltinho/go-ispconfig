# Proposal: add-panel-ui-theme

## Why

The foundation change (`port-ispconfig3-to-go` / `panel-skeleton`) delivers a functional Vue 3 + Tailwind v4 shell with DataTable/TabbedForm primitives, but no visual identity. This change delivers the actual theme: a recreation of the ISPConfig3 look (same structure, colors and interaction patterns users already know) modernized — fluid width, solid buttons, subtle external shadows, accessible focus states, dark mode toggle — so the panel feels familiar to ISPConfig3 users while looking current.

## What Changes

- **Tailwind v4 design tokens** (`@theme`) derived from the original SASS palette (`base/ispconfig3_install/interface/web/themes/default/assets/stylesheets/themes/default/colors.sass`): brand `#C70F19`, body bg `#F2F5F7`, surface `#FFFFFF`, text `#3C444B`, link `#2371CA`, success `#3CB355`, border `#D3D7DA`, info bg `#DFEAF6`, danger bg `#F7DFDF`, dark thead `#3E474E`. **Radius 0 everywhere (square corners — project owner requirement).**
- **Preserved identity** (from `interface/web/themes/default/templates/main.tpl.htm` and the SASS components): topbar with icon-over-title module buttons, red logout button, global search field, dark-header tables with inline per-column filters + zebra striping + right-aligned row actions, tabbed forms with flat tabs and `:`-suffixed labels, info/danger alerts in the original style, centered card login, dashboard dashlets.
- **Icon migration**: the proprietary `ispconfig` icon-font (PUA codepoints, `assets/fonts/ispconfig.*`) is replaced by a 1:1 Lucide mapping for modules (dashboard, sites, dns, mail, client, monitor, system, tools, help) and row/utility actions (search, filter, edit, delete, link, stats).
- **Modernizations** replacing dated ISPConfig3 traits: fluid layout instead of fixed 950px, solid buttons instead of gradient + 2px dark bottom border, short transitions instead of 500ms, subtle external shadows instead of inset, WCAG AA hover/focus states, empty states, loading skeletons, dark mode toggle (original compiled dark scheme used as color reference), locally vendored Inter woff2 in `web/static/fonts` (zero CDN).
- **Responsive behavior**: collapsible sidebar; tables scroll horizontally (no card-stacking like the BS3 original).
- **Visual validation workflow**: agent-browser screenshots into `docs/prints/` (not committed) for human approval; approved shots copied to `docs/screenshots/` (committed).
- **E2E**: agent-browser tests of login, navigation, list and form flows with the theme applied, plus a check that no request leaves the origin host.

## Capabilities

### New Capabilities

- `panel-theme`: design tokens, component styling (topbar, sidebar, tables, forms, buttons, alerts, login, dashlets), iconography, typography, dark mode, and responsive rules for the panel.

### Modified Capabilities

(none — `panel-skeleton` requirements are unchanged; this change styles the primitives it delivers without altering their behavior contracts)

## Impact

- Frontend only: `web/` (Tailwind theme CSS, Vue components, Lucide icon imports, vendored fonts in `web/static/fonts`). No Go code changes beyond the embedded `web/dist` rebuild.
- New dev dependency: `lucide-vue-next` (tree-shaken icon components). New vendored asset: Inter woff2 files (committed, no CDN).
- Reference PHP/SASS sources (read-only): `base/ispconfig3_install/interface/web/themes/default/` — `assets/stylesheets/themes/default/colors.sass`, `templates/main.tpl.htm`, `templates/tabbed_form.tpl.htm`, list templates (`*_list.htm`), login template. Research summary: `docs/research/ispconfig3-theme.md`.
- Docs: screenshots in `docs/screenshots/` (committed); `docs/prints/` stays git-ignored.

## Non-goals

- White-label support or multiple selectable themes (single theme, light + dark scheme only).
- Theme editor / user-customizable colors.
- RTL layout support.
- Any behavioral change to the `panel-skeleton` primitives (pagination, form metadata, i18n) — visual layer only.
