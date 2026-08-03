# getmail-ui

## ADDED Requirements

### Requirement: Fetchmail REST resource
The API SHALL expose `/api/mail/fetchmail` as a CRUD entity over the existing `mail_get` table (PK `mailget_id`), mounted with the other mail resources, with riud scoping, `{old,new}` datalog rows on the row's `server_id` and swagger annotations — the REST port of `remote.d/mail.inc.php` `mail_fetchmail_get`, `mail_fetchmail_add`, `mail_fetchmail_update` and `mail_fetchmail_delete`. No schema change SHALL be made to `mail_get`.

#### Scenario: Creating an account writes a datalog row
- **WHEN** `POST /api/mail/fetchmail` succeeds
- **THEN** the `mail_get` row is inserted and a `sys_datalog` row with `dbtable=mail_get`, `action=i` and the `{old,new}` payload is written for the derived `server_id`

#### Scenario: Listing is scoped to the caller
- **WHEN** a client lists `/api/mail/fetchmail`
- **THEN** only rows readable under that client's riud permissions are returned

### Requirement: Field validation
The fetchmail entity SHALL port the validators of `form/mail_get.tform.php`: `type` restricted to `pop3|imap|pop3ssl|imapssl`; `source_server` non-empty, IDN-encoded, lowercased and matching a hostname-or-IPv4 pattern; `source_username` non-empty with tags and newlines stripped; `source_password` non-empty and at most 64 characters (the actual `varchar(64)` column width, which the PHP form overstates as 255 and silently truncates); `destination` a valid email address; `source_delete`, `source_read_all` and `active` restricted to `y`/`n`.

#### Scenario: Malformed source server is rejected
- **WHEN** `source_server` is not a hostname or IPv4 address
- **THEN** the API returns a validation error and nothing is persisted

#### Scenario: Oversized password is rejected, not truncated
- **WHEN** `source_password` exceeds 64 characters
- **THEN** the API returns a validation error instead of storing a silently truncated password

#### Scenario: Username is stripped of markup
- **WHEN** `source_username` contains HTML tags or newlines
- **THEN** they are removed before the value is stored

### Requirement: Destination ownership and derived fields
On create and update the API SHALL verify that `destination` matches a `mail_user` row readable by the caller, rejecting otherwise (`no_destination_perm` parity), SHALL derive `server_id` from that mailbox and ignore any client-supplied `server_id`, and SHALL set the row's `sys_groupid` from that mailbox's `sys_groupid` (port of `mail_get_edit.php::onSubmit` and `onAfterInsert`).

#### Scenario: Foreign destination is refused
- **WHEN** a client submits a `destination` belonging to another client
- **THEN** the API returns a validation error and no row is written

#### Scenario: Server is derived from the destination mailbox
- **WHEN** a fetchmail account is created for a mailbox hosted on server 3 while the request body claims `server_id=1`
- **THEN** the stored row and its datalog entry use `server_id=3`

#### Scenario: Ownership follows the destination mailbox
- **WHEN** a fetchmail account is created
- **THEN** its `sys_groupid` equals the `sys_groupid` of the destination `mail_user` row

### Requirement: Illegal delete/read_all combination refused
The API SHALL reject a payload where `source_delete` is not `y` while `source_read_all` is `y` (port of the `error_delete_read_all_combination` check in `mail_get_edit.php::onSubmit`), because that combination re-downloads the whole remote mailbox on every run.

#### Scenario: Keep-on-server plus read-all is rejected
- **WHEN** a request sets `source_delete=n` and `source_read_all=y`
- **THEN** the API returns a validation error and nothing is persisted

### Requirement: limit_fetchmail enforcement
Creating a fetchmail account SHALL be refused for a non-admin caller whose client (or reseller) has reached its `limit_fetchmail` allowance, counted as the number of `mail_get` rows in the caller's group — the counter already wired into the client-limits helper (port of the `checkClientLimit`/`checkResellerLimit` and the explicit count in `mail_get_edit.php`).

#### Scenario: Client at its limit cannot add another account
- **WHEN** a client with `limit_fetchmail=2` and two existing accounts posts a third
- **THEN** the API returns a limit error and no row is written

#### Scenario: Unlimited client is never blocked
- **WHEN** a client's `limit_fetchmail` is `-1`
- **THEN** account creation is not limited

#### Scenario: Admin bypasses the limit
- **WHEN** an admin creates a fetchmail account for any client
- **THEN** the limit check does not apply

### Requirement: Source password is write-only
`source_password` SHALL be accepted on create and update but SHALL NOT be returned by list or detail endpoints, and SHALL NOT appear in API logs or error messages.

#### Scenario: List omits the password
- **WHEN** `GET /api/mail/fetchmail` is called
- **THEN** no item contains a `source_password` value

#### Scenario: Update without a password keeps the stored one
- **WHEN** an update omits `source_password`
- **THEN** the previously stored password is preserved

### Requirement: Fetch Email panel views
The panel SHALL provide a "Fetch Email" list and form inside the Email module. The list SHALL show active, type, source server, source username and destination with the shared filtering/paging table and datalog state badges (the columns of `list/mail_get.list.php`). The form SHALL expose type as a four-option select, source server, source username, a write-only password field, destination as a select over mailboxes visible to the caller, and the `source_delete`, `source_read_all` and `active` checkboxes; `server_id` SHALL NOT be shown because it is derived.

#### Scenario: Account appears in the list after creation
- **WHEN** a user saves a new fetch account
- **THEN** it appears in the Fetch Email list with its destination and a pending datalog badge until the daemon applies it

#### Scenario: Editing an account never shows the stored password
- **WHEN** an existing account is opened for editing
- **THEN** the password field is empty and leaving it empty preserves the stored password

#### Scenario: Destination choices are limited to the caller's mailboxes
- **WHEN** the destination select is opened
- **THEN** only mailboxes readable by the caller are offered

### Requirement: Getmail server-config tab
The server-config view SHALL provide a Getmail tab exposing `getmail_config_dir`, validated as a non-empty absolute path matching `^/[a-zA-Z0-9._\-/]{5,128}$` (port of the `getmail` tab in `admin/form/server_config.tform.php`).

#### Scenario: Invalid config directory rejected
- **WHEN** an admin saves a relative or too-short `getmail_config_dir`
- **THEN** a validation error is returned and the server config is unchanged
