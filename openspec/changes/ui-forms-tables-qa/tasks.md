# Tasks: ui-forms-tables-qa

## 1. Inventory and checklist

- [x] 1.1 Inventory all list/form routes in `frontend/src` + router; map to API paths and module. Commit as `docs/ui-qa-inventory.md`.
- [x] 1.2 Write acceptance checklist template (filters, sort, empty, validation, 403, i18n, theme radius 0, no CDN assets). Commit.
- [x] 1.3 Baseline agent-browser smoke against lab `.10` (login + each sidebar module opens). Screenshots `docs/prints/ui-qa-baseline-*`. Commit script only.

## 2. Sites / databases / cron / ftp-shell

- [x] 2.1 Sites web domains list+form QA + fixes. Commit.
- [x] 2.2 Databases + database users list filters/forms QA + fixes. Commit.
- [x] 2.3 Cron list/form/history QA + fixes (after cron on main). Commit.
- [x] 2.4 FTP users + shell users list/form QA + fixes (after ftp on main). Commit.

## 3. Mail / DNS / clients / system

- [x] 3.1 Mail module lists/forms QA + fixes. Commit.
- [x] 3.2 DNS zone lists/forms QA + fixes. Commit.
- [x] 3.3 Clients/resellers/templates lists/forms QA + fixes. Commit.
- [x] 3.4 System (firewall, server settings if any) lists/forms QA + fixes. Commit.

## 4. Cross-cutting polish (Claude Fable 5 review gate)

- [x] 4.1 DataTable consistency (filter inputs on decorated columns, pagination labels, empty states).
- [x] 4.2 EntityForm/TabbedForm consistency (readonly fields, checkbox active, error keys i18n).
- [x] 4.3 Theme pass: radius 0, spacing, contrast, light/dark if applicable; Claude Fable 5 review write-up `.hermes/review-fable-ui.md`.
- [x] 4.4 Unified `make e2e-ui-qa` target; Telegram status with MEDIA screenshots of failures before/after.

## 5. Close-out

- [x] 5.1 Redeploy binary to lab `.10`; full smoke. Commit docs only if needed.
- [x] 5.2 Archive OpenSpec change when checklist all green; PR to main.
