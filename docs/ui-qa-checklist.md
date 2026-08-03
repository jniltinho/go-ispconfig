# UI QA Acceptance Checklist

Living pass/fail checklist for every list and form screen catalogued in
`docs/ui-qa-inventory.md`. One conventional commit per task batch; update
status here as screens are exercised (agent-browser + manual on lab `.10`).

**Change:** `openspec/changes/ui-forms-tables-qa`
**Branch:** `feat/ui-forms-tables-qa`
**Theme tokens:** `--radius: 0`, local fonts only (zero CDN). See
`docs/research/ispconfig3-theme.md` and `frontend/src/style.css` / `theme.ts`.

## How to use

1. Pick a screen from the inventory.
2. Run the criteria below (list vs form set).
3. Mark **Pass** / **Fail** / **N/A** and note bugs in the **Findings** column.
4. Failures become code fixes on this branch (tasks 2.x / 3.x / 4.x).
5. Screenshots of failures and after-fix go under `docs/prints/ui-qa-*`
   (never committed). Attach to Telegram group via Hermes on each marco.

Legend: ✅ pass · ❌ fail · ⬜ not run · — N/A

---

## Shared criteria

### A. List screens (DataTable)

| ID | Criterion | How to verify |
|---|---|---|
| L1 | **Loads without error** | Open route; no red `UiAlert`; network 2xx for list GET |
| L2 | **Loading state** | Skeleton/aria-busy while fetch in flight (`data-test="skeleton-row"`) |
| L3 | **Empty state** | Zero rows shows empty icon + `table.empty` (+ filtered hint if filters active) |
| L4 | **Column filters work** | Type in filterable column, Enter or filter button → list refetches with query params; results match |
| L5 | **Decorated column filters** | Columns like `_server_name`, `_parent_domain`, `_database_user` filter by **display name**, not raw id |
| L6 | **Pagination** | Page size respected; next/prev/page change updates rows; total/labels correct |
| L7 | **Sort** | If sort UI present, order changes; else mark — (current DataTable has no client sort) |
| L8 | **Row actions** | Edit navigates to form; delete confirms and removes row (or shows error) |
| L9 | **Row click** | Opens edit when wired (`@row-click`) |
| L10 | **403 isolation** | Client/reseller session only sees own rows; admin-only routes redirect or 403 cleanly |
| L11 | **i18n** | Title, buttons, column labels, empty text resolve from `en.json` (no raw keys unless intentional) |
| L12 | **Theme** | `--radius: 0` on table/inputs/buttons; no external CDN font/script requests |
| L13 | **Keyboard** | Filter inputs focusable; Enter applies filters; action buttons reachable |

### B. Form screens (EntityForm / TabbedForm / dedicated)

| ID | Criterion | How to verify |
|---|---|---|
| F1 | **Loads metadata** | Create: fields from `GET /api/meta/forms/{entity}`; Edit: record filled |
| F2 | **Validation errors** | Invalid submit → 422; inline field errors via i18n keys (`ApiError.fields`) |
| F3 | **Required fields** | Empty required blocks save with clear feedback |
| F4 | **Readonly fields** | Declared readonly (e.g. `server_id` on edit) not editable |
| F5 | **Active checkbox** | `active` y/n (or equivalent) persists correctly |
| F6 | **Save create** | POST succeeds; navigates back to list; new row visible (or pending badge) |
| F7 | **Save edit** | PUT succeeds; list reflects changes |
| F8 | **Cancel / back** | Returns to list without mutating |
| F9 | **403 isolation** | Client cannot edit foreign entity; admin-only forms gated |
| F10 | **i18n** | Labels, help, errors from locale; no bare API keys in UI when key exists |
| F11 | **Theme** | Tabs, inputs, buttons use surface/border tokens; radius 0 |
| F12 | **Keyboard / focus** | Tab order sensible; submit via keyboard; focus not lost on error |

### C. Cross-cutting / theme

| ID | Criterion | How to verify |
|---|---|---|
| T1 | **No CDN assets** | Network log: fonts/scripts same-origin only (see `e2e/panel-theme.sh`) |
| T2 | **Radius 0** | Computed style `border-radius` is `0px` on key controls |
| T3 | **Light/dark** | Theme toggle (if present) keeps contrast; forms readable in both |
| T4 | **Sidebar nav** | Every sidebar section for enabled modules opens without blank error page |
| T5 | **CSRF** | Mutations send `X-CSRF-Token`; stale token rehydrates once |

---

## Screen status matrix

Update after each QA batch. **Baseline** (task 1.3) only checks T4 + login for sidebar modules.

### Auth / dashboard

| Screen | Route | List | Form | Status | Findings |
|---|---|---|---|---|---|
| Login | `/login` | — | F* | ⬜ | |
| Dashboard | `/dashboard` | — | — | ⬜ | T4 only for baseline |

### Sites

| Screen | Route | List | Form | Status | Findings |
|---|---|---|---|---|---|
| Web domains list | `/sites` | L* | — | ⬜ | |
| Web domain form | `/sites/domains/new\|:id` | — | F* | ⬜ | |
| Folders list | `/sites/folders` | L* | — | ⬜ | |
| Folder form | `/sites/folders/new\|:id` | — | F* | ⬜ | |
| Folder users list | `/sites/folders/:fid/users` | L* | — | ⬜ | |
| Folder user form | `…/users/new\|:id` | — | F* | ⬜ | |
| Databases list | `/sites/databases` | L* (L5 critical) | — | ⬜ | |
| Database form | `/sites/databases/new\|:id` | — | F* | ⬜ | |
| DB users list | `/sites/database-users` | L* | — | ⬜ | |
| DB user form | `/sites/database-users/new\|:id` | — | F* | ⬜ | |
| Crons | `/sites/crons` | — | — | — | N/A until cron merged |
| FTP users | `/sites/ftp-users` | — | — | — | N/A until ftp-shell merged |
| Shell users | `/sites/shell-users` | — | — | — | N/A until ftp-shell merged |

### DNS

| Screen | Route | List | Form | Status | Findings |
|---|---|---|---|---|---|
| Zones list | `/dns` | L* | — | ⬜ | |
| Zone wizard | `/dns/wizard` | — | F* | ⬜ | |
| Zone form + records | `/dns/zones/:id` | — | F* | ⬜ | |
| Slave zones | `/dns/slave-zones` + form | L* / F* | ⬜ | |
| Templates | `/dns/templates` + form | L* / F* | ⬜ | adminOnly |
| PowerDNS | — | — | — | — | N/A |

### Mail

| Screen | Route | Status | Findings |
|---|---|---|---|
| Domains list + DomainForm | `/mail`, `/mail/domains/*` | ⬜ | |
| Mailboxes list + form | `/mail/mailboxes` | ⬜ | |
| Aliases | `/mail/aliases` | ⬜ | |
| Forwards | `/mail/forwards` | ⬜ | |
| Catchalls | `/mail/catchalls` | ⬜ | |
| Alias domains | `/mail/alias-domains` | ⬜ | |
| Transports | `/mail/transports` | ⬜ | |
| Spam policies | `/mail/spamfilter/policies` | ⬜ | adminOnly |
| Spam users | `/mail/spamfilter/users` | ⬜ | |
| WB lists | `/mail/spamfilter/wblists` | ⬜ | |
| Access | `/mail/access` | ⬜ | |

### Clients

| Screen | Route | Status | Findings |
|---|---|---|---|
| Clients list + form | `/clients` | ⬜ | L10 isolation critical |
| Resellers list + form | `/clients/resellers` | ⬜ | adminOnly |
| Limit templates | `/clients/limit-templates` | ⬜ | adminOnly |
| Message templates | `/clients/message-templates` | ⬜ | |
| Send message | `/clients/send-message` | ⬜ | |
| Delete dialog | (from list) | ⬜ | resource counts / delete everything |

### System

| Screen | Route | Status | Findings |
|---|---|---|---|
| Placeholder | `/system` | ⬜ | placeholder OK |
| Firewall list + form | `/system/firewall` | ⬜ | adminOnly |
| Migration wizard | `/system/migration` | ⬜ | adminOnly |
| Monitor | — | — | N/A |

---

## Template for a new finding

```markdown
### BUG-XXX — short title
- **Screen / route:**
- **Criteria:** L4 / F2 / …
- **Repro:**
- **Expected:**
- **Actual:**
- **Fix PR/commit:**
- **Verified:** ✅ / ⬜
```

---

## Sign-off

| Gate | Owner | When | Status |
|---|---|---|---|
| Inventory | Grok | task 1.1 | ✅ |
| This checklist | Grok | task 1.2 | ✅ |
| Baseline smoke | Grok | task 1.3 | ⬜ |
| Module batches 2.x–3.x | Grok | code fixes | ⬜ |
| Cross-cutting 4.1–4.2 | Grok | DataTable/EntityForm | ⬜ |
| Theme polish 4.3 | Claude Fable 5 | `.hermes/review-fable-ui.md` | ⬜ |
| Lab `.10` full smoke | Grok | task 5.1 | ⬜ |
)
