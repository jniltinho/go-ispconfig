# rspamd-config

## ADDED Requirements

### Requirement: Baseline Rspamd snippet deployment
On the same server / server_ip events that already refresh the templated Rspamd snippets, and only when the Rspamd config root exists, the rspamd plugin SHALL also deploy the baseline configuration that `install/lib/installer_base.lib.php::configure_rspamd()` deploys in ISPConfig: `local.d/users.conf`, `local.d/{groups,antivirus,mx_check,milter_headers,neural,neural_group,arc}.conf`, `override.d/{rbl_group,surbl_group}.conf` and `local.d/maps.d/{dkim_whitelist,dmarc_whitelist,spf_dkim_whitelist,spf_whitelist}.inc.ispc`, creating `local.d/`, `local.d/maps.d/` and `override.d/` with mode `0755` when missing. `local.d/users.conf` — whose `.include(try=true; glob=true) "$LOCAL_CONFDIR/local.d/users/*.conf"` is what loads the per-identity settings files — SHALL be daemon-owned and rewritten on every run. All other baseline files SHALL be written only when absent, so operator tuning survives. Assets SHALL come from the embedded `.master` set with the existing custom-template override path. Files SHALL be readable by Rspamd (`0644`), and after deployment a delayed `rspamd` reload SHALL be requested.

#### Scenario: Fresh server gets the full baseline
- **WHEN** a `server_update` event is processed on a mail server whose Rspamd config root exists but contains no baseline files
- **THEN** `local.d/users.conf`, every baseline `local.d` and `override.d` snippet and every `maps.d` whitelist file exist, and a delayed `rspamd` reload is queued

#### Scenario: Operator-tuned baseline file is not clobbered
- **WHEN** an operator has edited `override.d/rbl_group.conf` and a later `server_update` event is processed
- **THEN** the edited content is preserved unchanged

#### Scenario: Edited users.conf is restored
- **WHEN** `local.d/users.conf` has been modified or deleted and a `server_update` event is processed
- **THEN** the file is rewritten from the template so the per-identity settings glob is included again

#### Scenario: Rspamd absent is a no-op
- **WHEN** a server event is processed on a host where the Rspamd config root is not a directory
- **THEN** no file or directory is created and the handler returns without error

### Requirement: Obsolete greylist module config retired
When deploying the baseline, the plugin SHALL rename an existing `local.d/greylist.conf` to `local.d/greylist.old`, matching ISPConfig, because greylisting is driven per identity through the settings files and a stale module config would override it.

#### Scenario: Legacy greylist config renamed once
- **WHEN** `local.d/greylist.conf` exists and a server event is processed
- **THEN** it is renamed to `local.d/greylist.old` and no `greylist.conf` remains

#### Scenario: Rename does not repeat
- **WHEN** a server event is processed and only `local.d/greylist.old` exists
- **THEN** nothing is renamed and no error is raised

### Requirement: Controller worker configuration for rspamc
The plugin SHALL write `local.d/worker-controller.inc` from the `worker-controller` template on every server event, with `secure_ip` restricted to `127.0.0.1` and `::1`. A `password` entry SHALL be rendered only when the mail getconf `rspamd_password` value is non-empty, hashed with `rspamadm pw` when that binary is available and used verbatim otherwise. The file SHALL be group-owned by the Rspamd group and mode `0640`, and the password SHALL never be logged or returned by any API response.

#### Scenario: Controller reachable from localhost without a configured password
- **WHEN** `[mail] rspamd_password` is empty and a server event is processed
- **THEN** `worker-controller.inc` exists with localhost `secure_ip` entries and no `password` line

#### Scenario: Configured password is hashed
- **WHEN** `[mail] rspamd_password` is set and `rspamadm` is available
- **THEN** the rendered `password` value is the `rspamadm pw` output, not the plaintext value

#### Scenario: Credential file permissions
- **WHEN** `worker-controller.inc` has been written
- **THEN** its group is the Rspamd group and its mode is `0640`

### Requirement: Continuous Bayes training via Dovecot IMAPSieve
When Dovecot and its sieve plugin are present, the plugin SHALL deploy an IMAPSieve drop-in that trains the Bayes classifier on Junk moves: a Dovecot configuration file enabling `imap_sieve` with a rule for messages moved **into** the Junk mailbox and a rule for messages moved **out of** it, two sieve scripts piping the message to a learn wrapper, and two executable wrappers invoking `rspamc learn_spam` and `rspamc learn_ham` against the local controller with the configured password when set. The Dovecot drop-in SHALL be daemon-owned; the sieve scripts and wrappers SHALL be written only when absent. Configuration SHALL be validated before a delayed `dovecot` reload is requested, and the whole step SHALL be skipped without error when Dovecot or its sieve plugin is missing.

#### Scenario: Moving mail to Junk trains spam
- **WHEN** the IMAPSieve deployment has run and a user moves a message into the Junk mailbox
- **THEN** the report-spam script runs the `learn_spam` wrapper for that message

#### Scenario: Moving mail out of Junk trains ham
- **WHEN** a user moves a message out of the Junk mailbox into another folder
- **THEN** the report-ham script runs the `learn_ham` wrapper for that message

#### Scenario: Dovecot without sieve support is skipped
- **WHEN** a server event is processed on a host where the Dovecot sieve plugin is not installed
- **THEN** no Dovecot file is written, no reload is requested and the handler returns without error

#### Scenario: Invalid generated config never reaches a reload
- **WHEN** the deployed Dovecot drop-in fails configuration validation
- **THEN** no `dovecot` reload is requested and the failure is logged

### Requirement: On-demand learning remote action
The rspamd plugin SHALL register a remote action that trains the classifier from an existing mailbox folder, taking the kind (`spam` or `ham`), the mailbox address and an optional folder (defaulting to the Junk folder for spam and the inbox for ham). It SHALL resolve the maildir from the `mail_user` row for that address on this server, refuse addresses that do not resolve, invoke `rspamc` with the appropriate `learn_spam` / `learn_ham` subcommand over the messages in the folder in bounded batches, and report `ok` when all messages were accepted, `warning` when some were rejected or the batch cap was reached, and `error` when the controller could not be reached.

#### Scenario: Train an existing Junk folder
- **WHEN** a learn action with kind `spam` is dispatched for a mailbox whose Junk folder holds messages
- **THEN** `rspamc learn_spam` is invoked for those messages and the action state is `ok`

#### Scenario: Unknown mailbox is rejected
- **WHEN** a learn action names an address with no `mail_user` row on this server
- **THEN** no command is executed and the action state is `error`

#### Scenario: Controller unreachable
- **WHEN** `rspamc` cannot connect to the controller worker
- **THEN** the action state is `error` and the command output is logged

#### Scenario: Partially rejected batch
- **WHEN** some messages are refused because they were already learned
- **THEN** the action state is `warning` and processing of the remaining messages continues

### Requirement: Spamfilter resync remote action
The rspamd plugin SHALL register a remote action that rebuilds this server's Rspamd per-row configuration from the database: re-rendering the settings file of every `spamfilter_users`, `mail_user` and `mail_forwarding` identity and the conf of every `spamfilter_wblist` and `mail_access` row for this server, removing files in the users config directory that correspond to no current row, and requesting a delayed `rspamd` reload. Rendering SHALL reuse the same code paths as the event handlers so resync output cannot diverge from event output. This action is the supported way to propagate a `spamfilter_policy` change, which by design raises no per-row event.

#### Scenario: Policy change propagated by resync
- **WHEN** a `spamfilter_policy` row has been updated and a resync action is dispatched
- **THEN** every settings file of an identity bound to that policy is rewritten with the new thresholds and a reload is queued

#### Scenario: Resync equals event replay
- **WHEN** a resync runs against a given database state
- **THEN** the resulting set of settings and wblist files is identical to the set produced by replaying the corresponding insert events one by one

#### Scenario: Orphaned conf removed
- **WHEN** a conf file exists in the users config directory for a row that no longer exists
- **THEN** the resync deletes it
