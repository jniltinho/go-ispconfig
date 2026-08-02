# mail-module-events Specification

## Purpose
TBD - created by archiving change add-mail-module. Update Purpose after archive.
## Requirements

### Requirement: Mail table hooks raise named events
The daemon mail module SHALL register table hooks for `mail_domain`, `mail_user`, `mail_forwarding`, `mail_transport`, `mail_access`, `spamfilter_users` and `spamfilter_wblist`, and announce/raise the events `mail_domain_insert|update|delete`, `mail_user_insert|update|delete`, `mail_forwarding_insert|update|delete`, `mail_transport_insert|update|delete`, `mail_access_insert|update|delete`, `spamfilter_users_insert|update|delete`, `spamfilter_wblist_insert|update|delete`, mapping datalog actions `i`/`u`/`d` respectively (port of `mail_module.inc.php::process` for the in-scope tables).

#### Scenario: Domain update datalog row dispatches mail_domain_update
- **WHEN** the daemon processes a `sys_datalog` row with `dbtable=mail_domain` and `action=u`
- **THEN** the `mail_domain_update` event is raised with the `{old,new}` payload

#### Scenario: Mailbox insert dispatches mail_user_insert
- **WHEN** the daemon processes a `sys_datalog` row with `dbtable=mail_user` and `action=i`
- **THEN** the `mail_user_insert` event is raised with the `{old,new}` payload

#### Scenario: Out-of-scope tables are not hooked
- **WHEN** the daemon processes a `sys_datalog` row for `mail_get`, `mail_mailinglist` or `mail_content_filter`
- **THEN** the mail module does not raise an event for that row (tables are not registered)

#### Scenario: Unregistered plugin cannot subscribe to unannounced event
- **WHEN** a plugin attempts to register a handler for an event the mail module did not announce
- **THEN** registration is rejected (foundation registry contract)

### Requirement: Postfix, Dovecot and Rspamd service registration
The mail module SHALL register services `postfix`, `dovecot` and `rspamd` in the services registry supporting `restart` and `reload` actions, resolving the matching systemd unit names at runtime. Amavis SHALL NOT be registered.

#### Scenario: Postfix reload is delayed and deduped
- **WHEN** multiple events in one datalog run request `reload` of `postfix`
- **THEN** exactly one `systemctl reload postfix` (or equivalent) is executed at the end of the run

#### Scenario: Restart wins over reload
- **WHEN** one event requests `reload` and a later event requests `restart` for `rspamd` in the same datalog run
- **THEN** exactly one `restart` is executed at the end of the run

### Requirement: Module enablement follows server role and config
The mail module SHALL only load when the daemon's server record has `mail_server = 1` and the module is enabled in `config.toml`.

#### Scenario: Non-mail server skips module
- **WHEN** the daemon starts on a server whose `server.mail_server` flag is 0
- **THEN** no mail table hooks are registered and mail datalog rows are not applied by this module on that host

### Requirement: Plugin subscription matrix
The mail plugins SHALL subscribe to events as follows (PHP parity):
- `mailPlugin`: `mail_user_insert|update|delete`, `mail_domain_delete`, `mail_transport_insert|update|delete`
- `maildeliverPlugin`: `mail_user_insert|update|delete`
- `dkimPlugin`: `mail_domain_insert|update|delete`
- `rspamdPlugin`: `spamfilter_wblist_*`, `mail_access_*`, `spamfilter_users_*`, `mail_user_*`, `mail_forwarding_*`, and server/server_ip events when available

#### Scenario: Transport update reaches mailPlugin only for postfix reload
- **WHEN** `mail_transport_update` is raised
- **THEN** `mailPlugin` handles it (postfix reload) and maildeliver/dkim plugins do not claim the event
