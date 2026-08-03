# Proposal: ui-forms-tables-qa

## Why

Hosting panel UX quality drifts as modules land in parallel (sites, mail, DNS, clients, databases, cron, ftp/shell). Admins need every **list table** and **form** to behave consistently: filters, empty states, validation, permissions, i18n, theme tokens (`--radius: 0`, no CDN), and parity with ISPConfig3 interaction patterns where intentional.

This change is a **cross-module QA + polish pass** — not a new product module. It produces a checklist, automated agent-browser coverage, and targeted code fixes.

## What Changes

- **Inventory**: catalog all Vue list/form screens under `frontend/src/views/**` mapped to API entities and OpenSpec modules.
- **Checklist & goldens**: per-screen acceptance criteria (filters work, create/edit/delete, 403 isolation, keyboard focus, error inline, loading/empty).
- **agent-browser suites**: expand/create `e2e/panel-*.sh` (or a unified `e2e/panel-ui-qa.sh`) covering each module against the built binary and lab `.10`.
- **Code fixes**: Grok implements UX/bug fixes found; Claude **Fable 5** validates UI quality (visual consistency, forms/tables) and may refine.
- **Screenshots**: `docs/prints/ui-qa-*` (not committed) attached on Telegram status; curated shots may move to `docs/screenshots/` after approval.
- **No schema changes**. Minimal API changes only when list filters or form metadata are broken.

## Capabilities

### New
- `ui-qa-checklist`: living checklist of screens and pass/fail status.
- `ui-qa-e2e`: agent-browser coverage for forms and tables.
- `ui-qa-polish`: coordinated fix pass (Grok code / Claude Fable 5 UI review).

### Modified
- Existing panel views/components as bugs are found (sites, mail, dns, clients, system, cron, ftp/shell when merged).

## Impact

- Touches `frontend/` primarily; possible small API filter fixes.
- Depends on modules already on main + in-flight cron/ftp branches when merged.
- Lab redeploy to `192.168.56.10` required after UI fix batches.

## Non-goals

- New ISPConfig modules (PowerDNS, monitor stay separate).
- Full visual redesign / new design system.
- Translations beyond English.
- Mobile-first redesign.

## Agents

| Role | Who |
|---|---|
| Implement fixes | Grok |
| UI quality review / refine | Claude **Fable 5** (`claude-fable-5`) |
| Orchestration / kanban / status | Hermes |
