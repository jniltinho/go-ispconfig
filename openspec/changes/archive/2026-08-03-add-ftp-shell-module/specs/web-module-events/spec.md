# web-module-events

## MODIFIED Requirements

### Requirement: Web module registers table hooks
The web module SHALL register table hooks for `web_domain`, `ftp_user`, `shell_user`, `web_folder` and `web_folder_user` in the module registry, and SHALL announce the events `web_domain_insert`, `web_domain_update`, `web_domain_delete`, `ftp_user_insert`, `ftp_user_update`, `ftp_user_delete`, `shell_user_insert`, `shell_user_update`, `shell_user_delete`, `web_folder_insert`, `web_folder_update`, `web_folder_delete`, `web_folder_user_insert`, `web_folder_user_update`, `web_folder_user_delete` so plugins can subscribe to them.

#### Scenario: Datalog row fans out to a named event
- **WHEN** the daemon processes a `sys_datalog` row with `dbtable=web_domain` and `action=u`
- **THEN** the web module raises the `web_domain_update` event with the decoded `{old,new}` payload

#### Scenario: Unhooked table is ignored
- **WHEN** the daemon processes a `sys_datalog` row with `dbtable=webdav_user`
- **THEN** the web module raises no event and processing continues without error
