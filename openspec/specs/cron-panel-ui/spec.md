# cron-panel-ui

## ADDED Requirements

### Requirement: Sites → Cron navigation and job list
The panel SHALL add a Cron section under the Sites module (visible per user permissions) with a job list backed by `/api/sites/crons`: columns for active status, parent domain, schedule summary (composed from `run_min`/`run_hour`/`run_mday`/`run_month`/`run_wday`), type, truncated command, and log flag; search/filter; and an "Add cron job" entry point. All strings SHALL go through the i18n layer (en first).

#### Scenario: Job list shows only accessible crons
- **WHEN** a client opens Sites → Cron
- **THEN** only cron rows readable under the riud scope are listed

#### Scenario: Schedule summary is human-readable
- **WHEN** a job has `run_min='0'`, `run_hour='2'`, `run_mday='*'`, `run_month='*'`, `run_wday='*'`
- **THEN** the list shows a clear schedule summary derived from those five fields (not a raw opaque blob)

### Requirement: Cron job form
The form SHALL mirror `cron.tform.php` / `cron_edit.php`: parent domain select (vhosts only; disabled when editing), the five schedule fields, command (with help text for placeholders `{DOMAIN}`, `{DOCROOT_CLIENT}`, `{SITE_PHP}`), type (reflecting API auto-derivation), log toggle, active toggle. `server_id` is shown read-only from the parent site when known. Client-side validation SHALL mirror the API rules and API field errors SHALL display inline.

#### Scenario: Create form saves a URL job
- **WHEN** the user selects a parent site, enters a valid five-field schedule, a `https://…` command, enables log and active, and saves
- **THEN** the job appears in the list as type url and a success navigation/refresh occurs

#### Scenario: Parent domain locked on edit
- **WHEN** the user opens an existing cron job
- **THEN** the parent domain control is disabled and cannot be changed

#### Scenario: Validation error shown inline
- **WHEN** the user submits an invalid `run_min` value
- **THEN** the form shows the field error and no request-level state is lost

### Requirement: Run history view
The panel SHALL provide a run-history view (section on the edit form or a dedicated sub-view) listing recent executions for that job from the runs API: start time, duration or end time, status, exit code, and output tail.

#### Scenario: History shows logged runs
- **WHEN** the user opens history for a job that has `log='y'` and past executions
- **THEN** the recent runs are listed with status and output tail

#### Scenario: Empty history for never-run job
- **WHEN** the user opens history for a newly created job with no executions
- **THEN** an empty state is shown without error

### Requirement: E2E coverage of the Cron UI
agent-browser E2E tests SHALL cover: creating a URL cron job, editing schedule/active/log, viewing run history (seeded `sys_log` rows acceptable), deleting a job, and verifying a client only sees their own jobs. Screenshots land in `docs/prints/` for human review.

#### Scenario: E2E suite passes against a seeded panel
- **WHEN** the Cron E2E suite runs against a built binary with seeded data
- **THEN** all listed flows complete without errors
