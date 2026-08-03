# cron-rest-api

## ADDED Requirements

### Requirement: Cron CRUD endpoints porting sites_cron_*
The REST API SHALL expose cron operations porting `remote.d/sites.inc.php` (`sites_cron_get` / `sites_cron_add` / `sites_cron_update` / `sites_cron_delete`) under `/api/sites/crons`, session/token authenticated, riud-scoped, writing `sys_datalog` rows with `dbtable='cron'` and `{old,new}` payloads. The interface SHALL never apply OS changes itself.

#### Scenario: Create cron job
- **WHEN** `POST /api/sites/crons` is called with a valid parent domain, schedule fields, command and active flag by an authorized user
- **THEN** a `cron` row is created with ownership and `server_id` taken from the parent `web_domain`, a datalog insert row is written for that server, and the new id is returned

#### Scenario: Get and list cron jobs
- **WHEN** `GET /api/sites/crons` or `GET /api/sites/crons/:id` is called
- **THEN** only rows readable under the caller's riud scope are returned

#### Scenario: Update cron job
- **WHEN** `PUT /api/sites/crons/:id` is called with valid fields by a user with update permission
- **THEN** the row is updated, a datalog update row with `{old,new}` is written, and `parent_domain_id` cannot change

#### Scenario: Delete cron job
- **WHEN** `DELETE /api/sites/crons/:id` is called by a user with delete permission
- **THEN** the row is removed and a datalog delete row is written

#### Scenario: Foreign client cannot read another client's cron
- **WHEN** a client requests a cron row owned by another client
- **THEN** the API returns 404 or 403 and no data is leaked

### Requirement: Field validation matching cron.tform.php and validate_cron
Create/update SHALL enforce the ISPConfig rules on the exact `cron` columns: `parent_domain_id` required and referencing an accessible `web_domain` with `type='vhost'`; `run_min` / `run_hour` / `run_mday` / `run_wday` matching `validate_cron::run_time_format` (charset `0-9,-/*`, ranges min 0–59, hour 0–23, mday 1–31, wday 0–7, step/range syntax); `run_month` allowing the same or the literal `@reboot`; `command` NOTEMPTY and `command_format` (no CR/LF/NUL; when a URL scheme is present, only `http`/`https`, hostname-shaped host after `{DOMAIN}` expansion, no backslash); `type` in `url|chrooted|full`; `log` and `active` in `n|y`.

#### Scenario: Invalid minute field rejected
- **WHEN** a create request sends `run_min='60'`
- **THEN** the API returns a validation error naming the schedule rule and no row or datalog entry is written

#### Scenario: @reboot accepted only in run_month
- **WHEN** a create request sends `run_month='@reboot'` with otherwise valid fields
- **THEN** the row is accepted; the same token in `run_min` is rejected

#### Scenario: URL command with bad host rejected
- **WHEN** a command `https://not a host/path` is submitted
- **THEN** validation fails with the command format error

### Requirement: Type auto-derivation from command and client limits
On create/update the API SHALL derive `type` as in `cron_edit.php::onSubmit`: if `command` matches `^https?://` (case-insensitive) set `type='url'`; otherwise use the parent site's owner `client.limit_cron_type` (`full` → `full`, else `chrooted`); sites without a client owner default to `full`. The derived type is what is stored and what limit checks apply to.

#### Scenario: HTTP command forces type url
- **WHEN** a client submits `command='https://example.com/job'` with any type value
- **THEN** the stored row has `type='url'`

#### Scenario: Non-URL command under limit_cron_type=chrooted
- **WHEN** the parent site's client has `limit_cron_type='chrooted'` and the command is not a URL
- **THEN** the stored row has `type='chrooted'`

### Requirement: Parent site ownership and immutability
On create, `server_id` and `sys_groupid` SHALL be copied from the parent `web_domain` (port of `onSubmit` / `onAfterInsert`). On update, `parent_domain_id` SHALL be immutable. The caller MUST be allowed to read the parent site.

#### Scenario: server_id inherited from parent site
- **WHEN** a cron is created for a site on server 2
- **THEN** the stored `cron.server_id` is 2 regardless of any client-supplied value

#### Scenario: parent_domain_id cannot change on update
- **WHEN** an update tries to move a cron to another `parent_domain_id`
- **THEN** the field is rejected or ignored and the stored parent remains unchanged

#### Scenario: Inaccessible parent domain rejected
- **WHEN** a client submits a `parent_domain_id` for a site they cannot read
- **THEN** the API returns a permission/validation error and no row is written

### Requirement: Client limit enforcement
For non-admin callers the API SHALL enforce `client.limit_cron`, `limit_cron_type` and `limit_cron_frequency` (port of `cron_edit.php` limit checks):

- `limit_cron >= 0`: create rejected when the client's current cron count is already at the limit (`-1` means unlimited).
- `limit_cron_type='url'`: non-url types rejected; `chrooted` rejects `full`.
- `limit_cron_frequency > 1`: rejected when the schedule's minimum frequency in minutes (port of `validate_cron` `cron_min_freq`) is strictly less than the limit.

Admins are not subject to these limits.

#### Scenario: Count limit blocks create
- **WHEN** a client with `limit_cron=1` already owns one cron and posts a second
- **THEN** the API returns a limit error and no row is written

#### Scenario: Frequency limit rejects every-minute schedule
- **WHEN** a client with `limit_cron_frequency=5` submits `run_min='*'`
- **THEN** the API returns a frequency limit error

#### Scenario: url-only client cannot create full jobs
- **WHEN** a client with `limit_cron_type='url'` submits a non-URL command that would derive to `full` or `chrooted`
- **THEN** the API returns a type limit error

#### Scenario: Admin bypasses limits
- **WHEN** an admin creates a cron for a client already at `limit_cron`
- **THEN** the create succeeds

### Requirement: Run history endpoint
The API SHALL expose a paginated run-history endpoint for a cron id that returns `sys_log` rows whose message matches the `cron_run id=<id>` convention, only when the caller can read that cron row.

#### Scenario: Owner lists run history
- **WHEN** an authorized user calls the runs endpoint for a cron they can read
- **THEN** matching `sys_log` rows are returned newest-first with status, timestamps and output tail

#### Scenario: Unauthorized history access denied
- **WHEN** a user without read access to the cron requests its runs
- **THEN** the API returns 403 or 404

### Requirement: Swagger documentation for all cron endpoints
Every cron endpoint SHALL carry swaggo annotations (summary, params, request/response models, security, error codes) and appear in the embedded Swagger UI; CI SHALL fail when generated swagger output is stale.

#### Scenario: Cron endpoints visible in Swagger UI
- **WHEN** `/swagger/` is opened
- **THEN** the cron CRUD and run-history endpoints are listed with typed request/response schemas
