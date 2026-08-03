# getmail-config

## ADDED Requirements

### Requirement: mail_get table hook
The daemon mail module SHALL register a table hook for `mail_get` and announce/raise the events `mail_get_insert`, `mail_get_update` and `mail_get_delete`, mapping datalog actions `i`/`u`/`d` respectively (port of `server/mods-available/mail_module.inc.php`, which registers `mail_get` in `actions_available` and `registerTableHook('mail_get', …)`). This supersedes the `mail-module-events` scenario that declared `mail_get` unhooked; `mail_mailinglist` and `mail_content_filter` SHALL remain unhooked.

#### Scenario: Fetchmail insert dispatches mail_get_insert
- **WHEN** the daemon processes a `sys_datalog` row with `dbtable=mail_get` and `action=i`
- **THEN** the `mail_get_insert` event is raised with the `{old,new}` payload

#### Scenario: Fetchmail delete dispatches mail_get_delete
- **WHEN** the daemon processes a `sys_datalog` row with `dbtable=mail_get` and `action=d`
- **THEN** the `mail_get_delete` event is raised with the `{old,new}` payload

#### Scenario: Mailing list table stays unhooked
- **WHEN** the daemon processes a `sys_datalog` row for `mail_mailinglist` or `mail_content_filter`
- **THEN** the mail module does not raise an event for that row

### Requirement: Per-account rc file rendering
On `mail_get_insert|update` with `active=y`, the getmail plugin SHALL render the embedded `getmail.conf.master` template into one rc file per row, substituting `{DELETE}`, `{READ_ALL}`, `{TYPE}`, `{SERVER}`, `{USERNAME}`, `{PASSWORD}` and `{DESTINATION}` exactly as `getmail_plugin.inc.php::update` does: `source_delete` and `source_read_all` map `y` to `true` and anything else to `false`; `type` maps `pop3`→`SimplePOP3Retriever`, `imap`→`SimpleIMAPRetriever`, `pop3ssl`→`SimplePOP3SSLRetriever`, `imapssl`→`SimpleIMAPSSLRetriever`; `source_server`, `source_username`, `source_password` and `destination` are inserted verbatim. The `[destination]` block SHALL stay `MDA_external` to the sendmail binary with arguments `("-i", "-bm", "<destination>")`, so fetched mail re-enters the local MTA and is delivered to the `mail_user` mailbox by the existing mail stack.

#### Scenario: Active POP3 account renders its rc file
- **WHEN** `mail_get_insert` arrives with `active=y`, `type=pop3`, `source_delete=y`, `source_read_all=n`
- **THEN** an rc file exists containing `type = SimplePOP3Retriever`, `delete = true`, `read_all = false`, the source server/username/password and an `MDA_external` destination naming the row's `destination` address

#### Scenario: IMAP over SSL selects the SSL retriever
- **WHEN** a row with `type=imapssl` is rendered
- **THEN** the rc file contains `type = SimpleIMAPSSLRetriever`

#### Scenario: Unknown retriever type is rejected
- **WHEN** a row carries a `type` outside `pop3|imap|pop3ssl|imapssl`
- **THEN** no rc file is written and the plugin logs an error (an unsubstituted `{TYPE}` would break every account in the batch run)

#### Scenario: Custom template overrides the embedded one
- **WHEN** `getmail.conf.master` exists in the configured custom template directory
- **THEN** it is used instead of the embedded template (`mastertpl` custom-dir contract, PHP `conf-custom/getmail.conf.master`)

### Requirement: rc file naming and path guards
The rc file path SHALL be `<getmail_config_dir>/<clean(source_server)>_<clean(source_username)>.conf`, where `clean` replaces every character outside `[A-Za-z0-9\-_]` with `_` (byte-identical to `getmail_plugin.inc.php::_clean_path`, so files written by a previous PHP installation are re-used). The plugin SHALL refuse to write or delete a path containing `..`, `|`, `;` or `$` (PHP parity), SHALL additionally verify that the cleaned absolute path's parent is `getmail_config_dir`, and SHALL NOT create `getmail_config_dir` — a missing or non-directory config dir is logged as an error and the event is a no-op.

#### Scenario: Special characters in the account are sanitised
- **WHEN** a row has `source_server=mail.example.com` and `source_username=john.doe@example.com`
- **THEN** the rc file is named `mail_example_com_john_doe_example_com.conf`

#### Scenario: Traversal attempt is refused
- **WHEN** the assembled path contains `..`, `|`, `;` or `$`
- **THEN** no file is written or removed and the plugin logs an error

#### Scenario: Missing config directory is a no-op
- **WHEN** `getmail_config_dir` does not exist or is not a directory
- **THEN** the plugin logs an error, writes nothing and does not create the directory

### Requirement: rc file ownership and permissions
Every rc file SHALL be written with mode `0600` and owned by the configured getmail user and its group, applying ownership before the final mode so the file is never simultaneously root-owned and group-readable. Ownership changes SHALL go through the daemon command runner so every invocation is logged and fakeable in tests. The rc file contains a cleartext third-party password: neither the rendered body nor the password SHALL ever be logged.

#### Scenario: Rendered rc is private to the getmail user
- **WHEN** an rc file is written
- **THEN** its mode is `0600` and it is owned by the configured getmail user and group

#### Scenario: Password never reaches the log
- **WHEN** an rc file is written at debug log level
- **THEN** the log records the file path but neither the rendered template body nor `source_password`

### Requirement: Deactivation, rename and delete remove the rc file
The plugin SHALL delete the rc file derived from the `{old}` payload before writing the `{new}` one, SHALL delete without writing when `active` is not `y`, and SHALL delete on `mail_get_delete`. A missing file on delete is not an error.

#### Scenario: Deactivating an account removes its rc file
- **WHEN** `mail_get_update` sets `active=n`
- **THEN** the rc file for that account is removed and no new file is written

#### Scenario: Renaming the source account leaves no orphan
- **WHEN** `mail_get_update` changes `source_username` from `old@example.com` to `new@example.com`
- **THEN** the rc file named from the old values is removed and only the file named from the new values exists

#### Scenario: Deleting the row removes the rc file
- **WHEN** `mail_get_delete` arrives for an account whose rc file exists
- **THEN** the file is removed

#### Scenario: Delete of an already-absent file succeeds
- **WHEN** `mail_get_delete` arrives and the rc file does not exist
- **THEN** the event completes without error

### Requirement: Mirror servers never write rc files
When the server's `mirror_server_id` is greater than zero, the getmail plugin SHALL return immediately on every `mail_get` event without writing or deleting anything (port of the `getmail_plugin.inc.php::update` mirror guard: "Do not write getmail config files on mirror servers to avoid double fetching of emails").

#### Scenario: Mirror server ignores a fetchmail event
- **WHEN** `mail_get_insert` is processed on a server whose `mirror_server_id` is greater than zero
- **THEN** no rc file is created and the event completes without error

### Requirement: Getmail getconf section
`internal/getconf` SHALL expose a typed `[getmail]` server-config section with `getmail_config_dir` (default `/etc/getmail`, the only key ISPConfig exposes — `server_config.tform.php` Getmail tab), `getmail_program` (default `/usr/bin/getmail`) and `getmail_user` (default `getmail`). Absent keys SHALL fall back to those defaults, and a blank `getmail_config_dir` SHALL NOT disarm the containment guard.

#### Scenario: Absent section yields defaults
- **WHEN** a server's config has no `[getmail]` section
- **THEN** the plugin uses `/etc/getmail`, `/usr/bin/getmail` and the `getmail` user

#### Scenario: Blank config dir disables the plugin rather than widening guards
- **WHEN** `getmail_config_dir` is empty or not an absolute path
- **THEN** the plugin logs an error and performs no filesystem operation

### Requirement: Installer provisions getmail
On mail servers the installer SHALL install the getmail package, create the `getmail` system user with its home set to `getmail_config_dir` when the user is absent, and ensure that directory exists with mode `0700` owned by `getmail:getmail` (port of `installer_base.lib.php::configure_getmail`). The installer SHALL NOT install `run-getmail.sh` and SHALL NOT create or modify any crontab (foundation rule: the daemon scheduler owns all periodic work).

#### Scenario: Fresh mail server gets a usable getmail setup
- **WHEN** the installer runs on a server with the mail role
- **THEN** the getmail package is installed, the `getmail` user exists with home `/etc/getmail`, and `/etc/getmail` is `0700` owned by `getmail:getmail`

#### Scenario: No crontab is written
- **WHEN** the installer completes
- **THEN** no crontab entry exists for the `getmail` user and `/usr/local/bin/run-getmail.sh` is not created
