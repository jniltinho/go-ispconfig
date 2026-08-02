# client-limits

## ADDED Requirements

### Requirement: Limit templates stored in client_template
The system SHALL manage `client_template` rows with fields from the schema and `client_template.tform.php`: `template_name` NOTEMPTY, `template_type` ∈ {`m` (master), `a` (additional)}, and the same limit/default/server columns as on `client` (e.g. `limit_web_domain`, `limit_dns_zone`, `limit_mailbox`, `limit_ftp_user`, `limit_shell_user`, `limit_database`, `limit_cron`, `limit_client`, `web_php_options`, `ssh_chroot`, `mail_servers`, `web_servers`, `dns_servers`, `db_servers`, …). Template CRUD SHALL use riud permissions and datalog writes (`server_id = 0`).

#### Scenario: Create master template
- **WHEN** an authorized user creates a template with `template_type = m`, name `Starter`, and `limit_web_domain = 5`
- **THEN** a `client_template` row is stored with those values

#### Scenario: Empty template name rejected
- **WHEN** a template is saved without `template_name`
- **THEN** validation fails and no row is written

### Requirement: Template assignment via template_master and client_template_assigned
A client MAY reference a master template through `client.template_master` (`0` = custom limits, no auto-materialize). Additional templates SHALL be stored as rows in `client_template_assigned` (`client_id`, `client_template_id`). Legacy slash-separated ids in `client.template_additional` SHALL be migrated into `client_template_assigned` on save and the text column cleared (PHP parity).

#### Scenario: Assign additional template
- **WHEN** additional template id 7 is assigned to client 3
- **THEN** a `client_template_assigned` row links `client_id = 3` to `client_template_id = 7`

#### Scenario: Legacy template_additional migrated
- **WHEN** a client still has `template_additional = '2/5'` and is saved
- **THEN** assigned rows for templates 2 and 5 exist and `template_additional` is empty

### Requirement: Materialize master and additional templates onto client limits
Applying templates (port of `client_templates.inc.php::apply_client_templates`) SHALL copy the master template's limit/default/server fields onto the client when `template_master > 0`, then merge each additional template: numeric limits add while current ≠ `-1` (additional `-1` promotes to unlimited); `limit_cron_frequency` takes the minimum value ≥ 1; `y`/`n` flags take the less-restrictive value (`y` wins except `force_suexec` where `n` wins); CHECKBOXARRAY and server-list fields are set-unions; additional templates do not override default servers already set by the master. Non-reseller clients MUST NOT receive a non-zero `limit_client` from templates.

#### Scenario: Master template overwrites custom limits
- **WHEN** a client with `template_master` pointing at a template with `limit_dns_zone = 10` is saved
- **THEN** `client.limit_dns_zone` becomes `10`

#### Scenario: Additional template adds numeric quota
- **WHEN** master sets `limit_web_domain = 5` and an additional template has `limit_web_domain = 3`
- **THEN** the materialized client limit is `8`

#### Scenario: Additional unlimited promotes
- **WHEN** master sets `limit_mailbox = 10` and an additional template has `limit_mailbox = -1`
- **THEN** the materialized client limit is `-1`

#### Scenario: Client template cannot grant limit_client
- **WHEN** templates are applied to a non-reseller client (`limit_client = 0`)
- **THEN** `limit_client` remains `0` even if a template specifies a positive value

#### Scenario: Custom master skips materialization
- **WHEN** `template_master = 0`
- **THEN** additional templates are not merged into limits on save (PHP guard against unbounded growth)

### Requirement: Create limit enforcement via RegisterLimitHook
The module SHALL register an `api.RegisterLimitHook` implementation that, before entity creates handled by the foundation CRUD framework, resolves the owning client and enforces the matching `limit_*` column. Semantics: limit `< 0` allow; `== 0` veto; `> 0` veto when the count of existing resources for the client's `sys_groupid` is already ≥ limit. Veto SHALL return `*api.LimitError` with i18n key `error.limit_<entity>` (HTTP 403). Admin identities bypass count limits.

Minimum mappings for phase-1 modules already in tree:

| Create entity | Limit column |
|---|---|
| web vhost (`web_domain` type vhost) | `limit_web_domain` |
| web subdomain / aliasdomain | `limit_web_subdomain` / `limit_web_aliasdomain` |
| `dns_soa` | `limit_dns_zone` |
| `dns_slave` | `limit_dns_slave_zone` |
| `dns_rr` | `limit_dns_record` |
| `client` (child under reseller) | `limit_client` |

Future modules (mail, ftp, shell, database, cron) SHALL reuse the same hook with their `limit_*` columns without changing those endpoints.

#### Scenario: DNS zone create blocked at limit
- **WHEN** a client with `limit_dns_zone = 1` already owns one `dns_soa` and tries to create another
- **THEN** the create is vetoed with 403 and key `error.limit_dns_soa` (or the registered entity key) and no datalog row is written

#### Scenario: Unlimited allows create
- **WHEN** a client has `limit_web_domain = -1` and already owns many sites
- **THEN** a new site create is not vetoed by the limit hook

#### Scenario: Zero limit blocks feature
- **WHEN** a client has `limit_dns_zone = 0`
- **THEN** any `dns_soa` create for that client is vetoed

#### Scenario: Admin bypasses client limits
- **WHEN** an admin creates a zone owned by a client that is already at limit
- **THEN** the create succeeds (admin is not constrained by the client quota)

#### Scenario: Reseller child count enforced
- **WHEN** a reseller with `limit_client = 2` already has two child clients and creates a third
- **THEN** the create is vetoed with a limit error

### Requirement: Child limits cannot exceed parent limits
When a reseller (or admin acting as that reseller context) saves a child client's limits, each numeric/flag limit SHALL be capped so the child is not more permissive than the parent (port of tform `valuelimit` / client limit inheritance). Template materialization on children is also subject to this cap.

#### Scenario: Child web domain limit capped by reseller
- **WHEN** a reseller with `limit_web_domain = 5` tries to set a child to `limit_web_domain = 20`
- **THEN** validation rejects or clamps the value so it does not exceed 5
