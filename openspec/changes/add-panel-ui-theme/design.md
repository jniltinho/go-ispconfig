# Design: add-panel-ui-theme

## Context

The `port-ispconfig3-to-go` foundation ships a working Vue 3 + Vite + Tailwind v4 + Pinia shell (login, topbar module navigation, sidebar, DataTable and TabbedForm primitives) with default Tailwind styling. The visual research in `docs/research/ispconfig3-theme.md` documents the original theme (`base/ispconfig3_install/interface/web/themes/default/`): a single light scheme built on Bootstrap 3 + SASS, with a compiled-but-unused dark scheme, a proprietary `ispconfig` icon-font, fixed 950px layout, gradient buttons with 2px dark bottom borders, inset shadows, and 4px border radius.

This change layers the theme on top of the skeleton without changing any primitive's behavior contract. Constraints: square corners everywhere (project owner requirement), zero external requests (fonts vendored), English-first artifacts, screenshots workflow (`docs/prints/` local, `docs/screenshots/` committed).

## Goals / Non-Goals

**Goals:**
- Faithful ISPConfig3 identity: same palette, layout structure, table/form/alert patterns.
- Modernized execution: fluid width, solid buttons, external shadows, short transitions, AA-contrast interactive states, empty states, skeletons, dark mode toggle.
- Single source of truth for tokens (Tailwind v4 `@theme`) consumed by every component.
- Icon-font fully replaced by Lucide with a documented 1:1 mapping.
- Responsive: collapsible sidebar, horizontally scrolling tables.

**Non-Goals:**
- White-label, multiple themes, theme editor, RTL.
- Changes to primitive behavior (pagination, form metadata, i18n contracts).
- Porting Select2/Datetimepicker/Chart.js equivalents (out of scope; native inputs suffice for now).

## Decisions

### D1 — Tokens as Tailwind v4 `@theme` CSS variables, semantic names
Define all colors, radius, and fonts in one `@theme` block (e.g. `web/src/assets/theme.css`) using semantic names (`--color-brand`, `--color-surface`, `--color-thead`, …) rather than raw Tailwind palette overrides. Rationale: Tailwind v4 generates utilities (`bg-brand`, `text-link`) directly from `@theme`, and dark mode becomes a variable re-assignment, not per-component classes. Alternative considered: `tailwind.config` JS theme extension — rejected, v4 is CSS-first and the project already uses v4.

Token values (light): brand `#C70F19`, brand-dark `#9C0C14`, bg `#F2F5F7`, surface `#FFFFFF`, text `#3C444B`, link `#2371CA`, success `#3CB355`, border `#D3D7DA`, info-bg `#DFEAF6` / info-border `#CEDDED` / info-text `#698296`, danger-bg `#F7DFDF` / danger-border `#DCB2B3` / danger-text `#95686B`, thead `#3E474E` (flat — no gradient), dashlet `#E1E4E9`. `--radius-*: 0` for every radius token so no utility can produce a rounded corner.

### D2 — Dark mode via `.dark` class + CSS variable overrides
A `.dark` selector re-assigns the semantic variables (values guided by the original compiled dark scheme in the SASS sources). Toggle button in the topbar writes the preference to `localStorage` and sets the class on `<html>`; initial state honors `prefers-color-scheme` when no stored preference exists. Alternative: Tailwind `dark:` variants per component — rejected, duplicates every color decision and drifts.

### D3 — Icons: `lucide-vue-next`, tree-shaken, central mapping module
One module (e.g. `web/src/icons.ts`) exports the mapping so the icon-font codepoints have exactly one Lucide counterpart: dashboard→`LayoutDashboard`, sites→`Globe`, dns→`Network`, mail→`Mail`, client→`Users`, monitor→`Activity`, system→`Settings`, tools→`Wrench`, help→`CircleHelp`; utility: lens→`Search`, filter→`Filter`, edit→`Pencil`, delete→`Trash2`, link→`ExternalLink`, stats→`BarChart3`, loginas→`LogIn`, calendar→`Calendar`. Rationale: tree-shaking keeps the bundle small and imports stay grep-able; an icon-font would reintroduce the asset we are removing. Alternative: inline SVG sprites — more manual upkeep for no benefit.

### D4 — Typography: vendored Inter woff2
Download Inter (Latin subset, weights 400/600/700, woff2) into `web/static/fonts/`, commit the files, declare `@font-face` locally with `font-display: swap` and a system-stack fallback. Base size 14px to match original density. Rationale: original used the system stack; Inter modernizes while the vendored files keep the zero-CDN guarantee. The E2E network check enforces this.

### D5 — Buttons: solid, flat, three variants
Replace the gradient + 2px-darker-bottom-border signature with solid fills: default (surface bg, border, text), primary/success (`success` green), danger (`brand` red — also the logout button). Hover darkens ~8%, focus shows a visible 2px outline, transitions ≤150ms. Rationale: keeps the original color semantics (green=confirm, red=logout/destructive) while dropping the 2009-era gradient.

### D6 — Layout: fluid with max-width, CSS grid shell
Topbar (logo, module buttons icon-over-title, global search, dark-mode toggle, red logout) + optional per-module sidebar + fluid content (`max-w` generous, e.g. none or ~1600px, padding-based gutters) replacing the fixed 950px. Sidebar collapses to an off-canvas drawer below `lg`; state in Pinia. Tables wrap in `overflow-x-auto` containers instead of the original card-stacking at 600px.

### D7 — Table identity: flat dark thead, filter row, zebra, right actions
`thead` gets flat `#3E474E` with white text (drop the gradient), second header row hosts per-column filter inputs + filter button (the signature ISPConfig trait), odd rows zebra `#F2F5F7`, hover `#DFEAF6`, actions right-aligned as icon buttons (delete requires confirm). Loading renders skeleton rows; zero results renders an empty state with an icon and hint text.

### D8 — Screenshot validation loop
agent-browser drives the built binary, saving PNGs to `docs/prints/` (git-ignored) for each key screen (login, dashboard, list, tabbed form, dark mode, mobile widths). Human approves; approved images are copied to `docs/screenshots/` and committed. Rationale: keeps the repo free of churn from unapproved iterations while still versioning the accepted look.

## Risks / Trade-offs

- [Lucide icons read differently from the original icon-font glyphs] → 1:1 mapping reviewed against original screenshots during the print-approval loop; adjust individual picks before committing screenshots.
- [Dark scheme values from the never-shipped compiled dark SASS may have poor contrast] → treat them as reference only; every dark token pair is checked for WCAG AA (4.5:1 text) before acceptance.
- [Flat thead + square corners can look austere] → compensate with the subtle external shadow on cards/tables and adequate row padding; validated visually in the screenshot loop.
- [Vendored Inter adds ~100KB to the binary] → Latin subset + 3 weights only; acceptable for a self-hosted panel, and `font-display: swap` avoids FOIT.
- [Horizontal table scroll on mobile is less touch-friendly than cards] → deliberate: preserves column filters and density; sticky first column can be added later if needed.

## Migration Plan

Frontend-only. Ship behind nothing: the theme replaces the default styling in one change; `npm run build` regenerates `web/dist`, the Go binary embeds it. Rollback = revert the change and rebuild. No data or API impact.

## Open Questions

- Exact `max-w` for content (unbounded fluid vs ~1600px cap) — decide during screenshot review.
- Whether the global search field ships functional or as a styled placeholder wired later — depends on skeleton search API availability at implementation time.
